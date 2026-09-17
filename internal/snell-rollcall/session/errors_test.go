package session

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"dhs/internal/snell-rollcall/codec"
)

// TestProtocolError_Message covers both forms. A refusal that names a reason
// must show it, because "call refused" alone gives an operator nothing to act
// on; one that carries no text must not print an empty pair of brackets.
func TestProtocolError_Message(t *testing.T) {
	tests := []struct {
		name string
		err  *ProtocolError
		want string
	}{
		{
			"with a reason",
			&ProtocolError{Op: "call", Type: codec.MsgNack, Detail: "no long strings", Err: ErrRefused},
			"rollcall: call: NACK (no long strings)",
		},
		{
			"without one",
			&ProtocolError{Op: "GETSTAT", Type: codec.MsgInvSess, Err: ErrInvalidSession},
			"rollcall: GETSTAT: INVSESS",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.Error(); got != tc.want {
				t.Errorf("Error() = %q, want %q", got, tc.want)
			}
			if !errors.Is(tc.err, tc.err.Err) {
				t.Error("Unwrap must expose the sentinel")
			}
		})
	}
}

// TestProtocolError_Mapping pins which refusal means what. Nack and InvCmd are
// the pair that matters: one means the peer understood and will not, the other
// that it did not understand at all, and a client that conflates them either
// retries something impossible or abandons a peer that wanted a different
// message.
func TestProtocolError_Mapping(t *testing.T) {
	tests := []struct {
		reply codec.PacketType
		want  error
	}{
		{codec.MsgNack, ErrRefused},
		{codec.MsgBusy, ErrBusy},
		{codec.MsgInvCmd, ErrNotUnderstood},
		{codec.MsgInvSess, ErrInvalidSession},
		{codec.MsgRetStat, ErrUnexpectedReply},
	}
	for _, tc := range tests {
		err := protocolError("op", codec.Frame{Type: tc.reply})
		if !errors.Is(err, tc.want) {
			t.Errorf("%s mapped to %v, want %v", tc.reply, err, tc.want)
		}
	}
}

// TestRefusalText covers the optional text a refusal may carry. None of it is
// required, and a frame is well formed without it, so a missing or malformed
// payload must not turn a refusal into a decode failure.
func TestRefusalText(t *testing.T) {
	id, err := codec.ID{Name: "Control Panel"}.AppendTo(nil)
	if err != nil {
		t.Fatalf("AppendTo: %v", err)
	}

	tests := []struct {
		name string
		in   codec.Frame
		want string
	}{
		{"busy names the holder", codec.Frame{Type: codec.MsgBusy, Payload: id}, "Control Panel"},
		{"busy with no payload", codec.Frame{Type: codec.MsgBusy}, ""},
		{"busy with a short payload", codec.Frame{Type: codec.MsgBusy, Payload: []byte{1, 2}}, ""},
		{"nack with a reason", codec.Frame{Type: codec.MsgNack, Payload: []byte("bad\x00")}, "bad"},
		{"nack with none", codec.Frame{Type: codec.MsgNack}, ""},
		{"ack with a note", codec.Frame{Type: codec.MsgAck, Payload: []byte("ok\x00")}, "ok"},
		{"a type that carries no text", codec.Frame{Type: codec.MsgInvCmd, Payload: []byte("x\x00")}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := refusalText(tc.in); got != tc.want {
				t.Errorf("= %q, want %q", got, tc.want)
			}
		})
	}
}

// TestCall_UnencodableIdentity covers a caller whose own identity will not fit
// the wire. It fails before anything is sent and before a session index is
// taken, so a bad configuration does not consume a slot on the peer.
func TestCall_UnencodableIdentity(t *testing.T) {
	h := newHarness(t, Config{})

	bad := ClientIdentity("x", codec.SvcMenus)
	bad.ID.Name = strings.Repeat("n", codec.MaxTextSize)

	_, err := Call(context.Background(), h.link, peerAddr, codec.SvcMenus, codec.LevelUser, bad)
	if !errors.Is(err, codec.ErrStringTooLong) {
		t.Fatalf("err = %v, want ErrStringTooLong", err)
	}
	h.peer.quiet()
	if n := h.link.SessionCount(); n != 0 {
		t.Errorf("%d sessions registered after a failed call", n)
	}
}

// TestLink_IndexExhaustion covers a link asked for more sessions than the
// index space holds. Indices are a single byte with three values reserved, so
// this is reachable on a busy gateway and must be reported rather than
// returning a duplicate.
func TestLink_IndexExhaustion(t *testing.T) {
	h := newHarness(t, Config{})

	var held []*Session
	for {
		s := bareSession(h.link)
		if err := h.link.register(s); err != nil {
			if !strings.Contains(err.Error(), "no free session index") {
				t.Fatalf("err = %v, want the exhaustion message", err)
			}
			break
		}
		held = append(held, s)
		if len(held) > 300 {
			t.Fatal("the index space should have run out by now")
		}
	}

	// The space is one byte with zero, 0xFE and 0xFF reserved.
	if len(held) != 0xFD {
		t.Errorf("held %d sessions before exhaustion, want %d", len(held), 0xFD)
	}

	// Freeing one makes room again.
	h.link.removeSession(held[0].localIndex)
	if err := h.link.register(bareSession(h.link)); err != nil {
		t.Errorf("register after freeing an index: %v", err)
	}
}

// TestExchange_SendFailureReleasesTheSlot covers the socket dying between
// taking the one-in-flight slot and writing. The slot has to come back, or
// every later request on that channel blocks for ever behind a message that
// was never sent.
func TestExchange_SendFailureReleasesTheSlot(t *testing.T) {
	h := newHarness(t, Config{})
	s := h.callSession(t, codec.SvcControl)

	h.peer.close()

	// Give the link a moment to notice, then confirm the failure surfaces
	// rather than hanging on the reply deadline.
	deadline := time.Now().Add(2 * time.Second)
	for {
		_, err := s.Do(context.Background(), codec.MsgGetStat, nil)
		if err != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("a request on a dead link should fail")
		}
	}

	// The channel is not wedged: a second attempt fails the same way rather
	// than blocking.
	done := make(chan error, 1)
	go func() {
		_, err := s.Do(context.Background(), codec.MsgGetID, nil)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Error("the second request should have failed too")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the slot was never released, so the channel is wedged")
	}
}

// TestHandshake_GatewayThatDoesNotStamp covers a peer that leaves our address
// alone, which is what a direct connection to a unit does. There is nothing to
// learn, and nothing must be invented.
func TestHandshake_GatewayThatDoesNotStamp(t *testing.T) {
	h := newHarness(t, Config{})

	done := make(chan error, 1)
	go func() {
		_, err := h.link.Handshake(context.Background())
		done <- err
	}()
	h.peer.recvType(codec.MsgGetDevInfo)

	info, err := codec.DeviceInfo{Address: peerAddr}.AppendTo(nil)
	if err != nil {
		t.Fatalf("AppendTo: %v", err)
	}
	// The reply comes back addressed to the zeros we sent.
	h.peer.write(codec.Frame{
		Dst:     codec.Address{Index: codec.IndexUnknown},
		Src:     peerAddr,
		Type:    codec.MsgRetDevInfo,
		Payload: info,
	})

	if err := <-done; err != nil {
		t.Fatalf("Handshake: %v", err)
	}
	if got := h.link.LocalAddress(); got.Unit != 0 || got.Port != 0 {
		t.Errorf("local address = %s, want it left as zeros", got)
	}
	if got := h.link.RemoteAddress(); got.Unit != peerAddr.Unit {
		t.Errorf("remote address = %s, want the peer's", got)
	}
}

// TestCall_GatewayStampsOnTheAcknowledgement covers learning our address from
// the Call rather than the handshake, which is what happens when a caller
// opens a session without probing first.
func TestCall_GatewayStampsOnTheAcknowledgement(t *testing.T) {
	h := newHarness(t, Config{})

	type result struct {
		s   *Session
		err error
	}
	done := make(chan result, 1)
	go func() {
		s, err := Call(context.Background(), h.link, peerAddr, codec.SvcMenus,
			codec.LevelUser, ClientIdentity("dhs", codec.SvcMenus))
		done <- result{s, err}
	}()

	req := h.peer.recvType(codec.MsgCall)
	if req.Src.Unit != 0 {
		t.Errorf("the call's source unit = %02X, want zero for a client", req.Src.Unit)
	}

	h.peer.write(codec.Frame{
		Dst:  codec.Address{Unit: 0x08, Port: 0xE1, Index: req.Src.Index},
		Src:  addrWithIndex(peerAddr, 0x31),
		Type: codec.MsgAck,
	})

	r := <-done
	if r.err != nil {
		t.Fatalf("Call: %v", r.err)
	}
	if got := h.link.LocalAddress(); got.Unit != 0x08 || got.Port != 0xE1 {
		t.Errorf("local address = %s, want the stamped 0000-08-E1", got)
	}

	// A second session must not overwrite the address we already learned.
	h.callSession(t, codec.SvcControl)
	if got := h.link.LocalAddress(); got.Port != 0xE1 {
		t.Errorf("local address changed to %s on a second call", got)
	}
}

// TestHandshake_Timeout covers a peer that accepts the connection and then
// says nothing, which is what a wrong port looks like: the socket opens, and
// nothing on the other side speaks RollCall.
func TestHandshake_Timeout(t *testing.T) {
	h := newHarness(t, Config{ReplyTimeout: 3 * time.Second})

	errCh := make(chan error, 1)
	go func() {
		_, err := h.link.Handshake(context.Background())
		errCh <- err
	}()
	h.peer.recvType(codec.MsgGetDevInfo)

	h.fire(t, 1, 3*time.Second)

	if err := <-errCh; !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v, want ErrTimeout", err)
	}
}

// TestCall_Timeout covers the same for opening a session, and checks the
// index is given back. A call that times out must not leak a session slot,
// because the peer never opened one.
func TestCall_Timeout(t *testing.T) {
	h := newHarness(t, Config{ReplyTimeout: 3 * time.Second})

	errCh := make(chan error, 1)
	go func() {
		_, err := Call(context.Background(), h.link, peerAddr, codec.SvcMenus,
			codec.LevelUser, ClientIdentity("dhs", codec.SvcMenus))
		errCh <- err
	}()
	h.peer.recvType(codec.MsgCall)

	h.fire(t, 1, 3*time.Second)

	if err := <-errCh; !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v, want ErrTimeout", err)
	}
	if n := h.link.SessionCount(); n != 0 {
		t.Errorf("%d sessions left registered after a timed-out call", n)
	}
}
