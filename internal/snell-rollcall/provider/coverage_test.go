package rollcall

import (
	"context"
	"net"
	"testing"
	"time"

	"dhs/internal/clock"
	"dhs/internal/snell-rollcall/codec"
	"dhs/internal/snell-rollcall/session"
	"dhs/internal/transport"
)

// What is left after the wire tests are the paths a client cannot reach on
// purpose: a connection that dies mid-answer, a session whose link has already
// gone. They are reached here by calling the handler directly, which is what
// the read loop does anyway.

func TestFactoryBuildsAProvider(t *testing.T) {
	f := &Factory{}

	meta := f.Meta()
	if meta.Name != "rollcall" || meta.DefaultPort != DefaultPort {
		t.Errorf("meta = %+v", meta)
	}

	// What comes back is the registry's interface rather than our own type,
	// which is what the supervisor holds.
	p := f.New(testDeps(clock.NewFake(time.Time{})), testTree())
	if p == nil {
		t.Fatal("the factory built nothing")
	}
	if err := p.Stop(); err != nil {
		t.Errorf("stopping a provider that never bound: %v", err)
	}
}

func TestServeDefaultsToTheIPSharePort(t *testing.T) {
	fake := &recordingNet{}
	deps := testDeps(clock.NewFake(time.Time{}))
	deps.Net = fake

	p := New(deps, testTree())

	ctx, cancel := context.WithCancel(context.Background())
	errs := make(chan error, 1)
	go func() { errs <- p.Serve(ctx, "") }()

	waitForAddr(t, p)
	cancel()
	<-errs

	if fake.addr != ":2050" {
		t.Errorf("bound %q, want the IPShare port", fake.addr)
	}
}

func TestServeReportsAnAcceptThatFailsOnItsOwn(t *testing.T) {
	p := New(testDeps(clock.NewFake(time.Time{})), testTree())

	errs := make(chan error, 1)
	go func() { errs <- p.Serve(context.Background(), "127.0.0.1:0") }()

	waitForAddr(t, p)

	// The listener dies without the provider being asked to stop, which is
	// what a socket taken away underneath it looks like. That is an error to
	// report rather than a clean end.
	p.mu.Lock()
	ln := p.listener
	p.mu.Unlock()
	_ = ln.Close()

	if err := <-errs; err == nil {
		t.Fatal("Serve should report a listener that failed")
	}
	_ = p.Stop()
}

func TestAHandshakeIntoAConnectionThatHasGoneIsLogged(t *testing.T) {
	s, blocked := newServedFaulty(t, testTree())
	link := providerLink(t, s.p)

	blocked.failing.Store(true)

	// The client is not there any more. There is nothing to do about that and
	// nothing to tell, so it is logged and the read loop carries on.
	s.p.handshake(link, codec.Frame{})
}

func TestAnAcknowledgementIntoAConnectionThatHasGoneIsReported(t *testing.T) {
	s, blocked := newServedFaulty(t, testTree())
	sess := s.open(1, codec.SvcControl)
	server := serverSession(t, s.p, sess)

	blocked.failing.Store(true)

	// A back channel we could not acknowledge was not opened, and saying so is
	// what stops the caller believing it was.
	if err := s.p.backChannel(server, codec.Frame{
		Payload: []byte{codec.BackChannelEnable},
	}); err == nil {
		t.Error("acknowledging into a dead connection should report an error")
	}
}

func TestAnswersOnADeadConnectionAreLoggedNotPanicked(t *testing.T) {
	s := newServed(t, testTree())
	sess := s.open(1, codec.SvcControl|codec.SvcFile|codec.SvcMenus)

	server := serverSession(t, s.p, sess)
	link := providerLink(t, s.p)

	// The client is gone. Everything the handler does next has nowhere to
	// write, and none of it may take the read loop down with it.
	_ = s.cl.Close()

	req := codec.Frame{
		Dst:  addrOf(s.cl.RemoteAddress(), server.LocalIndex()),
		Src:  addrOf(s.cl.LocalAddress(), codec.IndexUnknown),
		Type: codec.MsgGetTime,
	}
	s.p.Request(server, req)
	s.p.handshake(link, req)
	s.p.answerUnsolicited(link, req, codec.MsgAck, nil)

	if err := s.p.backChannel(server, codec.Frame{
		Payload: []byte{codec.BackChannelDisable},
	}); err == nil {
		t.Error("acknowledging on a dead connection should report an error")
	}
}

func TestACallThatCannotBeAcknowledgedFails(t *testing.T) {
	s := newServed(t, testTree())
	link := providerLink(t, s.p)

	_ = s.cl.Close()

	// The acknowledgement is part of accepting: a call we cannot answer has
	// not been accepted, however willing we were.
	err := s.p.Call(link, codec.Frame{
		Dst: addrOf(s.cl.RemoteAddress(), codec.IndexUnknown),
		Src: addrOf(s.cl.LocalAddress(), 5),
	}, codec.Connect{Services: codec.SvcMenus, UserLevel: codec.LevelSupervisor})

	if err == nil {
		t.Error("accepting on a dead connection should fail")
	}
}

func TestRequestsOnALinkThatIsGoneAreRefused(t *testing.T) {
	s := newServed(t, testTree())
	sess := s.open(1, codec.SvcControl|codec.SvcFile|codec.SvcMenus)

	server := serverSession(t, s.p, sess)
	link := providerLink(t, s.p)

	// The connection has been torn down and the bookkeeping with it, but a
	// frame already in flight still arrives. Every path that needs the
	// bookkeeping has to say so rather than assume it is there.
	s.p.mu.Lock()
	delete(s.p.links, link)
	s.p.mu.Unlock()

	if err := s.p.fileRequest(server, codec.Frame{Type: codec.MsgFileDir}); err == nil {
		t.Error("a file request on a forgotten link should be refused")
	}
	if err := s.p.nextItem(server, codec.Frame{
		Type: codec.MsgGetNextPkt, Payload: codec.GetNext{}.AppendTo(nil),
	}); err == nil {
		t.Error("a fetch on a forgotten link should be refused")
	}
	if err := s.p.backChannel(server, codec.Frame{
		Payload: []byte{codec.BackChannelEnable},
	}); err == nil {
		t.Error("a back channel request on a forgotten link should be refused")
	}

	// A transfer opened with nowhere to remember it still answers: the block
	// header goes out, and the fetches that follow are refused one at a time.
	if err := s.p.beginTransfer(server, codec.MsgGetFunc, codec.MsgRetFunc, nil); err != nil {
		t.Errorf("opening a transfer with no bookkeeping: %v", err)
	}

	// The handshake has nothing to assign from, so it answers nothing rather
	// than assigning an address it cannot record.
	s.p.handshake(link, codec.Frame{})
}

func TestA16BitWriteToAReadOnlyLineIsRefused(t *testing.T) {
	s := newServed(t, testTree())
	slot, status := commandOf(t, s.p, "frame.card1.status")

	sess := s.open(slot, codec.SvcControl)
	payload, err := codec.FuncStatus{
		Command: uint16(status), Mode: codec.ModeValue, Value: 1,
	}.AppendTo(nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	if got := refused(t, sess, codec.MsgSetParam, payload); got.Type != codec.MsgNack {
		t.Errorf("answered %s, want Nack", got.Type)
	}
}

func TestAReadSizeOutsideTheBlockIsClamped(t *testing.T) {
	s := newServed(t, testTree())
	body := []byte("0123456789")
	s.p.AddFile("small.bin", body)

	sess := s.open(1, codec.SvcFile)
	handle := openFileOn(t, sess, "small.bin")

	for _, count := range []int16{0, -1, 32767} {
		req := codec.File{SrcHandle: 1, FileHandle: handle, Offset: 0, Extra: count}
		reply := do(t, sess, codec.MsgFileRead, req.AppendTo(nil))

		f, err := codec.DecodeFile(reply.Payload)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if int(f.Offset) != len(body) {
			t.Errorf("asking for %d bytes returned %d, want the whole file", count, f.Offset)
		}
	}
}

func TestFlushOfASlotWithNoCard(t *testing.T) {
	s := newServed(t, testTree())
	sess := s.open(0x40, codec.SvcControl|codec.SvcLongStr)

	// There is nothing to flush and nothing to fail: a client may subscribe to
	// a slot before a card is fitted.
	enableBackChannel(t, sess, codec.BackChannelEnable)

	if got := do(t, sess, codec.MsgKeepAlive, nil); got.Type != codec.MsgAck {
		t.Errorf("the session stopped working: %s", got.Type)
	}
}

func TestAFlushStopsWhenTheSubscriberGoes(t *testing.T) {
	s := newServed(t, testTree())
	sess := s.open(1, codec.SvcControl|codec.SvcLongStr)

	enableBackChannel(t, sess, codec.BackChannelFutureOnly)
	sub := subscriberOf(t, s.p, sess)

	// Fill the queue so a flush cannot finish, then close the subscription
	// underneath it. The flush has to notice rather than wait forever.
	for i := 0; i < pushQueue; i++ {
		s.p.enqueue(sub, pending{isValue: true, value: codec.Value{Command: 1}})
	}

	done := make(chan struct{})
	s.p.wg.Add(1)
	go func() {
		defer close(done)
		s.p.flush(sub)
	}()

	st := s.p.linkState(providerLink(t, s.p))
	st.setSubscribed(sub.s, false)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a flush kept waiting after its subscriber went")
	}
}

func TestAFlushStopsWhenTheProviderDoes(t *testing.T) {
	s := newServed(t, testTree())
	sess := s.open(1, codec.SvcControl|codec.SvcLongStr)

	enableBackChannel(t, sess, codec.BackChannelFutureOnly)
	sub := subscriberOf(t, s.p, sess)

	for i := 0; i < pushQueue; i++ {
		s.p.enqueue(sub, pending{isValue: true, value: codec.Value{Command: 1}})
	}

	done := make(chan struct{})
	s.p.wg.Add(1)
	go func() {
		defer close(done)
		s.p.flush(sub)
	}()

	_ = s.p.Stop()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a flush kept waiting after the provider stopped")
	}
}

func TestComplianceEventsAreCountedNotAccumulated(t *testing.T) {
	p := New(testDeps(clock.NewFake(time.Time{})), testTree())

	for i := 0; i < 5; i++ {
		p.fire(EventUnknownCommand, "again")
	}
	p.fire(EventPushDropped, "and another kind")

	events := p.ComplianceEvents()
	if len(events) != 2 {
		t.Fatalf("%d kinds recorded, want 2", len(events))
	}
	// One entry per kind with a count: a client that misbehaves on every one
	// of sixty-five thousand destinations must not fill memory with evidence.
	if events[0].Name != EventUnknownCommand || events[0].Count != 5 {
		t.Errorf("first event = %s x%d", events[0].Name, events[0].Count)
	}
	if events[1].Name != EventPushDropped || events[1].Count != 1 {
		t.Errorf("second event = %s x%d", events[1].Name, events[1].Count)
	}
}

// serverSession returns the provider's own half of a session the client opened.
//
// A round trip first: the acknowledgement that opened the session reaches the
// client before the handler that sent it has finished recording it, so a test
// that looks straight afterwards can look too early. One answered message
// proves the read loop has moved on, because it is the same goroutine.
func serverSession(t *testing.T, p *Provider, client *session.Session) *session.Session {
	t.Helper()

	if got := do(t, client, codec.MsgKeepAlive, nil); got.Type != codec.MsgAck {
		t.Fatalf("keepalive: %s", got.Type)
	}

	st := p.linkState(providerLink(t, p))
	st.mu.Lock()
	defer st.mu.Unlock()

	for _, s := range st.sessions {
		if s.LocalIndex() == client.RemoteIndex() {
			return s
		}
	}
	t.Fatal("the provider has no session matching the client's")
	return nil
}

// recordingNet is a transport that remembers what it was asked to bind.
type recordingNet struct{ addr string }

func (n *recordingNet) Dial(context.Context, string, string) (net.Conn, error) {
	return nil, net.ErrClosed
}

func (n *recordingNet) Listen(_ context.Context, network, addr string) (net.Listener, error) {
	n.addr = addr
	return net.Listen(network, "127.0.0.1:0")
}

func (n *recordingNet) ListenPacket(context.Context, string, string) (net.PacketConn, error) {
	return nil, net.ErrClosed
}

var _ transport.Net = (*recordingNet)(nil)
