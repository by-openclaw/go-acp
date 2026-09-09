package tsl

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"dhs/internal/metrics"
	"dhs/internal/transport"
	"dhs/internal/tsl/codec"
)

// DefaultTCPKeepalivePeriod is the OS-layer SO_KEEPALIVE period on dialed
// TCP connections. TSL v5.0 over TCP carries no in-protocol keep-alive
// (verified empirically against VSM 2026-04-26 — 77 s of data flow with
// zero keep-alive frames), so the OS-layer probe is the dead-socket
// detector when the consumer goes away without sending FIN. It is the
// transport's default: the dialer opens sockets through the injected
// transport, so the period is the process's transport.Config (the
// `--keepalive` flag), not a value this package applies.
const DefaultTCPKeepalivePeriod = transport.DefaultTCPKeepalivePeriod

// tcpDialer maintains outbound TCP connections to v5.0 consumers (MVs).
// Per the TallyArbiter reference + Miranda emulator convention, the
// PRODUCER dials the consumer (MV listens on TCP). Connections are
// lazily established on first send; a failed send closes the connection
// and returns the error so the caller can retry.
type tcpDialer struct {
	// met counts what this dialer puts on the wire. Set by the Server at
	// construction; nil-safe so a dialer built by a test still works.
	met *metrics.Connector

	mu    sync.Mutex
	conns map[string]net.Conn // keyed by "host:port"

	// open opens each outbound connection: the owning provider's Base.Dial,
	// i.e. the injected transport, so the process owns the socket posture
	// (keepalive at transport.DefaultTCPKeepalivePeriod, TLS, source
	// address) and a test substitutes a fake Net. The connector never
	// decides how a socket is made.
	open dialFunc
}

// dialFunc is the shape of provider.Base.Dial / transport.Net.Dial.
type dialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

func newTCPDialer(met *metrics.Connector, dial dialFunc) *tcpDialer {
	return &tcpDialer{
		met:   met,
		conns: map[string]net.Conn{},
		open:  dial,
	}
}

// destKey formats a dest into the conns map key.
func destKey(host string, port int) string {
	return fmt.Sprintf("%s:%d", host, port)
}

// dial opens (or reuses) a TCP connection to the destination.
func (d *tcpDialer) dial(host string, port int) (net.Conn, error) {
	key := destKey(host, port)
	d.mu.Lock()
	defer d.mu.Unlock()
	if c, ok := d.conns[key]; ok {
		return c, nil
	}
	c, err := d.open(context.Background(), "tcp", key)
	if err != nil {
		return nil, fmt.Errorf("tsl v5.0 TCP dial %s: %w", key, err)
	}
	d.conns[key] = c
	return c, nil
}

// sendV50TCP writes a DLE/STX-wrapped v5.0 packet to one destination.
// On write error the connection is closed and dropped so the next send
// redials.
func (d *tcpDialer) sendV50TCP(host string, port int, p codec.V50Packet) error {
	start := time.Now() // send footprint: encode + dial + write
	packet, err := p.Encode()
	if err != nil {
		return fmt.Errorf("tsl v5.0 encode: %w", err)
	}
	wrapped := codec.EncodeDLEFrame(packet)

	c, err := d.dial(host, port)
	if err != nil {
		return err
	}
	if _, werr := c.Write(wrapped); werr == nil {
		if d.met != nil {
			d.met.ObserveTx(len(wrapped), time.Since(start))
		}
	} else {
		// Close + forget on write failure.
		d.mu.Lock()
		_ = c.Close()
		delete(d.conns, destKey(host, port))
		d.mu.Unlock()
		return fmt.Errorf("tsl v5.0 TCP write %s:%d: %w", host, port, werr)
	}
	return nil
}

// close shuts all active TCP connections.
func (d *tcpDialer) close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	var first error
	for k, c := range d.conns {
		if err := c.Close(); err != nil && first == nil {
			first = err
		}
		delete(d.conns, k)
	}
	return first
}
