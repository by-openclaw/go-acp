package tsl

import (
	"fmt"
	"net"
	"sync"
	"time"

	"acp/internal/tsl/codec"
)

// DefaultTCPKeepalivePeriod is the OS-layer SO_KEEPALIVE period applied
// to dialed TCP connections. TSL v5.0 over TCP carries no in-protocol
// keep-alive (verified empirically against VSM 2026-04-26 — 77 s of
// data flow with zero keep-alive frames), so the OS-layer probe is the
// dead-socket detector when the consumer goes away without sending FIN.
const DefaultTCPKeepalivePeriod = 30 * time.Second

// tcpDialer maintains outbound TCP connections to v5.0 consumers (MVs).
// Per the TallyArbiter reference + Miranda emulator convention, the
// PRODUCER dials the consumer (MV listens on TCP). Connections are
// lazily established on first send; a failed send closes the connection
// and returns the error so the caller can retry.
type tcpDialer struct {
	mu    sync.Mutex
	conns map[string]net.Conn // keyed by "host:port"
}

func newTCPDialer() *tcpDialer {
	return &tcpDialer{conns: map[string]net.Conn{}}
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
	c, err := net.Dial("tcp", key)
	if err != nil {
		return nil, fmt.Errorf("tsl v5.0 TCP dial %s: %w", key, err)
	}
	if tc, ok := c.(*net.TCPConn); ok {
		_ = tc.SetKeepAlive(true)
		_ = tc.SetKeepAlivePeriod(DefaultTCPKeepalivePeriod)
	}
	d.conns[key] = c
	return c, nil
}

// sendV50TCP writes a DLE/STX-wrapped v5.0 packet to one destination.
// On write error the connection is closed and dropped so the next send
// redials.
func (d *tcpDialer) sendV50TCP(host string, port int, p codec.V50Packet) error {
	packet, err := p.Encode()
	if err != nil {
		return fmt.Errorf("tsl v5.0 encode: %w", err)
	}
	wrapped := codec.EncodeDLEFrame(packet)

	c, err := d.dial(host, port)
	if err != nil {
		return err
	}
	if _, werr := c.Write(wrapped); werr != nil {
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
