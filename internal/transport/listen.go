package transport

// TCP listening, and the socket options that go with it, in ONE place.
//
// Every connector that accepts TCP wrote its own accept loop, and each one
// made its own decision about SO_KEEPALIVE — the OS-level dead-peer probe
// that is the only thing standing between a half-open inbound session and a
// goroutine plus a socket held for ever. An audit of the seven accept paths:
//
//	osc/consumer            SO_KEEPALIVE 30s
//	tsl/consumer            SO_KEEPALIVE 30s
//	acp1/provider (tcp)     NoDelay only — no keepalive
//	acp1/provider (an2)     NoDelay only — no keepalive
//	acp2/provider           nothing
//	emberplus/provider      nothing
//	probel-sw02p/provider   nothing
//	probel-sw08p/provider   nothing
//
// Two out of eight. The same probe is also hand-copied on the dialling side
// in osc/provider, tsl/provider, probel-sw02p/codec and probel-sw08p/codec —
// four more copies, two of them carrying their own test seam.
//
// That is a transport concern, not a per-protocol one: proving it once here
// means every connector inherits the same behaviour, and a future change
// (a different period, a platform quirk) is one edit rather than twelve.
//
// The two probel copies stay where they are on purpose: codec/ is
// stdlib-only and must never import dhs/* (ADR-0006), so they cannot reach
// this package. That is the one legitimate exception.

import (
	"context"
	"fmt"
	"net"
	"time"
)

// DefaultTCPKeepalivePeriod is the SO_KEEPALIVE period applied when a caller
// does not name one. 30 s matches what osc and tsl already used and gives
// three probes inside the 90 s liveness window the fleet treats as stale.
const DefaultTCPKeepalivePeriod = 30 * time.Second

// DisableKeepalive is the sentinel that turns SO_KEEPALIVE off, as distinct
// from 0, which means "use the default". Mirrors consumer.DisableInterval.
const DisableKeepalive = -1 * time.Nanosecond

// ListenOptions are the socket options applied to every connection a
// Listener accepts. The zero value is the sensible default: keepalive on at
// DefaultTCPKeepalivePeriod, Nagle left alone.
//
// Named to sit beside ws.DialOptions / ws.AcceptOptions and TLSOptions.
type ListenOptions struct {
	// KeepalivePeriod sets SO_KEEPALIVE. 0 ⇒ DefaultTCPKeepalivePeriod;
	// DisableKeepalive (or any negative value) ⇒ off.
	KeepalivePeriod time.Duration

	// NoDelay disables Nagle on accepted connections. Wanted by protocols
	// whose messages are small and latency-sensitive (ACP1 frames are
	// ≤ 141 bytes); pointless for bulk streams.
	NoDelay bool
}

// ApplyTCPKeepalive turns SO_KEEPALIVE on for c with the given period.
//
// A period of 0 means DefaultTCPKeepalivePeriod; a negative period turns
// keepalive off. A connection that is not a *net.TCPConn — net.Pipe in a
// test, a TLS wrapper, a Unix socket — is left alone and reported as no
// error, so a caller can apply this unconditionally.
func ApplyTCPKeepalive(c net.Conn, period time.Duration) error {
	tc, ok := c.(*net.TCPConn)
	if !ok {
		return nil
	}
	if period < 0 {
		if err := tc.SetKeepAlive(false); err != nil {
			return fmt.Errorf("%w: disable keepalive: %v", ErrListenFailed, err)
		}
		return nil
	}
	if period == 0 {
		period = DefaultTCPKeepalivePeriod
	}
	if err := tcpSetKeepAlive(tc, true); err != nil {
		return fmt.Errorf("%w: enable keepalive: %v", ErrListenFailed, err)
	}
	if err := tcpSetKeepAlivePeriod(tc, period); err != nil {
		return fmt.Errorf("%w: keepalive period %s: %v", ErrListenFailed, period, err)
	}
	return nil
}

// ApplyListenOptions applies opts to one accepted or dialled connection.
// Exported so a connector that already owns its listener — acp1's servers
// need *net.TCPConn from AcceptTCP — can still share the socket policy
// without giving up its accept loop.
//
// Errors are returned, not logged: setting an option on a socket the peer
// has already reset is not worth failing a session over, and only the
// caller knows whether it cares.
func ApplyListenOptions(c net.Conn, opts ListenOptions) error {
	if err := ApplyTCPKeepalive(c, opts.KeepalivePeriod); err != nil {
		return err
	}
	if opts.NoDelay {
		if tc, ok := c.(*net.TCPConn); ok {
			if err := tcpSetNoDelay(tc, true); err != nil {
				return fmt.Errorf("%w: nodelay: %v", ErrListenFailed, err)
			}
		}
	}
	return nil
}

// Test seams. Some failure arms above cannot be provoked on a real socket:
// any fd state that fails SetKeepAlivePeriod also fails SetKeepAlive, and
// any that fails SetNoDelay also fails both, so the earlier call always
// returns first. Same transparent-seam pattern probel-sw02p/codec uses for
// exactly the same reason.
var (
	tcpSetKeepAlive       = func(tc *net.TCPConn, on bool) error { return tc.SetKeepAlive(on) }
	tcpSetKeepAlivePeriod = func(tc *net.TCPConn, d time.Duration) error { return tc.SetKeepAlivePeriod(d) }
	tcpSetNoDelay         = func(tc *net.TCPConn, on bool) error { return tc.SetNoDelay(on) }
	applyListenOptions    = ApplyListenOptions
)

// Listener is a net.Listener whose Accept has already applied ListenOptions
// to the connection it returns. Callers keep their own accept loop and their
// own session handling; only the socket policy moves here.
//
// Use this where the connector OWNS its bind (tsl and osc consumers). Where a
// listener can be handed in from outside — the ServeListener / listenHook
// seams on the emberplus and probel providers — call ApplyListenOptions in
// the accept loop instead, so an injected listener gets the same policy.
type Listener struct {
	net.Listener
	opts ListenOptions
}

// ListenTCP binds addr and returns a Listener that applies opts to every
// accepted connection. network is "tcp", "tcp4" or "tcp6" — connectors that
// bind IPv4-only (acp1, acp2) pass "tcp4" and keep that behaviour.
func ListenTCP(ctx context.Context, network, addr string, opts ListenOptions) (*Listener, error) {
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, network, addr)
	if err != nil {
		return nil, fmt.Errorf("%w: listen %s %s: %v", ErrListenFailed, network, addr, err)
	}
	return &Listener{Listener: ln, opts: opts}, nil
}

// Accept returns the next connection with the socket options applied.
//
// An option that fails to apply does NOT fail the Accept: the connection is
// usable either way, and refusing a session because the kernel declined a
// keepalive tweak would trade a detectable problem for an outright outage.
func (l *Listener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	_ = applyListenOptions(c, l.opts)
	return c, nil
}
