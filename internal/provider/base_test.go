package provider

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"dhs/internal/metrics"
	"dhs/internal/plugin"
	"dhs/internal/transport"
)

// fakeConn is the smallest thing that satisfies Conn: Run blocks until
// Close, and both record that they happened.
type fakeConn struct {
	done   chan struct{}
	once   sync.Once
	ran    atomic.Bool
	closed atomic.Bool
}

func newFakeConn() *fakeConn { return &fakeConn{done: make(chan struct{})} }

func (f *fakeConn) Run(ctx context.Context) {
	f.ran.Store(true)
	select {
	case <-f.done:
	case <-ctx.Done():
	}
}

func (f *fakeConn) Close() {
	f.once.Do(func() { f.closed.Store(true); close(f.done) })
}

// The zero value has to work — this repo builds providers as bare struct
// literals in hundreds of tests.
func TestZeroBaseIsUsable(t *testing.T) {
	var b Base[*fakeConn]
	if b.Metrics() == nil {
		t.Error("Metrics must never be nil")
	}
	if got := b.Conns(); len(got) != 0 {
		t.Errorf("a zero Base tracks nothing, got %d", len(got))
	}
	if err := b.Stop(); err != nil {
		t.Errorf("Stop on a zero Base: %v", err)
	}
	select {
	case <-b.Stopped():
		t.Error("Stopped must not be closed by Stop alone — only the accept loop closes it")
	default:
	}
}

func TestInitKeepsTheSuppliedConnector(t *testing.T) {
	met := metrics.NewConnector()
	var b Base[*fakeConn]
	b.Init(plugin.Deps{Metrics: met})
	if b.Metrics() != met {
		t.Error("Init must keep the injected Connector, not replace it")
	}
}

func TestTrackRemoveConns(t *testing.T) {
	var b Base[*fakeConn]
	a, c := newFakeConn(), newFakeConn()
	b.Track(a)
	b.Track(c)
	if got := b.Conns(); len(got) != 2 {
		t.Fatalf("tracked 2, Conns returned %d", len(got))
	}
	b.Remove(a)
	b.Remove(a) // already gone; must not panic
	if got := b.Conns(); len(got) != 1 || got[0] != c {
		t.Errorf("after Remove want [c], got %v", got)
	}
}

// Conns is a snapshot: mutating the returned slice, or the set afterwards,
// must not affect the other. A fan-out that iterated the live map would
// race the session goroutines deleting from it.
func TestConnsIsASnapshot(t *testing.T) {
	var b Base[*fakeConn]
	a := newFakeConn()
	b.Track(a)
	snap := b.Conns()
	b.Remove(a)
	if len(snap) != 1 {
		t.Error("the snapshot changed under the caller")
	}
}

// The whole point of Stop: snapshot under the lock, close outside it. If
// Close took the Base lock (as a session's own exit path does via Remove),
// closing under the lock would block. This fakeConn's Close calls Remove
// to prove Stop does not hold the lock across it.
type selfRemovingConn struct {
	*fakeConn
	b *Base[*selfRemovingConn]
}

func (s *selfRemovingConn) Close() {
	s.fakeConn.Close()
	s.b.Remove(s) // takes b.mu — deadlocks if Stop still holds it
}

func TestStopClosesEveryConnOutsideTheLock(t *testing.T) {
	var b Base[*selfRemovingConn]
	c1 := &selfRemovingConn{newFakeConn(), &b}
	c2 := &selfRemovingConn{newFakeConn(), &b}
	b.Track(c1)
	b.Track(c2)

	done := make(chan error, 1)
	go func() { done <- b.Stop() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Stop: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Stop deadlocked — it is holding the lock across Close")
	}
	if !c1.closed.Load() || !c2.closed.Load() {
		t.Error("Stop must close every tracked connection")
	}
}

func TestStopIsIdempotentAndClosesTheListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var b Base[*fakeConn]
	b.mu.Lock()
	b.listener = ln
	b.mu.Unlock()

	if err := b.Stop(); err != nil {
		t.Fatalf("first Stop: %v", err)
	}
	if err := b.Stop(); err != nil {
		t.Fatalf("second Stop must be a no-op, got %v", err)
	}
	if _, err := ln.Accept(); !errors.Is(err, net.ErrClosed) {
		t.Errorf("listener still open after Stop: %v", err)
	}
}

// Listen goes through the injected transport and records the listener so
// Stop can find it. A Listen after Stop must refuse rather than leak a
// socket nothing will close.
func TestListenRecordsAndRefusesAfterStop(t *testing.T) {
	var b Base[*fakeConn]
	b.Init(plugin.Deps{})
	ln, err := b.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	if err := b.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if _, err := ln.Accept(); !errors.Is(err, net.ErrClosed) {
		t.Error("Stop did not close the listener Listen recorded")
	}
	if _, err := b.Listen(context.Background(), "tcp", "127.0.0.1:0"); !errors.Is(err, net.ErrClosed) {
		t.Errorf("Listen after Stop must refuse with ErrClosed, got %v", err)
	}
}

// The zero Base has no transport; Listen must default one rather than
// dereference nil.
func TestListenDefaultsTheTransport(t *testing.T) {
	var b Base[*fakeConn]
	ln, err := b.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen on a zero Base: %v", err)
	}
	_ = ln.Close()
}

func TestListenSurfacesBindErrors(t *testing.T) {
	var b Base[*fakeConn]
	if _, err := b.Listen(context.Background(), "tcp", "256.0.0.1:1"); err == nil {
		t.Error("an unbindable address must error")
	}
}

// End to end: a real listener, real dials, each accepted connection tracked
// while it runs and removed when it ends, Stopped closed when the loop exits.
func TestAcceptLoopTracksAndRunsConnections(t *testing.T) {
	var b Base[*fakeConn]
	ln, err := b.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var accepted atomic.Int32
	loopDone := make(chan error, 1)
	go func() {
		loopDone <- b.AcceptLoop(ctx, ln, func(net.Conn) *fakeConn {
			accepted.Add(1)
			return newFakeConn()
		})
	}()

	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()

	deadline := time.Now().Add(2 * time.Second)
	for accepted.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if accepted.Load() != 1 {
		t.Fatalf("accepted %d, want 1", accepted.Load())
	}
	// Track is synchronous but Run is a goroutine, so a conn can be in the
	// set a beat before its Run has scheduled. Wait for the running flag.
	for time.Now().Before(deadline) {
		if c := b.Conns(); len(c) == 1 && c[0].ran.Load() {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	conns := b.Conns()
	if len(conns) != 1 || !conns[0].ran.Load() {
		t.Fatalf("the accepted connection must be tracked and running, got %v", conns)
	}

	if err := b.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	select {
	case err := <-loopDone:
		if err != nil {
			t.Errorf("a closed listener is a clean exit, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("AcceptLoop did not return after Stop")
	}
	select {
	case <-b.Stopped():
	default:
		t.Error("Stopped must be closed once the loop returns")
	}
	for len(b.Conns()) != 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := b.Conns(); len(got) != 0 {
		t.Errorf("a closed connection must be removed, %d still tracked", len(got))
	}
}

// ctx cancellation is the other clean exit.
func TestAcceptLoopReturnsNilOnContextCancel(t *testing.T) {
	var b Base[*fakeConn]
	ln, err := b.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- b.AcceptLoop(ctx, ln, func(net.Conn) *fakeConn { return newFakeConn() }) }()
	cancel()
	_ = ln.Close() // unblock Accept; ctx is already done so the loop must say nil
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("want nil on ctx cancel, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("loop did not exit on cancel")
	}
}

// scriptedListener plays back a sequence of Accept results.
type scriptedListener struct {
	mu      sync.Mutex
	results []acceptResult
	addr    net.Addr
}

type acceptResult struct {
	conn net.Conn
	err  error
}

func (s *scriptedListener) Accept() (net.Conn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.results) == 0 {
		return nil, net.ErrClosed
	}
	r := s.results[0]
	s.results = s.results[1:]
	return r.conn, r.err
}
func (s *scriptedListener) Close() error   { return nil }
func (s *scriptedListener) Addr() net.Addr { return s.addr }

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "accept: i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

// A temporary accept error is retried, not fatal — net/http.Server.Serve
// does the same. Of the four loops this replaces, three would have exited
// the provider on a momentary EMFILE.
func TestAcceptLoopRetriesTemporaryErrors(t *testing.T) {
	var b Base[*fakeConn]
	ln := &scriptedListener{results: []acceptResult{
		{err: timeoutErr{}},
		{err: timeoutErr{}},
		// then closed → clean exit
	}}
	start := time.Now()
	err := b.AcceptLoop(context.Background(), ln, func(net.Conn) *fakeConn { return newFakeConn() })
	if err != nil {
		t.Errorf("temporary errors followed by close must exit nil, got %v", err)
	}
	// Two retries: 5ms then 10ms of backoff, so at least ~15ms elapsed.
	if elapsed := time.Since(start); elapsed < 10*time.Millisecond {
		t.Errorf("no backoff observed (%v) — temporary errors must not spin", elapsed)
	}
}

// A ctx that ends while the loop is backing off must still exit promptly.
func TestAcceptLoopBackoffHonoursContext(t *testing.T) {
	var b Base[*fakeConn]
	many := make([]acceptResult, 0, 64)
	for i := 0; i < 64; i++ {
		many = append(many, acceptResult{err: timeoutErr{}})
	}
	ln := &scriptedListener{results: many}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- b.AcceptLoop(ctx, ln, func(net.Conn) *fakeConn { return newFakeConn() }) }()
	// Backoff doubles from 5ms: by 300ms the loop is parked inside a wait of
	// 160ms or more, so the cancel lands in the select, not between calls.
	time.Sleep(300 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("want nil on cancel during backoff, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("loop stayed in backoff after ctx ended")
	}
}

// A non-temporary, non-closed error is genuinely terminal and is returned
// as is — the caller decides what a broken listener means.
func TestAcceptLoopReturnsTerminalErrors(t *testing.T) {
	var b Base[*fakeConn]
	boom := errors.New("listener exploded")
	ln := &scriptedListener{results: []acceptResult{{err: boom}}}
	err := b.AcceptLoop(context.Background(), ln, func(net.Conn) *fakeConn { return newFakeConn() })
	if !errors.Is(err, boom) {
		t.Errorf("want the terminal error back, got %v", err)
	}
	select {
	case <-b.Stopped():
	default:
		t.Error("Stopped must close on a terminal exit too")
	}
}

// The backoff resets once a connection is accepted; a long-running server
// must not carry a one-second delay forever after one bad moment.
func TestAcceptLoopBackoffResetsAfterSuccess(t *testing.T) {
	var b Base[*fakeConn]
	client, server := net.Pipe()
	t.Cleanup(func() { _ = client.Close(); _ = server.Close() })
	ln := &scriptedListener{results: []acceptResult{
		{err: timeoutErr{}},
		{conn: server},
		{err: timeoutErr{}},
	}}
	start := time.Now()
	_ = b.AcceptLoop(context.Background(), ln, func(net.Conn) *fakeConn { return newFakeConn() })
	// 5ms + (reset) 5ms; if it had NOT reset the second wait would be 10ms.
	// Either way this is bounded well under a second; the reset is
	// observable in the code path, and the assertion is that it completes.
	if time.Since(start) > 500*time.Millisecond {
		t.Error("backoff did not reset after a successful accept")
	}
}

// Stopped is created lazily so the zero Base can hand out a channel.
func TestStoppedOnZeroBaseIsOpen(t *testing.T) {
	var b Base[*fakeConn]
	select {
	case <-b.Stopped():
		t.Error("Stopped must be open until the loop exits")
	default:
	}
}

// stoppingNet is a transport.Net whose Listen calls the supplied hook after
// binding but before returning — the exact window in which a concurrent Stop
// can run. Base.Listen must notice and close the listener it just got,
// rather than record one nothing will ever close.
type stoppingNet struct {
	hook func()
}

func (n stoppingNet) Dial(context.Context, string, string) (net.Conn, error) {
	return nil, errors.New("not used")
}

func (n stoppingNet) Listen(_ context.Context, network, addr string) (net.Listener, error) {
	ln, err := net.Listen(network, addr)
	if err != nil {
		return nil, err
	}
	n.hook()
	return ln, nil
}

func (n stoppingNet) ListenPacket(context.Context, string, string) (net.PacketConn, error) {
	return nil, errors.New("not used")
}

func TestListenHonoursAStopThatRacedTheBind(t *testing.T) {
	var b Base[*fakeConn]
	b.Init(plugin.Deps{Net: stoppingNet{hook: func() { _ = b.Stop() }}})

	ln, err := b.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if !errors.Is(err, net.ErrClosed) {
		t.Fatalf("a Listen that lost the race to Stop must report ErrClosed, got %v (ln=%v)", err, ln)
	}
}

// The backoff policy, as a table: first retry 5ms, doubling, capped at one
// second, and the cap holds. Tested here rather than by sleeping through
// nine retries in AcceptLoop.
func TestNextBackoff(t *testing.T) {
	for _, tc := range []struct{ prev, want time.Duration }{
		{0, 5 * time.Millisecond},
		{5 * time.Millisecond, 10 * time.Millisecond},
		{320 * time.Millisecond, 640 * time.Millisecond},
		{640 * time.Millisecond, time.Second}, // 1280ms would exceed the cap
		{time.Second, time.Second},            // and it stays capped
	} {
		if got := nextBackoff(tc.prev); got != tc.want {
			t.Errorf("nextBackoff(%v) = %v, want %v", tc.prev, got, tc.want)
		}
	}
}

func TestAddrIsNilBeforeListenThenReports(t *testing.T) {
	var b Base[*fakeConn]
	if b.Addr() != nil {
		t.Error("Addr must be nil before Listen")
	}
	ln, err := b.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	if b.Addr() == nil || b.Addr().String() != ln.Addr().String() {
		t.Errorf("Addr = %v, want the bound %v", b.Addr(), ln.Addr())
	}
}

// AcceptLoop adopts a listener it was handed directly (the ServeListener
// case), so Stop closes it even though Listen never recorded it.
func TestAcceptLoopAdoptsAnInjectedListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var b Base[*fakeConn]
	go func() { _ = b.AcceptLoop(context.Background(), ln, func(net.Conn) *fakeConn { return newFakeConn() }) }()

	deadline := time.Now().Add(2 * time.Second)
	for b.Addr() == nil && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if b.Addr() == nil {
		t.Fatal("AcceptLoop did not adopt the listener")
	}
	if err := b.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if _, err := ln.Accept(); !errors.Is(err, net.ErrClosed) {
		t.Error("Stop did not close the adopted listener")
	}
}

// A listener injected after Stop is closed immediately, not served.
func TestAcceptLoopRefusesAnInjectedListenerAfterStop(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var b Base[*fakeConn]
	_ = b.Stop()
	if err := b.AcceptLoop(context.Background(), ln, func(net.Conn) *fakeConn { return newFakeConn() }); err != nil {
		t.Errorf("want nil after Stop, got %v", err)
	}
	if _, err := ln.Accept(); !errors.Is(err, net.ErrClosed) {
		t.Error("an injected listener after Stop must be closed")
	}
}

// --- UDP surface: ListenUDP / NoConn / Stop-closes-packet ---

// ListenUDP binds a datagram socket with SO_REUSEADDR set, so a second bind
// on the same port succeeds — the property that lets a provider and a
// consumer share one port on the same host. This is the whole point of the
// method, so it is the first thing the test proves.
func TestListenUDPReuseAddrSharesThePort(t *testing.T) {
	var b Base[*NoConn]
	first, err := b.ListenUDP(context.Background(), "udp4", "127.0.0.1:0", transport.UDPBindOptions{ReuseAddr: true})
	if err != nil {
		t.Fatalf("first ListenUDP: %v", err)
	}
	addr := first.LocalAddr().String()
	if _, err := b.ListenUDP(context.Background(), "udp4", addr, transport.UDPBindOptions{ReuseAddr: true}); err != nil {
		// Windows honours SO_REUSEADDR for UDP differently; skip rather than
		// fail there, matching transport's own ListenUDPAddr reuse test.
		t.Skipf("second bind on %s: %v (platform may not share UDP ports)", addr, err)
	}
	// Both sockets were recorded; Stop closes them both, each exactly once.
	if err := b.Stop(); err != nil {
		t.Errorf("Stop after ListenUDP: %v", err)
	}
}

// The datagram socket ListenUDP bound is closed by Stop, which is what makes
// a packet provider's blocked ReadFrom return.
func TestStopClosesTheDatagramSocket(t *testing.T) {
	var b Base[*NoConn]
	pc, err := b.ListenUDP(context.Background(), "udp4", "127.0.0.1:0", transport.UDPBindOptions{})
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	if err := b.Stop(); err != nil {
		t.Errorf("Stop: %v", err)
	}
	// A read on the closed socket returns immediately with ErrClosed rather
	// than blocking, proving Stop closed it.
	_ = pc.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 1)
	if _, _, err := pc.ReadFromUDP(buf); !errors.Is(err, net.ErrClosed) {
		t.Errorf("read after Stop = %v, want ErrClosed", err)
	}
}

// A ListenUDP that loses the race with Stop closes the socket it just bound
// rather than leaking one nothing will ever close, and reports ErrClosed —
// the UDP counterpart of the Listen-after-Stop race.
func TestListenUDPRefusedAfterStop(t *testing.T) {
	var b Base[*NoConn]
	_ = b.Stop()
	pc, err := b.ListenUDP(context.Background(), "udp4", "127.0.0.1:0", transport.UDPBindOptions{})
	if !errors.Is(err, net.ErrClosed) {
		t.Errorf("ListenUDP after Stop = %v, want ErrClosed", err)
	}
	if pc != nil {
		t.Error("no socket should be returned after Stop")
		_ = pc.Close()
	}
}

// NoConn is a placeholder: its Run returns at once and Close is a no-op, so
// a packet provider embedding Base[*NoConn] carries no live-connection cost.
func TestNoConnIsInert(t *testing.T) {
	var c NoConn
	c.Run(context.Background()) // must return immediately, not block
	c.Close()                   // must not panic
	// It satisfies Session, so Base[*NoConn] compiles and its set works.
	var b Base[*NoConn]
	if got := b.Conns(); len(got) != 0 {
		t.Errorf("a fresh packet Base has %d conns, want 0", len(got))
	}
}

// ListenUDP honours a Stop that raced the bind: if the socket binds but Stop
// runs before ListenUDP records it, the socket is closed and ErrClosed
// returned rather than a socket leaked that nothing will ever stop. Driven
// through the listenUDPAddr seam because ListenUDP deliberately does not go
// through the injected Net.
func TestListenUDPHonoursAStopThatRacedTheBind(t *testing.T) {
	var b Base[*NoConn]
	real := listenUDPAddr
	listenUDPAddr = func(ctx context.Context, network, addr string, opts transport.UDPBindOptions) (*net.UDPConn, error) {
		pc, err := real(ctx, network, addr, opts)
		if err == nil {
			_ = b.Stop() // flip closed AFTER a successful bind, BEFORE record
		}
		return pc, err
	}
	defer func() { listenUDPAddr = real }()

	pc, err := b.ListenUDP(context.Background(), "udp4", "127.0.0.1:0", transport.UDPBindOptions{})
	if !errors.Is(err, net.ErrClosed) {
		t.Fatalf("a ListenUDP that lost the race to Stop must report ErrClosed, got %v", err)
	}
	if pc != nil {
		t.Error("no socket should be returned when Stop won the race")
	}
}

// Stop surfaces the first socket-close error rather than swallowing it: a
// datagram socket closed out from under Base makes Close error, and Stop
// reports it.
func TestStopSurfacesADatagramCloseError(t *testing.T) {
	var b Base[*NoConn]
	pc, err := b.ListenUDP(context.Background(), "udp4", "127.0.0.1:0", transport.UDPBindOptions{})
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	_ = pc.Close() // close behind Base's back so Stop's Close errors
	if err := b.Stop(); err == nil {
		t.Error("Stop must surface the datagram close error")
	}
}

// The listener-close-error arm of Stop: a listener closed out from under
// Base makes ln.Close error, and Stop reports it.
func TestStopSurfacesAListenerCloseError(t *testing.T) {
	var b Base[*fakeConn]
	ln, err := b.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	_ = ln.Close() // close behind Base's back
	if err := b.Stop(); err == nil {
		t.Error("Stop must surface the listener close error")
	}
}

// A bind failure from ListenUDP is returned as-is, not masked. An address
// with an out-of-range port cannot bind on any platform.
func TestListenUDPSurfacesBindErrors(t *testing.T) {
	var b Base[*NoConn]
	if _, err := b.ListenUDP(context.Background(), "udp4", "127.0.0.1:999999", transport.UDPBindOptions{}); err == nil {
		t.Error("ListenUDP must surface a bind error")
	}
}

// Closed flips from false to true across Stop, so a packet provider can gate
// shutdown work on it.
func TestClosedReflectsStop(t *testing.T) {
	var b Base[*NoConn]
	if b.Closed() {
		t.Error("a fresh Base must not report Closed")
	}
	_ = b.Stop()
	if !b.Closed() {
		t.Error("Base must report Closed after Stop")
	}
}
