package osc

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"dhs/internal/metrics"
	"dhs/internal/osc/codec"
	"dhs/internal/transport"
)

// DefaultTCPKeepalivePeriod is the OS-layer SO_KEEPALIVE period applied
// to dialed TCP connections. OSC over TCP carries no in-protocol keep-
// alive, so the OS-layer probe is the dead-socket detector.
const DefaultTCPKeepalivePeriod = 30 * time.Second

// framerKind — local to provider; must match the consumer's enum.
type framerKind int

const (
	framerLenPrefix framerKind = iota
	framerSLIP
)

// tcpDialer maintains outbound TCP connections to OSC peers. Per
// TallyArbiter + Miranda conventions for push protocols, the producer
// dials the consumer. Connections are lazily established on first send;
// a failed write closes + drops the connection so the next send redials.
type tcpDialer struct {
	framer framerKind

	// met counts what this dialer puts on the wire. Set by the Server at
	// construction; nil-safe so a dialer built by a test still works.
	met *metrics.Connector

	mu    sync.Mutex
	conns map[string]net.Conn

	// dialer opens each outbound connection. Injected rather than calling
	// net.Dial inline so the pipe is substitutable, and so SO_KEEPALIVE is
	// applied by the shared dialer instead of by a separate call here.
	dialer transport.Dialer
}

func newTCPDialer(f framerKind, met *metrics.Connector) *tcpDialer {
	return &tcpDialer{
		framer: f,
		met:    met,
		conns:  map[string]net.Conn{},
		dialer: transport.TCPDialer{
			Options: transport.SocketOptions{
				KeepalivePeriod: DefaultTCPKeepalivePeriod,
			},
		},
	}
}

func destKey(host string, port int) string {
	return fmt.Sprintf("%s:%d", host, port)
}

func (d *tcpDialer) dial(host string, port int) (net.Conn, error) {
	key := destKey(host, port)
	d.mu.Lock()
	defer d.mu.Unlock()
	if c, ok := d.conns[key]; ok {
		return c, nil
	}
	c, err := d.dialer.DialContext(context.Background(), "tcp", key)
	if err != nil {
		return nil, fmt.Errorf("osc tcp dial %s: %w", key, err)
	}
	d.conns[key] = c
	return c, nil
}

func (d *tcpDialer) writeFramed(host string, port int, packet []byte) error {
	var wire []byte
	switch d.framer {
	case framerLenPrefix:
		wire = codec.EncodeLenPrefix(packet)
	case framerSLIP:
		wire = codec.EncodeSLIP(packet)
	}
	c, err := d.dial(host, port)
	if err != nil {
		return err
	}
	if _, werr := c.Write(wire); werr == nil {
		if d.met != nil {
			d.met.ObserveTx(len(wire), 0)
		}
	} else {
		d.mu.Lock()
		_ = c.Close()
		delete(d.conns, destKey(host, port))
		d.mu.Unlock()
		return fmt.Errorf("osc tcp write %s:%d: %w", host, port, werr)
	}
	return nil
}

func (d *tcpDialer) sendMessage(host string, port int, m codec.Message) error {
	wire, err := m.Encode()
	if err != nil {
		return fmt.Errorf("osc encode message: %w", err)
	}
	return d.writeFramed(host, port, wire)
}

func (d *tcpDialer) sendBundle(host string, port int, b codec.Bundle) error {
	wire, err := b.Encode()
	if err != nil {
		return fmt.Errorf("osc encode bundle: %w", err)
	}
	return d.writeFramed(host, port, wire)
}

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
