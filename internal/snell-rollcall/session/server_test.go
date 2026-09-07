package session

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"dhs/internal/snell-rollcall/codec"
)

// The answering side is tested the same way as the calling side: a real link
// with a fake peer on the other end of a pipe. What is being checked is where
// a frame goes when it matches nothing, because that is the whole difference
// between the two roles.

// serverHandler is a Handler a test can steer.
type serverHandler struct {
	t *testing.T

	// refuse is what Call returns; nil accepts.
	refuse error

	// before runs inside Call, before the session is accepted, so a test can
	// break the link from the one goroutine that is allowed to.
	before func()

	accepted  chan *Session
	requests  chan codec.Frame
	unsolicit chan codec.Frame

	// failures carries what Accept refused with, so a test can wait for the
	// handler rather than race it: it runs on the read loop, which is not the
	// goroutine the test is on.
	failures chan error

	// answer is what Request replies with. Zero refuses instead.
	answer codec.PacketType
}

func newServerHandler(t *testing.T) *serverHandler {
	return &serverHandler{
		t:         t,
		accepted:  make(chan *Session, 4),
		requests:  make(chan codec.Frame, 8),
		unsolicit: make(chan codec.Frame, 8),
		failures:  make(chan error, 4),
		answer:    codec.MsgAck,
	}
}

func (h *serverHandler) Call(l *Link, req codec.Frame, conn codec.Connect) error {
	if h.before != nil {
		h.before()
	}
	if h.refuse != nil {
		return h.refuse
	}

	s, err := Accept(l, req, conn)
	if err != nil {
		h.failures <- err
		return err
	}
	h.accepted <- s
	return nil
}

func (h *serverHandler) Request(s *Session, req codec.Frame) {
	h.requests <- req

	if h.answer == 0 {
		if err := s.Refuse(RefuseNack("no")); err != nil {
			h.t.Logf("refuse: %v", err)
		}
		return
	}
	if err := s.Answer(h.answer, nil); err != nil {
		h.t.Logf("answer: %v", err)
	}
}

func (h *serverHandler) Unsolicited(_ *Link, req codec.Frame) {
	h.unsolicit <- req
}

// callFrame is what a client sends to open a session.
func callFrame(index int16, services codec.Service, port uint8) codec.Frame {
	payload, _ := codec.Connect{
		Services:  services,
		UserLevel: codec.LevelSupervisor,
		Caller:    codec.DeviceInfo{ID: codec.ID{Name: "peer"}},
	}.AppendTo(nil)

	return codec.Frame{
		Dst:     codec.Address{Unit: 0x08, Port: port, Index: codec.IndexUnknown},
		Src:     addrWithIndex(peerAddr, index),
		Type:    codec.MsgCall,
		Payload: payload,
	}
}

// serveHarness is a link that answers, with the handler steering it.
func serveHarness(t *testing.T) (*harness, *serverHandler) {
	t.Helper()

	h := newServerHandler(t)
	return newHarness(t, Config{
		Handler:           h,
		Local:             Address{Unit: 0x08},
		KeepaliveInterval: -1,
	}), h
}

func TestAcceptTakesTheIndicesFromTheRightEnds(t *testing.T) {
	hn, h := serveHarness(t)

	hn.peer.write(callFrame(0x30, codec.SvcMenus, 0x02))

	ack := hn.peer.recvType(codec.MsgAck)
	if ack.Dst.Index != 0x30 {
		t.Errorf("the acknowledgement went to index %d, want the client's 0x30", ack.Dst.Index)
	}

	s := <-h.accepted
	if ack.Src.Index != s.LocalIndex() {
		t.Errorf("the acknowledgement carried index %d, want ours (%d)", ack.Src.Index, s.LocalIndex())
	}
	if s.RemoteIndex() != 0x30 {
		t.Errorf("remote index = %d, want 0x30", s.RemoteIndex())
	}
	if !s.IsServer() {
		t.Error("a session we accepted should know it is the server")
	}

	// A server session answers as the address the client called, which on a
	// gateway is the card's slot rather than the gateway's own address.
	if got := s.LocalAddress(); got.Port != 0x02 || got.Unit != 0x08 {
		t.Errorf("the session answers as %s, want unit 08 port 02", got)
	}
	if s.Link() != hn.link {
		t.Error("the session does not know which link it is on")
	}
	if got := s.Describe(); got == "" {
		t.Error("a session should describe itself for a log line")
	}
}

func TestAServerSessionAnswersOnTheSlotItWasCalledOn(t *testing.T) {
	hn, h := serveHarness(t)

	hn.peer.write(callFrame(0x30, codec.SvcMenus, 0x07))
	hn.peer.recvType(codec.MsgAck)
	s := <-h.accepted

	// A request on the established session, addressed the way a client
	// addresses one: to our index.
	hn.peer.write(codec.Frame{
		Dst:  addrWithIndex(codec.Address{Unit: 0x08, Port: 0x07}, s.LocalIndex()),
		Src:  addrWithIndex(peerAddr, s.RemoteIndex()),
		Type: codec.MsgKeepAlive,
	})

	if got := (<-h.requests).Type; got != codec.MsgKeepAlive {
		t.Errorf("the handler was given %s", got)
	}

	reply := hn.peer.recvType(codec.MsgAck)
	if reply.Src.Port != 0x07 {
		t.Errorf("the answer came from port %02X, want the slot the client called", reply.Src.Port)
	}
	if reply.Src.Index != s.LocalIndex() || reply.Dst.Index != s.RemoteIndex() {
		t.Errorf("the answer was addressed %s -> %s", reply.Src, reply.Dst)
	}
}

func TestARefusalSaysWhichKind(t *testing.T) {
	for _, tc := range []struct {
		name    string
		err     error
		want    codec.PacketType
		payload string
	}{
		{"busy is worth retrying", RefuseBusy("full"), codec.MsgBusy, ""},
		{"a nack with something to show", RefuseNack("no route"), codec.MsgNack, "no route"},
		{"a nack with nothing to say", RefuseNack(""), codec.MsgNack, ""},
		{"a message we do not implement", RefuseInvalidCommand(), codec.MsgInvCmd, ""},
		{"a refusal wrapped in context", fmt.Errorf("while accepting: %w", RefuseBusy("full")),
			codec.MsgBusy, ""},
		{"a handler that simply failed", errors.New("disk on fire"), codec.MsgNack, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hn, h := serveHarness(t)
			h.refuse = tc.err

			hn.peer.write(callFrame(0x31, codec.SvcMenus, 0))

			got := hn.peer.recvType(tc.want)
			if tc.payload != "" {
				text, _ := codec.CString(got.Payload)
				if text != tc.payload {
					t.Errorf("refusal carried %q, want %q", text, tc.payload)
				}
			}
		})
	}
}

func TestARefusalDescribesItself(t *testing.T) {
	if got := RefuseBusy("").Error(); got != "rollcall: refused with BUSY" {
		t.Errorf("Error() = %q", got)
	}
	if got := RefuseNack("no route").Error(); got != "rollcall: refused with NACK: no route" {
		t.Errorf("Error() = %q", got)
	}
}

func TestACallWeCannotDecodeIsNacked(t *testing.T) {
	hn, _ := serveHarness(t)

	hn.peer.write(codec.Frame{
		Dst:     codec.Address{Unit: 0x08, Index: codec.IndexUnknown},
		Src:     addrWithIndex(peerAddr, 0x32),
		Type:    codec.MsgCall,
		Payload: []byte{0x01},
	})

	hn.peer.recvType(codec.MsgNack)
}

func TestACallWeCannotAcknowledgeIsNotAccepted(t *testing.T) {
	h := newServerHandler(t)
	hn, blocked := newBlockedHarness(t, Config{
		Handler: h, Local: Address{Unit: 0x08}, KeepaliveInterval: -1,
	})

	// The write fails from inside the handler, which is the one place a test
	// can break it without racing the read loop: this runs on it.
	h.before = func() { blocked.failing.Store(true) }

	hn.peer.write(callFrame(0x33, codec.SvcMenus, 0))

	if err := waitForFailure(t, h); errors.Is(err, ErrLinkClosed) {
		t.Fatalf("the call was refused before the acknowledgement was tried: %v", err)
	}

	// An acknowledgement that never arrived is not an accepted session. Leaving
	// it registered would hold an index for a client that does not know it has
	// one, and a unit that runs out of indices stops answering anybody.
	if n := hn.link.SessionCount(); n != 0 {
		t.Errorf("%d sessions kept after an acknowledgement that never went", n)
	}
}

func TestAnErrorThatUnwrapsToNothingIsANack(t *testing.T) {
	hn, h := serveHarness(t)
	h.refuse = emptyWrapper{}

	hn.peer.write(callFrame(0x38, codec.SvcMenus, 0))
	hn.peer.recvType(codec.MsgNack)
}

func TestACallOnALinkThatIsClosingIsNotAccepted(t *testing.T) {
	hn, h := serveHarness(t)

	h.before = func() { hn.link.closeWith(ErrLinkClosed) }
	hn.peer.write(callFrame(0x34, codec.SvcMenus, 0))

	if err := waitForFailure(t, h); !errors.Is(err, ErrLinkClosed) {
		t.Errorf("refused with %v, want the link's own error", err)
	}
	if n := hn.link.SessionCount(); n != 0 {
		t.Errorf("%d sessions opened on a link that is closing", n)
	}
}

func TestAFrameForNoSessionGoesToTheHandler(t *testing.T) {
	hn, h := serveHarness(t)

	// On a link that serves, a frame that answers nothing is a request rather
	// than an announcement. Dropping it means a client waits out its timeout
	// for something we chose not to read.
	hn.peer.write(codec.Frame{
		Dst:  codec.Address{Unit: 0x08, Index: codec.IndexUnknown},
		Src:  addrWithIndex(peerAddr, codec.IndexUnknown),
		Type: codec.MsgGetDevInfo,
	})

	if got := (<-h.unsolicit).Type; got != codec.MsgGetDevInfo {
		t.Errorf("the handler was given %s", got)
	}
}

func TestARefusalOnAnEstablishedSession(t *testing.T) {
	hn, h := serveHarness(t)
	h.answer = 0

	hn.peer.write(callFrame(0x35, codec.SvcMenus, 0))
	hn.peer.recvType(codec.MsgAck)
	s := <-h.accepted

	hn.peer.write(codec.Frame{
		Dst:  addrWithIndex(codec.Address{Unit: 0x08}, s.LocalIndex()),
		Src:  addrWithIndex(peerAddr, s.RemoteIndex()),
		Type: codec.MsgGetTime,
	})
	<-h.requests

	got := hn.peer.recvType(codec.MsgNack)
	if text, _ := codec.CString(got.Payload); text != "no" {
		t.Errorf("refusal carried %q", text)
	}
}

func TestSendFrameWritesWhatItIsGiven(t *testing.T) {
	hn, _ := serveHarness(t)

	err := hn.link.SendFrame(codec.Frame{
		Dst:  addrWithIndex(peerAddr, codec.IndexUnknown),
		Src:  addrWithIndex(codec.Address{Unit: 0x08, Port: 0xE0}, codec.IndexUnknown),
		Type: codec.MsgRetDevInfo,
	})
	if err != nil {
		t.Fatalf("SendFrame: %v", err)
	}

	got := hn.peer.recvType(codec.MsgRetDevInfo)
	if got.Dst.Port != 0x00 || got.Src.Port != 0xE0 {
		t.Errorf("addressed %s -> %s", got.Src, got.Dst)
	}
}

func TestAnAnswerIntoADeadConnectionIsLogged(t *testing.T) {
	hn, _ := serveHarness(t)
	hn.peer.close()

	// replyTo has nowhere to write and nobody to tell. It must log rather than
	// fail: it is called from the read loop, which has to carry on.
	hn.link.replyTo(codec.Frame{
		Dst:  codec.Address{Unit: 0x08, Index: codec.IndexUnknown},
		Src:  addrWithIndex(peerAddr, codec.IndexUnknown),
		Type: codec.MsgGetDevInfo,
	}, codec.MsgNack, nil)
}

func TestTerminateEndsASessionWithoutSayingSo(t *testing.T) {
	hn, h := serveHarness(t)

	hn.peer.write(callFrame(0x36, codec.SvcMenus, 0))
	hn.peer.recvType(codec.MsgAck)
	s := <-h.accepted

	// A client that has said goodbye is not listening, so nothing goes back;
	// what matters is that our half is gone rather than held until the unit
	// reboots.
	s.Terminate(nil)
	hn.peer.quiet()

	if hn.link.SessionCount() != 0 {
		t.Error("the session was kept after being terminated")
	}
	// Twice is not an error: a client's Term and a link going down can both
	// arrive for one session.
	s.Terminate(errors.New("again"))
}

func TestServeContextEndsWithTheContext(t *testing.T) {
	hn, _ := serveHarness(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := hn.link.ServeContext(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("ServeContext returned %v, want context.Canceled", err)
	}
}

func TestServeContextEndsWithTheLink(t *testing.T) {
	hn, _ := serveHarness(t)

	go func() { _ = hn.link.Close() }()

	if err := hn.link.ServeContext(context.Background()); !errors.Is(err, ErrLinkClosed) {
		t.Errorf("ServeContext returned %v, want the link's own error", err)
	}
}

func TestASessionClosedTwiceStaysClosed(t *testing.T) {
	hn, h := serveHarness(t)

	hn.peer.write(callFrame(0x37, codec.SvcMenus, 0))
	hn.peer.recvType(codec.MsgAck)
	s := <-h.accepted

	s.Terminate(nil)

	// The link going down afterwards must not try to close it again: the
	// second close would be re-entering a shutdown that has already run.
	_ = hn.link.Close()
}

// emptyWrapper is an error that says it wraps something and then does not,
// which is what walking an unwrap chain has to survive.
type emptyWrapper struct{}

func (emptyWrapper) Error() string { return "nothing underneath" }
func (emptyWrapper) Unwrap() error { return nil }

// waitForFailure returns what the handler refused with.
//
// Waiting is the point: the handler runs on the read loop, so a test that
// looked straight after writing would be looking before it had run.
func waitForFailure(t *testing.T, h *serverHandler) error {
	t.Helper()

	select {
	case err := <-h.failures:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("the handler never refused the call")
		return nil
	}
}
