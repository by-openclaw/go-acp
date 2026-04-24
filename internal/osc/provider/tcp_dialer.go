package osc

import (
	"fmt"
	"net"
	"sync"

	"acp/internal/osc/codec"
)

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

	mu    sync.Mutex
	conns map[string]net.Conn
}

func newTCPDialer(f framerKind) *tcpDialer {
	return &tcpDialer{framer: f, conns: map[string]net.Conn{}}
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
	c, err := net.Dial("tcp", key)
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
	if _, werr := c.Write(wire); werr != nil {
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
