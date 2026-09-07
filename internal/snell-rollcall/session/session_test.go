package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"dhs/internal/snell-rollcall/codec"
)

// TestCall_StickyIndices is the defect this package was shaped to prevent.
//
// Two session indices exist and they are not interchangeable. Ours goes in the
// source of everything we send and in the destination of everything the peer
// pushes. The peer's, learned from its acknowledgement, goes in the
// destination of everything we send. Swapping them produced SP_INVSESS during
// the audit and silently lost every back-channel push.
func TestCall_StickyIndices(t *testing.T) {
	h := newHarness(t, Config{})

	type result struct {
		s   *Session
		err error
	}
	done := make(chan result, 1)
	go func() {
		s, err := Call(context.Background(), h.link, peerAddr,
			codec.SvcMenus|codec.SvcControl, codec.LevelSupervisor,
			ClientIdentity("dhs", codec.SvcMenus))
		done <- result{s, err}
	}()

	req := h.peer.recvType(codec.MsgCall)

	// The call goes to the unconnected index, because there is no session
	// yet, and carries our chosen index as its source.
	if req.Dst.Index != codec.IndexUnknown {
		t.Errorf("Call destination index = %d, want %d (UNKNOWNSESS)",
			req.Dst.Index, codec.IndexUnknown)
	}
	ourIndex := req.Src.Index
	if ourIndex == codec.IndexUnknown || ourIndex == codec.IndexBlind {
		t.Fatalf("Call source index = %d, want a real allocated index", ourIndex)
	}

	// The peer answers from its own index, which is deliberately different.
	const peerIndex int16 = 0x2A
	h.peer.write(codec.Frame{
		Dst:  addrWithIndex(req.Src, ourIndex),
		Src:  addrWithIndex(peerAddr, peerIndex),
		Type: codec.MsgAck,
	})

	r := <-done
	if r.err != nil {
		t.Fatalf("Call: %v", r.err)
	}
	s := r.s

	if s.LocalIndex() != ourIndex {
		t.Errorf("LocalIndex = %d, want %d", s.LocalIndex(), ourIndex)
	}
	if s.RemoteIndex() != peerIndex {
		t.Errorf("RemoteIndex = %d, want %d", s.RemoteIndex(), peerIndex)
	}

	// Every later message must carry the peer's index as its destination and
	// ours as its source.
	go func() { _, _ = s.Do(context.Background(), codec.MsgGetStat, nil) }()
	next := h.peer.recvType(codec.MsgGetStat)

	if next.Dst.Index != peerIndex {
		t.Errorf("destination index = %d, want the peer's %d", next.Dst.Index, peerIndex)
	}
	if next.Src.Index != ourIndex {
		t.Errorf("source index = %d, want ours %d", next.Src.Index, ourIndex)
	}
}

// TestCall_ServicesAreAllOrNothing pins the negotiation rule. A peer that
// cannot supply every requested bit refuses the whole call, so a client
// wanting to fall back from the 32-bit generation must issue a second call
// without the long-string bit rather than expect a partial grant.
func TestCall_ServicesAreAllOrNothing(t *testing.T) {
	h := newHarness(t, Config{})

	want := codec.SvcMenus | codec.SvcControl | codec.SvcLongStr
	errCh := make(chan error, 1)
	go func() {
		_, err := Call(context.Background(), h.link, peerAddr, want,
			codec.LevelSupervisor, ClientIdentity("dhs", want))
		errCh <- err
	}()

	req := h.peer.recvType(codec.MsgCall)
	conn, err := codec.DecodeConnect(req.Payload)
	if err != nil {
		t.Fatalf("DecodeConnect: %v", err)
	}
	if conn.Services != want {
		t.Errorf("requested %s, want %s", conn.Services, want)
	}
	if conn.UserLevel != codec.LevelSupervisor {
		t.Errorf("user level = %s, want supervisor", conn.UserLevel)
	}

	// A peer without long strings refuses the whole thing.
	h.peer.send(req, codec.MsgNack, []byte("no long strings\x00"))

	err = <-errCh
	if !errors.Is(err, ErrRefused) {
		t.Fatalf("err = %v, want ErrRefused", err)
	}
	var pe *ProtocolError
	if !errors.As(err, &pe) || pe.Detail != "no long strings" {
		t.Errorf("err = %v, want the peer's text carried through", err)
	}

	// The refused session must not be left registered on the link.
	if n := h.link.SessionCount(); n != 0 {
		t.Errorf("link holds %d sessions after a refused call", n)
	}

	// The second call, without the bit, is what a client does next.
	s := h.callSession(t, codec.SvcMenus|codec.SvcControl)
	if s.Uses32Bit() {
		t.Error("the fallback session must not claim the 32-bit generation")
	}
}

func TestCall_Busy(t *testing.T) {
	h := newHarness(t, Config{})

	errCh := make(chan error, 1)
	go func() {
		_, err := Call(context.Background(), h.link, peerAddr, codec.SvcMenus,
			codec.LevelUser, ClientIdentity("dhs", codec.SvcMenus))
		errCh <- err
	}()

	req := h.peer.recvType(codec.MsgCall)

	// Busy may name who holds the session, as an ID_STR.
	id, err := codec.ID{Name: "Control Panel"}.AppendTo(nil)
	if err != nil {
		t.Fatalf("AppendTo: %v", err)
	}
	h.peer.send(req, codec.MsgBusy, id)

	err = <-errCh
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("err = %v, want ErrBusy", err)
	}
	var pe *ProtocolError
	if !errors.As(err, &pe) || pe.Detail != "Control Panel" {
		t.Errorf("err = %v, want it to name who holds the session", err)
	}
	// Busy is worth retrying, unlike a refusal.
	if errors.Is(err, ErrRefused) {
		t.Error("busy must not read as a refusal")
	}
}

func TestCall_RejectsAnImpossibleLevel(t *testing.T) {
	h := newHarness(t, Config{})

	// LevelAll is a mask value the vendor engine rejects outright, so sending
	// it wastes a round trip and a session slot.
	_, err := Call(context.Background(), h.link, peerAddr, codec.SvcMenus,
		codec.LevelAll, ClientIdentity("dhs", codec.SvcMenus))
	if err == nil {
		t.Fatal("a level above factory must be refused before it is sent")
	}
	h.peer.quiet()
}

// TestSession_Generations covers the property that decides which message types
// every later request uses. It belongs to the session, not the device: the
// same unit serves both, one session each.
func TestSession_Generations(t *testing.T) {
	h := newHarness(t, Config{})

	gen16 := h.callSession(t, codec.SvcMenus|codec.SvcControl)
	if gen16.Uses32Bit() {
		t.Error("a session without the long-string bit is the 16-bit generation")
	}

	gen32 := h.callSession(t, codec.SvcMenus|codec.SvcControl|codec.SvcLongStr)
	if !gen32.Uses32Bit() {
		t.Error("a session with the long-string bit is the 32-bit generation")
	}

	// Both live on one link, with distinct indices.
	if gen16.LocalIndex() == gen32.LocalIndex() {
		t.Error("two sessions on one link share an index")
	}
	if n := h.link.SessionCount(); n != 2 {
		t.Errorf("link holds %d sessions, want 2", n)
	}
}

// TestSession_Do covers an ordinary request and the refusals that are not
// errors of ours. Nack and InvCmd are different failures: one means the peer
// will not, the other that it cannot understand, and a client that treats them
// alike either retries forever or gives up too early.
func TestSession_Do(t *testing.T) {
	h := newHarness(t, Config{})
	s := h.callSession(t, codec.SvcControl)

	tests := []struct {
		name  string
		reply codec.PacketType
		want  error
	}{
		{"answered", codec.MsgRetFStat, nil},
		{"refused", codec.MsgNack, ErrRefused},
		{"not implemented", codec.MsgInvCmd, ErrNotUnderstood},
		{"bad session", codec.MsgInvSess, ErrInvalidSession},
		{"busy", codec.MsgBusy, ErrBusy},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			type result struct {
				f   codec.Frame
				err error
			}
			done := make(chan result, 1)
			go func() {
				f, err := s.Do(context.Background(), codec.MsgGetFStat, []byte{0, 1})
				done <- result{f, err}
			}()

			req := h.peer.recvType(codec.MsgGetFStat)
			h.peer.send(req, tc.reply, nil)

			r := <-done
			if tc.want == nil {
				if r.err != nil {
					t.Fatalf("Do: %v", r.err)
				}
				if r.f.Type != tc.reply {
					t.Errorf("reply = %s, want %s", r.f.Type, tc.reply)
				}
				return
			}
			if !errors.Is(r.err, tc.want) {
				t.Errorf("err = %v, want %v", r.err, tc.want)
			}
			// The frame comes back even on a refusal, so a caller can read
			// whatever the peer put in it.
			if r.f.Type != tc.reply {
				t.Errorf("reply frame = %s, want %s", r.f.Type, tc.reply)
			}
		})
	}
}

// TestSession_TermIsAlwaysSent pins the rule that keeps units usable. Servers
// do not time idle sessions out, so a session we abandon is leaked until the
// unit reboots.
func TestSession_TermIsAlwaysSent(t *testing.T) {
	h := newHarness(t, Config{})
	s := h.callSession(t, codec.SvcMenus)

	if err := s.Term(context.Background(), codec.TermUser, "done"); err != nil {
		t.Fatalf("Term: %v", err)
	}

	f := h.peer.recvType(codec.MsgTerm)
	ts, err := codec.DecodeTermSess(f.Payload)
	if err != nil {
		t.Fatalf("DecodeTermSess: %v", err)
	}
	if ts.Code != codec.TermUser || ts.Reason != "done" {
		t.Errorf("= %+v, want the user code and reason", ts)
	}
	if f.Dst.Index != s.RemoteIndex() {
		t.Errorf("Term destination index = %d, want the peer's %d", f.Dst.Index, s.RemoteIndex())
	}

	// The session is gone from the link and refuses further work.
	if n := h.link.SessionCount(); n != 0 {
		t.Errorf("link still holds %d sessions", n)
	}
	if _, err := s.Do(context.Background(), codec.MsgGetStat, nil); !errors.Is(err, ErrSessionClosed) {
		t.Errorf("err = %v, want ErrSessionClosed", err)
	}

	// Terminating twice sends one Term. A second would be answered with
	// InvSess by a peer that already freed the session.
	if err := s.Close(); err != nil {
		t.Errorf("Close after Term: %v", err)
	}
	h.peer.quiet()
}

// TestSession_TermSurvivesAnOverlongReason covers a caller passing a reason too
// long for the twenty-byte field. The code is what the peer acts on, so the
// session must still be closed rather than left behind over a label.
func TestSession_TermSurvivesAnOverlongReason(t *testing.T) {
	h := newHarness(t, Config{})
	s := h.callSession(t, codec.SvcMenus)

	long := "a reason far longer than the field can carry"
	if err := s.Term(context.Background(), codec.TermNetError, long); err != nil {
		t.Fatalf("Term: %v", err)
	}

	f := h.peer.recvType(codec.MsgTerm)
	ts, err := codec.DecodeTermSess(f.Payload)
	if err != nil {
		t.Fatalf("DecodeTermSess: %v", err)
	}
	if ts.Code != codec.TermNetError {
		t.Errorf("code = %s, want net-error", ts.Code)
	}
	if ts.Reason != "" {
		t.Errorf("reason = %q, want it dropped rather than the Term abandoned", ts.Reason)
	}
}

// TestSession_TermWhileARequestIsPending covers the case the vendor gets
// wrong. A request that will never be answered holds the one-in-flight slot;
// waiting for it is how a session ends up abandoned instead of closed, so Term
// is sent directly.
func TestSession_TermWhileARequestIsPending(t *testing.T) {
	h := newHarness(t, Config{})
	s := h.callSession(t, codec.SvcControl)

	go func() { _, _ = s.Do(context.Background(), codec.MsgGetStat, nil) }()
	h.peer.recvType(codec.MsgGetStat) // never answered

	if err := s.Term(context.Background(), codec.TermUser, ""); err != nil {
		t.Fatalf("Term with a request pending: %v", err)
	}
	h.peer.recvType(codec.MsgTerm)
}

func TestSession_Peer(t *testing.T) {
	h := newHarness(t, Config{})
	s := h.callSession(t, codec.SvcMenus)

	p := s.Peer()
	if p.Unit != peerAddr.Unit {
		t.Errorf("Peer unit = %02X, want %02X", p.Unit, peerAddr.Unit)
	}
	if p.Index != s.RemoteIndex() {
		t.Errorf("Peer index = %d, want the peer's session index %d", p.Index, s.RemoteIndex())
	}
	if s.Services() != codec.SvcMenus {
		t.Errorf("Services = %s", s.Services())
	}
	if s.UserLevel() != codec.LevelSupervisor {
		t.Errorf("UserLevel = %s, want supervisor", s.UserLevel())
	}
}

// TestSession_LinkClosedWakesEveryone covers a link dying under its sessions.
// A caller blocked on a request must learn immediately rather than wait out
// its own timeout, and no Term is attempted because there is nothing to send
// it on.
func TestSession_LinkClosedWakesEveryone(t *testing.T) {
	h := newHarness(t, Config{})
	s := h.callSession(t, codec.SvcControl)

	errCh := make(chan error, 1)
	go func() {
		_, err := s.Do(context.Background(), codec.MsgGetStat, nil)
		errCh <- err
	}()
	h.peer.recvType(codec.MsgGetStat)

	_ = h.link.Close()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("the pending request should have failed")
		}
		if !errors.Is(err, ErrLinkClosed) {
			t.Errorf("err = %v, want ErrLinkClosed", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a pending request was not woken when the link closed")
	}

	if !errors.Is(h.link.Err(), ErrLinkClosed) {
		t.Errorf("link error = %v, want ErrLinkClosed", h.link.Err())
	}
	select {
	case <-h.link.Done():
	default:
		t.Error("Done was not closed")
	}
}
