package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"dhs/internal/snell-rollcall/codec"
)

// TestEnableBackChannel_NeedsBothMessages pins the step that is easy to miss.
// BkChnReady opens the channel, and control-value pushes additionally require
// ReportChange. With only the first, a session looks subscribed and never
// receives a value.
func TestEnableBackChannel_NeedsBothMessages(t *testing.T) {
	h := newHarness(t, Config{})
	s := h.callSession(t, codec.SvcMenus|codec.SvcControl)

	done := make(chan error, 1)
	go func() { done <- s.EnableBackChannel(context.Background(), true) }()

	ready := h.peer.recvType(codec.MsgBkChnReady)
	if len(ready.Payload) != 1 || ready.Payload[0] != codec.BackChannelEnable {
		t.Errorf("payload = %x, want the enable byte %02x", ready.Payload, codec.BackChannelEnable)
	}
	h.peer.send(ready, codec.MsgAck, nil)

	report := h.peer.recvType(codec.MsgRepFChg)
	if got := uint16(report.Payload[0])<<8 | uint16(report.Payload[1]); got != codec.ReportAllCommands {
		t.Errorf("command = %04X, want the wildcard %04X", got, codec.ReportAllCommands)
	}
	h.peer.send(report, codec.MsgAck, nil)

	if err := <-done; err != nil {
		t.Fatalf("EnableBackChannel: %v", err)
	}
}

// TestEnableBackChannel_MenusOnly covers a menu-only subscription, which does
// not need value reporting and must not send a message the peer may Nack.
func TestEnableBackChannel_MenusOnly(t *testing.T) {
	h := newHarness(t, Config{})
	s := h.callSession(t, codec.SvcMenus)

	done := make(chan error, 1)
	go func() { done <- s.EnableBackChannel(context.Background(), false) }()

	ready := h.peer.recvType(codec.MsgBkChnReady)
	h.peer.send(ready, codec.MsgAck, nil)

	if err := <-done; err != nil {
		t.Fatalf("EnableBackChannel: %v", err)
	}
	h.peer.quiet()
}

func TestEnableBackChannel_Refused(t *testing.T) {
	h := newHarness(t, Config{})
	s := h.callSession(t, codec.SvcControl)

	done := make(chan error, 1)
	go func() { done <- s.EnableBackChannel(context.Background(), true) }()

	ready := h.peer.recvType(codec.MsgBkChnReady)
	h.peer.send(ready, codec.MsgNack, nil)

	if err := <-done; !errors.Is(err, ErrRefused) {
		t.Fatalf("err = %v, want ErrRefused", err)
	}

	// A peer that opens the channel but refuses value reporting is also a
	// failure: the caller asked for values and will not get them.
	done2 := make(chan error, 1)
	go func() { done2 <- s.EnableBackChannel(context.Background(), true) }()
	ready = h.peer.recvType(codec.MsgBkChnReady)
	h.peer.send(ready, codec.MsgAck, nil)
	report := h.peer.recvType(codec.MsgRepFChg)
	h.peer.send(report, codec.MsgNack, nil)

	if err := <-done2; !errors.Is(err, ErrRefused) {
		t.Fatalf("err = %v, want ErrRefused", err)
	}
}

func TestDisableBackChannel(t *testing.T) {
	h := newHarness(t, Config{})
	s := h.callSession(t, codec.SvcControl)

	done := make(chan error, 1)
	go func() { done <- s.DisableBackChannel(context.Background()) }()

	f := h.peer.recvType(codec.MsgBkChnReady)
	if len(f.Payload) != 1 || f.Payload[0] != codec.BackChannelDisable {
		t.Errorf("payload = %x, want the disable byte", f.Payload)
	}
	h.peer.send(f, codec.MsgAck, nil)

	if err := <-done; err != nil {
		t.Fatalf("DisableBackChannel: %v", err)
	}
}

// TestPushIsAcknowledgedByTheApplication pins the flow control the protocol
// provides. Each push is a request, and its acknowledgement is what asks for
// the next one. Acknowledging before the application has taken the message
// throws that away and lets a fast device outrun a slow reader.
func TestPushIsAcknowledgedByTheApplication(t *testing.T) {
	h := newHarness(t, Config{})
	s := h.callSession(t, codec.SvcControl)

	value, err := codec.FuncStatus{Command: 0x0113, Mode: codec.ModeValue, Value: -60}.AppendTo(nil)
	if err != nil {
		t.Fatalf("AppendTo: %v", err)
	}
	h.peer.push(s, codec.MsgRetFStat, value)

	var got Push
	select {
	case got = <-s.Pushes():
	case <-time.After(2 * time.Second):
		t.Fatal("the push was never delivered")
	}

	if got.Frame.Type != codec.MsgRetFStat {
		t.Errorf("push type = %s, want RETFSTAT", got.Frame.Type)
	}
	if !got.Frame.BackChannel() {
		t.Error("a push must carry the back-channel flag")
	}
	fs, err := codec.DecodeFuncStatus(got.Frame.Payload)
	if err != nil {
		t.Fatalf("DecodeFuncStatus: %v", err)
	}
	if fs.Command != 0x0113 || fs.Value != -60 {
		t.Errorf("= %+v, want command 275 at -60", fs)
	}

	// Nothing is sent until the application acknowledges.
	h.peer.quiet()

	if err := got.Ack(); err != nil {
		t.Fatalf("Ack: %v", err)
	}
	ack := h.peer.recvType(codec.MsgAck)
	if !ack.BackChannel() {
		t.Error("the acknowledgement must go back on the back channel")
	}
	if ack.Dst.Index != s.RemoteIndex() {
		t.Errorf("ack destination index = %d, want the peer's %d", ack.Dst.Index, s.RemoteIndex())
	}
}

// TestPushAddressing pins the addressing a push must carry, which is the third
// of the three index mistakes found during the audit. A push is addressed to
// the client's own index, not the server's, and one addressed wrongly is
// dropped without a word.
func TestPushAddressing(t *testing.T) {
	h := newHarness(t, Config{})
	s := h.callSession(t, codec.SvcControl)

	// Addressed correctly: to our index.
	h.peer.push(s, codec.MsgDispData, nil)
	select {
	case <-s.Pushes():
	case <-time.After(2 * time.Second):
		t.Fatal("a correctly addressed push was not delivered")
	}

	// Addressed to the peer's own index instead. It belongs to no session
	// here and must not be delivered to ours.
	h.peer.write(codec.Frame{
		Dst:   addrWithIndex(peerAddr, s.RemoteIndex()+50),
		Src:   addrWithIndex(peerAddr, s.RemoteIndex()),
		Type:  codec.MsgDispData,
		Flags: codec.FlagBackChannel,
	})

	select {
	case p := <-s.Pushes():
		t.Fatalf("a push for another session was delivered: %s", p.Frame)
	case <-time.After(100 * time.Millisecond):
	}
}

// TestPushQueueBackPressure covers a reader that stops taking pushes. The queue
// fills and the withheld acknowledgements stall the peer, which is what the
// protocol intends; the alternative is dropping values silently.
func TestPushQueueBackPressure(t *testing.T) {
	h := newHarness(t, Config{PushQueue: 2})
	s := h.callSession(t, codec.SvcControl)

	for range 5 {
		h.peer.push(s, codec.MsgDispData, nil)
	}

	// A front-channel frame behind the pushes is the barrier. The read loop
	// is sequential, so once this has been dispatched every push before it
	// has been too, and the queue can be counted without racing the reader.
	h.peer.write(codec.Frame{
		Dst:  addrWithIndex(peerAddr, s.LocalIndex()),
		Src:  addrWithIndex(peerAddr, s.RemoteIndex()),
		Type: codec.MsgTime,
	})
	select {
	case <-h.link.Unsolicited():
	case <-time.After(2 * time.Second):
		t.Fatal("the barrier frame never arrived")
	}

	// Exactly the queue depth is held; the rest were refused rather than
	// buffered without bound.
	held := 0
	for {
		select {
		case <-s.Pushes():
			held++
			continue
		default:
		}
		break
	}
	if held != 2 {
		t.Errorf("queue held %d pushes, want the configured depth of 2", held)
	}

	// Nothing was acknowledged, because nothing acknowledges on the
	// application's behalf.
	h.peer.quiet()
}

// TestProviderPushIsAnActiveMessage covers the provider side. A push is a
// request: the next may not be sent until this one is acknowledged, which is
// why a burst of changes queues instead of being written to the socket.
func TestProviderPushIsAnActiveMessage(t *testing.T) {
	h := newHarness(t, Config{})
	s := h.callSession(t, codec.SvcControl)

	first := make(chan error, 1)
	go func() { first <- s.Push(context.Background(), codec.MsgRetFStat, make([]byte, 8)) }()
	sent := h.peer.recvType(codec.MsgRetFStat)
	if !sent.BackChannel() {
		t.Error("a push must carry the back-channel flag")
	}

	second := make(chan error, 1)
	go func() { second <- s.Push(context.Background(), codec.MsgDispData, nil) }()
	h.peer.quiet()

	select {
	case <-second:
		t.Fatal("a second push went out before the first was acknowledged")
	default:
	}

	h.peer.sendFlags(sent, codec.MsgAck, codec.FlagBackChannel, nil)
	if err := <-first; err != nil {
		t.Fatalf("first push: %v", err)
	}

	next := h.peer.recvType(codec.MsgDispData)
	h.peer.sendFlags(next, codec.MsgAck, codec.FlagBackChannel, nil)
	if err := <-second; err != nil {
		t.Fatalf("second push: %v", err)
	}
}

// TestPushTimeout covers a client that stops acknowledging. A provider must
// not wait forever on it, or one dead panel stalls every update it subscribed
// to.
func TestPushTimeout(t *testing.T) {
	h := newHarness(t, Config{ReplyTimeout: 3 * time.Second})
	s := h.callSession(t, codec.SvcControl)

	errCh := make(chan error, 1)
	go func() { errCh <- s.Push(context.Background(), codec.MsgDispData, nil) }()
	h.peer.recvType(codec.MsgDispData)

	h.fire(t, 1, 3*time.Second)

	if err := <-errCh; !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v, want ErrTimeout", err)
	}
}

// TestReplyOnTheBackChannel covers a provider answering a push the other way,
// which is what a consumer's acknowledgement is underneath.
func TestReplyOnTheBackChannel(t *testing.T) {
	h := newHarness(t, Config{})
	s := h.callSession(t, codec.SvcControl)

	if err := s.Reply(codec.MsgAck, nil); err != nil {
		t.Fatalf("Reply: %v", err)
	}
	f := h.peer.recvType(codec.MsgAck)
	if !f.BackChannel() {
		t.Error("Reply must set the back-channel flag")
	}
}

// TestFrontChannelChatterIsSurfaced covers a peer sending on the front channel
// with nothing outstanding. Some units send display updates that way, so the
// frame is surfaced rather than dropped.
func TestFrontChannelChatterIsSurfaced(t *testing.T) {
	h := newHarness(t, Config{})
	s := h.callSession(t, codec.SvcDisplay)

	h.peer.write(codec.Frame{
		Dst:  addrWithIndex(peerAddr, s.LocalIndex()),
		Src:  addrWithIndex(peerAddr, s.RemoteIndex()),
		Type: codec.MsgDispData,
	})

	select {
	case f := <-h.link.Unsolicited():
		if f.Type != codec.MsgDispData {
			t.Errorf("= %s, want DISPDATA", f.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("front-channel chatter was dropped instead of surfaced")
	}
}
