package session

import (
	"context"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"dhs/internal/clock"
	"dhs/internal/metrics"
	"dhs/internal/plugin"
	"dhs/internal/snell-rollcall/codec"
)

// Every test in this package runs over net.Pipe against a fake peer, with time
// supplied by a fake clock. There is no socket, no port and no sleep anywhere:
// a timeout is tested by advancing the clock, which makes the result the same
// on a loaded Windows runner as on an idle laptop.

// peerAddr is the device the fake peer presents as.
var peerAddr = codec.Address{Unit: 0x20, Port: 0x00, Index: codec.IndexUnknown}

// peer is the other end of the pipe: it reads frames into a channel and writes
// whatever a test tells it to.
type peer struct {
	t    *testing.T
	conn net.Conn

	frames chan codec.Frame
	done   chan struct{}
	once   sync.Once
}

func newPeer(t *testing.T, conn net.Conn) *peer {
	t.Helper()
	p := &peer{
		t:      t,
		conn:   conn,
		frames: make(chan codec.Frame, 64),
		done:   make(chan struct{}),
	}
	go p.read()
	return p
}

func (p *peer) read() {
	r := codec.NewReader(p.conn)
	for {
		f, err := r.ReadFrame()
		if err != nil {
			close(p.frames)
			return
		}
		// The payload aliases the reader's buffer, so it must be copied
		// before being handed across a channel.
		f.Payload = append([]byte(nil), f.Payload...)
		select {
		case p.frames <- f:
		case <-p.done:
			return
		}
	}
}

// recv returns the next frame the peer received, failing the test if none
// arrives. The wait is a real one because it is bounded by the pipe rather
// than by protocol time; nothing under test is waiting on it.
func (p *peer) recv() codec.Frame {
	p.t.Helper()
	select {
	case f, ok := <-p.frames:
		if !ok {
			p.t.Fatal("peer: connection closed while waiting for a frame")
		}
		return f
	case <-time.After(2 * time.Second):
		p.t.Fatal("peer: no frame arrived")
		return codec.Frame{}
	}
}

// recvType waits for a frame and requires it to be of the given type.
func (p *peer) recvType(want codec.PacketType) codec.Frame {
	p.t.Helper()
	f := p.recv()
	if f.Type != want {
		p.t.Fatalf("peer received %s, want %s", f.Type, want)
	}
	return f
}

// quiet requires that nothing further arrived. Used to prove the
// one-in-flight rule: a second request must not be on the wire while the
// first is unanswered.
func (p *peer) quiet() {
	p.t.Helper()
	select {
	case f, ok := <-p.frames:
		if ok {
			p.t.Fatalf("peer received %s, but nothing should have been sent", f)
		}
	case <-time.After(50 * time.Millisecond):
	}
}

// send writes a frame back, addressed as a reply to req.
func (p *peer) send(req codec.Frame, typ codec.PacketType, payload []byte) {
	p.t.Helper()
	p.sendFlags(req, typ, 0, payload)
}

func (p *peer) sendFlags(req codec.Frame, typ codec.PacketType, flags uint8, payload []byte) {
	p.t.Helper()

	// A reply swaps the addresses: what was our source becomes its
	// destination, index included. That index is ours, which is how the link
	// finds the session again.
	f := codec.Frame{
		Dst:     req.Src,
		Src:     req.Dst,
		Type:    typ,
		Flags:   flags,
		Payload: payload,
	}
	if f.Src.Unit == 0 {
		f.Src = addrWithIndex(peerAddr, req.Dst.Index)
	}
	p.write(f)
}

// push sends an unsolicited back-channel message on a session.
func (p *peer) push(s *Session, typ codec.PacketType, payload []byte) {
	p.t.Helper()
	p.write(codec.Frame{
		Dst:     addrWithIndex(peerAddr, s.LocalIndex()),
		Src:     addrWithIndex(peerAddr, s.RemoteIndex()),
		Type:    typ,
		Flags:   codec.FlagBackChannel,
		Payload: payload,
	})
}

func (p *peer) write(f codec.Frame) {
	p.t.Helper()
	b, err := f.Encode()
	if err != nil {
		p.t.Fatalf("peer: encode %s: %v", f.Type, err)
	}
	if _, err := p.conn.Write(b); err != nil && err != io.ErrClosedPipe {
		p.t.Fatalf("peer: write: %v", err)
	}
}

func (p *peer) close() {
	p.once.Do(func() {
		close(p.done)
		_ = p.conn.Close()
	})
}

// harness is a Link with a fake peer and a fake clock.
type harness struct {
	link *Link
	peer *peer
	clk  *clock.Fake
	met  *metrics.Connector
}

func newHarness(t *testing.T, cfg Config) *harness {
	t.Helper()
	h, _ := newHarnessWith(t, cfg, false)
	return h
}

// newBlockedHarness is a harness whose writes can be made to fail while its
// reads go on blocking. That is a socket that has gone away without our end
// having noticed, which is the only way to reach the paths that answer into
// one without racing the read loop.
func newBlockedHarness(t *testing.T, cfg Config) (*harness, *blockedConn) {
	t.Helper()
	return newHarnessWith(t, cfg, true)
}

func newHarnessWith(t *testing.T, cfg Config, blocked bool) (*harness, *blockedConn) {
	t.Helper()

	ours, theirs := net.Pipe()

	var block *blockedConn
	if blocked {
		block = &blockedConn{Conn: ours}
		ours = block
	}
	clk := clock.NewFake(time.Time{})
	met := metrics.NewConnector()

	h := &harness{
		peer: newPeer(t, theirs),
		clk:  clk,
		met:  met,
	}
	h.link = NewLink(ours, cfg, plugin.Deps{
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Clock:   clk,
		Metrics: met,
	})

	t.Cleanup(func() {
		_ = h.link.Close()
		h.peer.close()
	})
	return h, block
}

// blockedConn fails writes on demand and leaves reads alone.
type blockedConn struct {
	net.Conn
	failing atomic.Bool
}

func (c *blockedConn) Write(b []byte) (int, error) {
	if c.failing.Load() {
		return 0, io.ErrClosedPipe
	}
	return c.Conn.Write(b)
}

// fire advances the fake clock once n timers are armed.
//
// Waiting for the timer to be armed is what makes this deterministic: the code
// under test registers its deadline on its own goroutine, and advancing before
// it does would fire nothing and the test would hang rather than fail.
//
// The count is exact rather than a minimum, which is only possible because
// nothing in this package leaves a timer armed after it is finished with. That
// is worth asserting: a leaked timer is the usual cost of using After in a
// retry loop.
func (h *harness) fire(t *testing.T, n int, d time.Duration) {
	t.Helper()
	h.armed(t, n)
	h.clk.Advance(d)
}

// armed waits until exactly n timers are armed.
func (h *harness) armed(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for h.clk.Waiters() != n {
		if time.Now().After(deadline) {
			t.Fatalf("%d timers armed, want %d", h.clk.Waiters(), n)
		}
		time.Sleep(time.Millisecond)
	}
}

// peerSessionIndex is the index the fake peer assigns to the next session it
// accepts. It deliberately does not match the client's, because the two are
// independent and a harness that made them equal would hide the mistake this
// package exists to prevent.
var peerSessionIndex int16 = 0x30

// callSession opens a session against the fake peer, answering the Call.
func (h *harness) callSession(t *testing.T, services codec.Service) *Session {
	t.Helper()

	type result struct {
		s   *Session
		err error
	}
	done := make(chan result, 1)
	go func() {
		s, err := Call(context.Background(), h.link, peerAddr, services,
			codec.LevelSupervisor, ClientIdentity("test", services))
		done <- result{s, err}
	}()

	req := h.peer.recvType(codec.MsgCall)

	// A real device answers from its own session index, which is what the
	// client must then address every later message to.
	peerSessionIndex++
	h.peer.write(codec.Frame{
		Dst:  req.Src,
		Src:  addrWithIndex(peerAddr, peerSessionIndex),
		Type: codec.MsgAck,
	})

	r := <-done
	if r.err != nil {
		t.Fatalf("Call: %v", r.err)
	}
	return r.s
}

// barrier waits until everything the peer has written so far has been
// dispatched.
//
// The link's read loop is sequential, so a frame that reaches the unsolicited
// channel proves every frame written before it has already been handled. It is
// addressed to no session and is a request rather than a reply, so it can
// never be mistaken for the answer to something in flight.
func (h *harness) barrier(t *testing.T) {
	t.Helper()
	h.peer.write(codec.Frame{
		Dst:  codec.Address{Index: codec.IndexUnknown},
		Src:  peerAddr,
		Type: codec.MsgGetStat,
	})
	select {
	case <-h.link.Unsolicited():
	case <-time.After(2 * time.Second):
		t.Fatal("the barrier frame was never dispatched")
	}
}

// bareSession builds a session with its channels in place but no Call behind
// it, for tests of the link's own bookkeeping. Registering a half-built
// Session would crash the link's shutdown, which only ever sees sessions that
// Call constructed.
func bareSession(l *Link) *Session {
	s := &Session{
		link:        l,
		remoteIndex: codec.IndexUnknown,
		pushes:      make(chan Push, 1),
	}
	s.front = newChannel("front", l.clk, l.cfg.replyTimeout(), l.cfg.maxStrikes())
	s.back = newChannel("back", l.clk, l.cfg.replyTimeout(), l.cfg.maxStrikes())
	return s
}

// deps builds the standard dependency set with a fake clock.
func testDeps(clk clock.Clock) plugin.Deps {
	return plugin.Deps{
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Clock:   clk,
		Metrics: metrics.NewConnector(),
	}
}
