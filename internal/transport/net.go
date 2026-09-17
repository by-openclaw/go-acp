package transport

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"syscall"
	"time"
)

// Net is the whole transport contract a connector sees.
//
// Three verbs, and every return type is stdlib: net.Conn, net.Listener,
// net.PacketConn. That is deliberate and it is the point of the interface.
// Go's net package already IS the transport abstraction — its Dialer and
// ListenConfig are property structs, its Conn/Listener/PacketConn are
// interfaces, and crypto/tls reuses them — so a connector written against
// stdlib types can be handed OUR implementation, a third-party one, or a
// fake, without a line changing inside it.
//
// What varies between protocols is not the verbs. It is the properties:
// TCP or UDP, unicast or multicast or broadcast, TLS or plaintext, which
// keepalive, which source address. Those live in [Config], chosen once where
// the process is wired, never inside a protocol package.
//
// A connector that holds a Net cannot open a socket any other way, which is
// what makes "no transport code in a protocol package" structural instead of
// a list of exceptions somebody has to keep draining.
type Net interface {
	// Dial opens a client connection. network is "tcp", "tcp4", "udp", …
	Dial(ctx context.Context, network, addr string) (net.Conn, error)

	// Listen binds for connection-oriented networks.
	Listen(ctx context.Context, network, addr string) (net.Listener, error)

	// ListenPacket binds for datagram networks. The returned PacketConn both
	// receives and sends, which is why UDP needs no separate client and
	// server type — one socket is both roles.
	ListenPacket(ctx context.Context, network, addr string) (net.PacketConn, error)
}

// Config is the property set behind a [Net]. The zero value is a plain
// socket: no TLS, no keepalive tuning, no special options.
type Config struct {
	// Timeout bounds a Dial's connect handshake. Zero leaves it to ctx.
	Timeout time.Duration

	// LocalAddr pins the source address of a Dial. Needed where the choice
	// of egress interface is semantic rather than incidental — an acp1
	// provider bound to a VIP must announce FROM that VIP, or several
	// emulators collapse into whichever address the kernel picks.
	LocalAddr net.Addr

	// KeepalivePeriod sets SO_KEEPALIVE on TCP. Zero means the transport
	// default; negative disables.
	KeepalivePeriod time.Duration

	// NoDelay disables Nagle. Set it for request/response control protocols
	// where a 40ms coalescing delay is a latency bug.
	NoDelay bool

	// ReuseAddr allows several processes to share a port. It is applied
	// BEFORE bind, the only window in which it has any effect.
	ReuseAddr bool

	// Broadcast permits sends to a broadcast address.
	Broadcast bool

	// Multicast makes ListenPacket join a group: addr IS the group address,
	// as net.ListenMulticastUDP expects.
	Multicast bool

	// Iface selects the interface for a multicast join. Nil lets the system
	// choose, which on a multi-homed host is rarely what anyone wanted.
	Iface *net.Interface

	// TLS, when non-nil and Enabled, wraps Dial in a TLS client and Listen
	// in a TLS server. Nil is plaintext.
	TLS *TLSOptions
}

// New returns the stdlib implementation of [Net] for cfg.
//
// "Our implementation" in the sense the dependency policy means it: if a
// third-party library ever does this better, it is swapped in here and no
// connector notices, because none of them names this type.
func New(cfg Config) Net { return stdNet{cfg: cfg} }

type stdNet struct{ cfg Config }

// control builds the pre-bind setsockopt hook, or nil when nothing needs
// setting — an unnecessary hook is a syscall per socket for no reason.
func (n stdNet) control() func(string, string, syscall.RawConn) error {
	if !n.cfg.ReuseAddr && !n.cfg.Broadcast {
		return nil
	}
	return func(_, _ string, c syscall.RawConn) error {
		var opErr error
		if err := udpRawControl(c, func(fd uintptr) {
			if n.cfg.ReuseAddr {
				if opErr = udpSetReuse(fd); opErr != nil {
					return
				}
			}
			if n.cfg.Broadcast {
				opErr = udpSetBcast(fd)
			}
		}); err != nil {
			return err
		}
		return opErr
	}
}

// clientTLS / serverTLS treat a nil TLS pointer as plaintext, so a connector
// that never mentions TLS gets a plain socket without writing a nil check.
func (n stdNet) clientTLS() (*tls.Config, error) {
	if n.cfg.TLS == nil {
		return nil, nil
	}
	return n.cfg.TLS.Client()
}

func (n stdNet) serverTLS() (*tls.Config, error) {
	if n.cfg.TLS == nil {
		return nil, nil
	}
	return n.cfg.TLS.Server()
}

// socketOptions is the post-connect half of the policy.
func (n stdNet) socketOptions() SocketOptions {
	return SocketOptions{KeepalivePeriod: n.cfg.KeepalivePeriod, NoDelay: n.cfg.NoDelay}
}

// Dial implements Net.
func (n stdNet) Dial(ctx context.Context, network, addr string) (net.Conn, error) {
	d := net.Dialer{Timeout: n.cfg.Timeout, LocalAddr: n.cfg.LocalAddr, Control: n.control()}
	conn, err := d.DialContext(ctx, network, addr)
	if err != nil {
		return nil, fmt.Errorf("%w: dial %s %s: %v", ErrDialFailed, network, addr, err)
	}
	// A socket option that fails to apply does not fail the dial: the
	// connection works either way, and refusing it would trade a detectable
	// problem for an outage.
	_ = ApplySocketOptions(conn, n.socketOptions())

	tcfg, err := n.clientTLS()
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if tcfg == nil {
		return conn, nil
	}
	tc := tls.Client(conn, tcfg)
	if err := tc.HandshakeContext(ctx); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("%w: tls handshake %s: %v", ErrDialFailed, addr, err)
	}
	return tc, nil
}

// Listen implements Net.
func (n stdNet) Listen(ctx context.Context, network, addr string) (net.Listener, error) {
	lc := net.ListenConfig{Control: n.control()}
	ln, err := lc.Listen(ctx, network, addr)
	if err != nil {
		return nil, fmt.Errorf("%w: listen %s %s: %v", ErrListenFailed, network, addr, err)
	}
	// Options are applied per accepted connection, not at bind: that is the
	// only place they can be applied at all, and it is the arm that also
	// covers a listener handed in from outside.
	tcfg, err := n.serverTLS()
	if err != nil {
		_ = ln.Close()
		return nil, err
	}
	var out net.Listener = &Listener{Listener: ln, opts: n.socketOptions()}
	if tcfg != nil {
		out = tls.NewListener(out, tcfg)
	}
	return out, nil
}

// ListenPacket implements Net.
func (n stdNet) ListenPacket(ctx context.Context, network, addr string) (net.PacketConn, error) {
	if n.cfg.Multicast {
		gaddr, err := net.ResolveUDPAddr(network, addr)
		if err != nil {
			return nil, fmt.Errorf("%w: resolve multicast group %s: %v", ErrListenFailed, addr, err)
		}
		// Group membership is not a setsockopt we can apply after bind, so
		// this path goes through the stdlib call that does both.
		pc, err := net.ListenMulticastUDP(network, n.cfg.Iface, gaddr)
		if err != nil {
			return nil, fmt.Errorf("%w: join %s on %s: %v", ErrListenFailed, addr, network, err)
		}
		return pc, nil
	}
	lc := net.ListenConfig{Control: n.control()}
	pc, err := lc.ListenPacket(ctx, network, addr)
	if err != nil {
		return nil, fmt.Errorf("%w: listen packet %s %s: %v", ErrListenFailed, network, addr, err)
	}
	return pc, nil
}
