package provider

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"

	"dhs/internal/clock"
	"dhs/internal/metrics"
	"dhs/internal/plugin"
	"dhs/internal/transport"
)

// Session is the constraint on Base's type parameter: a Conn that can be a
// map key. Every concrete session here is a pointer, which is comparable
// by identity — exactly what a set of live connections wants.
type Session interface {
	Conn
	comparable
}

// Conn is what Base needs from a connector's per-connection state.
//
// Every TCP provider here already has it — a goroutine that reads until the
// peer goes away, and a way to make the peer go away — under the unexported
// names run and close. Naming the pair is what lets one accept loop and one
// Stop serve every connector instead of each rewriting both.
type Conn interface {
	// Run blocks until the connection is finished, for whatever reason.
	Run(ctx context.Context)
	// Close ends the connection. Idempotent: Stop may call it on a session
	// that is already closing itself.
	Close()
}

// NoConn is the placeholder session type for packet providers.
//
// UDP providers bind one socket and either push (osc, tsl) or dispatch each
// datagram inline (acp1); none has a per-connection session to track. They
// still embed the SAME Base — as Base[*NoConn] — so metrics, Init, Stop,
// Stopped and the UDP bind (ListenUDP) come from one contract, not a second
// base. The connection set stays empty because AcceptLoop is never called.
type NoConn struct{}

// Run and Close satisfy Conn so *NoConn satisfies Session. Neither is ever
// invoked: a packet provider never accepts a connection to run or close.
func (*NoConn) Run(context.Context) {}

// Close is a no-op; see Run.
func (*NoConn) Close() {}

// Base is the half of a TCP provider that is not its protocol: the
// listener, the set of live connections, the stop sequence, and the
// counters.
//
// Four providers carried all of it — acp2, emberplus, probel-sw08p and
// probel-sw02p — as one algorithm in three idioms. The two probel copies were
// byte-for-byte identical. emberplus retried transient accept errors where
// the others returned. acp2 closed every session while still holding the
// server mutex, and reached into sess.conn to do it because its session had
// no close of its own. None of that variation was intended; it is what
// happens when the same loop is written four times.
//
// S is the connector's own session type, so the connection set stays typed:
// eleven fan-out sites in the repo iterate it, and a []Conn would have cost
// each of them a type assertion.
//
// Embedded BY VALUE, and the zero value works, for the same reason as
// consumer.Base: this repo builds providers as bare struct literals in
// hundreds of tests, and an embedded pointer would be nil in every one.
type Base[S Session] struct {
	mu       sync.Mutex
	listener net.Listener
	packet   []*net.UDPConn
	conns    map[S]struct{}
	closed   bool
	stopped  chan struct{}
	stopOnce sync.Once

	net     transport.Net
	metrics *metrics.Connector
	clk     clock.Clock
}

// Init wires the injected dependency set. Called once from the factory.
func (b *Base[S]) Init(deps plugin.Deps) {
	deps = deps.WithDefaults()
	b.mu.Lock()
	defer b.mu.Unlock()
	b.net = deps.Net
	b.metrics = deps.Metrics
	b.clk = deps.Clock
}

// Clock returns the injected clock — every wait, deadline and timestamp in
// a provider goes through it so a test drives time instead of sleeping.
// Never nil: a Base that was never Init'ed answers with the system clock.
func (b *Base[S]) Clock() clock.Clock {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.clk == nil {
		b.clk = clock.System()
	}
	return b.clk
}

// Metrics returns the provider's counter set, satisfying the optional
// interface cmd/dhs type-asserts for --metrics-addr. Never nil.
func (b *Base[S]) Metrics() *metrics.Connector {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.metrics == nil {
		b.metrics = metrics.NewConnector()
	}
	return b.metrics
}

// Listen binds through the injected transport and records the listener so
// Stop can close it. The only way a provider built on Base opens a socket.
func (b *Base[S]) Listen(ctx context.Context, network, addr string) (net.Listener, error) {
	b.mu.Lock()
	n := b.net
	if n == nil {
		n = transport.New(transport.Config{})
		b.net = n
	}
	closed := b.closed
	b.mu.Unlock()
	if closed {
		return nil, net.ErrClosed
	}

	ln, err := n.Listen(ctx, network, addr)
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	// Stop may have run while we were binding. Honour it rather than
	// leaving a listener nothing will ever close.
	if b.closed {
		b.mu.Unlock()
		_ = ln.Close()
		return nil, net.ErrClosed
	}
	b.listener = ln
	b.mu.Unlock()
	return ln, nil
}

// listenUDPAddr is transport.ListenUDPAddr, indirected through a package
// var so a test can drive the Stop-races-the-bind branch deterministically
// (the same reason transport itself indirects its raw-socket calls). It is
// never reassigned in production.
var listenUDPAddr = transport.ListenUDPAddr

// ListenUDP binds a datagram socket, the UDP analogue of Listen. It goes
// through transport.ListenUDPAddr so the socket policy — SO_REUSEADDR, and
// SO_BROADCAST when opts asks — is applied in the pre-bind Control window,
// the only window in which SO_REUSEADDR takes effect. That is what lets a
// provider and a consumer share one port on the same host, and what lets a
// provider bind a port a controller already holds.
//
// It does NOT go through the injected transport.Net: WithDefaults hands a
// connector a Net built from an empty Config (no ReuseAddr/Broadcast), so
// delegating there would silently drop the socket policy this method exists
// to guarantee. UDP loopback binds are deterministic, so tests exercise the
// real path rather than a fake Net.
//
// The socket is recorded so Stop closes it, and a Stop that already ran
// wins — the same race Listen handles: bind, then re-check under the lock
// and close rather than leak a socket nothing will ever stop.
func (b *Base[S]) ListenUDP(ctx context.Context, network, addr string, opts transport.UDPBindOptions) (*net.UDPConn, error) {
	b.mu.Lock()
	closed := b.closed
	b.mu.Unlock()
	if closed {
		return nil, net.ErrClosed
	}

	pc, err := listenUDPAddr(ctx, network, addr, opts)
	if err != nil {
		return nil, err
	}

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		_ = pc.Close()
		return nil, net.ErrClosed
	}
	b.packet = append(b.packet, pc)
	b.mu.Unlock()
	return pc, nil
}

// Addr is the address the listener is bound to, or nil before Listen. A
// server given ":0" reports the port the OS actually chose here — the only
// way a caller (or a test) learns where to connect.
func (b *Base[S]) Addr() net.Addr {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.listener == nil {
		return nil
	}
	return b.listener.Addr()
}

// Closed reports whether Stop has been called. A packet provider that gates
// spontaneous work on shutdown checks this: acp1 suppresses broadcast
// announces once the socket is closing, since a write racing the close only
// produces a shutdown-noise warning. Connection providers observe shutdown
// through the listener close and rarely need it.
func (b *Base[S]) Closed() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closed
}

// Stopped is closed once the accept loop has returned. Connectors with
// background goroutines of their own (emberplus's streamer) select on it.
func (b *Base[S]) Stopped() <-chan struct{} {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.stoppedLocked()
}

// stoppedLocked lazily creates the channel so the zero Base works. Caller
// holds b.mu.
func (b *Base[S]) stoppedLocked() chan struct{} {
	if b.stopped == nil {
		b.stopped = make(chan struct{})
	}
	return b.stopped
}

// Track adds a live connection to the set. Remove is the twin.
func (b *Base[S]) Track(c S) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.conns == nil {
		b.conns = map[S]struct{}{}
	}
	b.conns[c] = struct{}{}
}

// Remove drops a connection from the set. Safe for one already removed.
func (b *Base[S]) Remove(c S) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.conns, c)
}

// Conns snapshots the live set for fan-out. A copy, taken under the lock,
// so a broadcast never holds the lock across a write to a peer — and never
// iterates a map a session goroutine is deleting from.
func (b *Base[S]) Conns() []S {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]S, 0, len(b.conns))
	for c := range b.conns {
		out = append(out, c)
	}
	return out
}

// Backoff policy for a temporary accept error: start at 5ms, double, cap at
// one second — the same shape and ceiling as net/http.Server.Serve.
const (
	acceptBackoffMin = 5 * time.Millisecond
	acceptBackoffMax = time.Second
)

// nextBackoff is the policy as a pure function, so it is tested as a table
// rather than by sleeping through it. prev is the previous delay, 0 for the
// first retry after a success.
func nextBackoff(prev time.Duration) time.Duration {
	if prev == 0 {
		return acceptBackoffMin
	}
	if next := prev * 2; next < acceptBackoffMax {
		return next
	}
	return acceptBackoffMax
}

// AcceptLoop accepts on ln until it is closed or ctx ends, handing each
// connection to accept and running the result on its own goroutine.
//
// The socket policy is applied here, per connection, rather than at bind:
// a listener injected by a test — ServeListener, listenHook — gets it too.
//
// On a transient accept error it backs off and retries, following
// net/http.Server.Serve. Of the four loops this replaces, one retried and
// three returned; a provider that has been up for months should not exit
// because the host ran out of file descriptors for a moment. A closed
// listener or a finished ctx returns nil; any other terminal error is
// returned as is. Either way Stopped is closed on the way out.
func (b *Base[S]) AcceptLoop(ctx context.Context, ln net.Listener, accept func(net.Conn) S) error {
	defer b.stopOnce.Do(func() {
		b.mu.Lock()
		close(b.stoppedLocked())
		b.mu.Unlock()
	})

	// Adopt the listener so Stop closes it, whether it came from Listen or
	// was injected by a ServeListener seam. A Stop that already ran wins:
	// close this listener rather than serve on one nothing will stop.
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		_ = ln.Close()
		return nil
	}
	b.listener = ln
	b.mu.Unlock()

	var delay time.Duration
	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) || ctx.Err() != nil {
				return nil
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				delay = nextBackoff(delay)
				select {
				case <-time.After(delay):
				case <-ctx.Done():
					return nil
				}
				continue
			}
			return err
		}
		delay = 0

		// OS-level dead-peer probe. Without it a half-open client session
		// — a NAT or firewall drop with no RST — holds a goroutine and a
		// socket for ever.
		_ = transport.ApplySocketOptions(conn, transport.SocketOptions{})

		c := accept(conn)
		b.Track(c)
		go func() {
			defer b.Remove(c)
			c.Run(ctx)
		}()
	}
}

// Stop closes the listener and every live connection. Safe to call more
// than once, and before Serve has run.
//
// The set is snapshotted under the lock and closed OUTSIDE it. Closing a
// connection makes its goroutine exit, and that goroutine's exit path takes
// this same lock to remove itself — holding the lock across the close does
// not deadlock, because Stop releases on return, but it queues every session
// goroutine on the mutex for the duration and makes the lock's hold time a
// function of peer I/O. acp2 did exactly that.
func (b *Base[S]) Stop() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	ln := b.listener
	packet := b.packet
	b.packet = nil
	conns := make([]S, 0, len(b.conns))
	for c := range b.conns {
		conns = append(conns, c)
	}
	b.mu.Unlock()

	for _, c := range conns {
		c.Close()
	}
	// Close every datagram socket ListenUDP bound. A packet provider's read
	// loop is blocked in ReadFrom on one of these; closing it is what makes
	// that loop return, the UDP counterpart of closing the listener.
	var firstErr error
	for _, pc := range packet {
		if err := pc.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if ln != nil {
		if err := ln.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
