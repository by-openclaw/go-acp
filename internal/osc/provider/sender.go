package osc

import (
	"context"
	"dhs/internal/transport"
	"errors"
	"fmt"
	"net"
	"sync"

	"dhs/internal/osc/codec"
)

// udpSender owns the outbound UDP socket for OSC. SO_REUSEADDR lets
// multiple dhs producers share a local egress port; SO_BROADCAST lets
// the destination list include broadcast addresses like 255.255.255.255
// or subnet broadcasts (e.g. 192.168.1.255).
type udpSender struct {
	mu    sync.RWMutex
	conn  *net.UDPConn
	dests []*net.UDPAddr

	closeOnce sync.Once
}

func newUDPSender() *udpSender {
	return &udpSender{}
}

// bind opens a UDP socket for outbound sends. addr may be ":0" or ""
// for an ephemeral local port.
func (s *udpSender) bind(addr string) error {
	// One-time bind: hold the write lock across the nil-check + ListenPacket
	// + assignment so two concurrent binds cannot both open a socket. This is
	// the only place s.mu is held across I/O, and bind is not on any hot
	// path. Mirrors the tsl provider conn-locking fix.
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != nil {
		return errors.New("osc provider: already bound")
	}
	if addr == "" {
		addr = ":0"
	}
	conn, err := transport.ListenUDPAddr(context.Background(), "udp", addr,
		transport.UDPBindOptions{ReuseAddr: true, Broadcast: true})
	if err != nil {
		return fmt.Errorf("osc provider: bind %q: %w", addr, err)
	}
	s.conn = conn
	return nil
}

func (s *udpSender) boundAddr() *net.UDPAddr {
	s.mu.RLock()
	conn := s.conn
	s.mu.RUnlock()
	if conn == nil {
		return nil
	}
	return conn.LocalAddr().(*net.UDPAddr)
}

func (s *udpSender) addDest(host string, port int) error {
	addr := fmt.Sprintf("%s:%d", host, port)
	raddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return fmt.Errorf("osc provider: resolve dest %q: %w", addr, err)
	}
	s.mu.Lock()
	s.dests = append(s.dests, raddr)
	s.mu.Unlock()
	return nil
}

func (s *udpSender) destsSnapshot() []*net.UDPAddr {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*net.UDPAddr, len(s.dests))
	copy(out, s.dests)
	return out
}

func (s *udpSender) sendBytes(payload []byte) error {
	// Capture the conn pointer under the read lock, then release it before
	// the network writes — the lock never spans I/O. destsSnapshot takes its
	// own lock separately.
	s.mu.RLock()
	conn := s.conn
	s.mu.RUnlock()
	if conn == nil {
		return errors.New("osc provider: not bound")
	}
	dests := s.destsSnapshot()
	if len(dests) == 0 {
		return errors.New("osc provider: no destinations configured")
	}
	var firstErr error
	for _, d := range dests {
		if _, err := conn.WriteToUDP(payload, d); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("write to %s: %w", d.String(), err)
		}
	}
	return firstErr
}

func (s *udpSender) close() error {
	var err error
	s.closeOnce.Do(func() {
		s.mu.RLock()
		conn := s.conn
		s.mu.RUnlock()
		if conn != nil {
			err = conn.Close()
		}
	})
	return err
}

func (s *udpSender) serveBlock(ctx context.Context, addr string) error {
	// Read the bound state under the lock, then RELEASE it before calling
	// bind (which acquires s.mu itself — avoids RWMutex re-entrancy) and
	// before the blocking ctx wait. bind re-checks s.conn==nil under the
	// write lock, so the read here is purely an optimization that preserves
	// the "already bound -> don't rebind" behaviour.
	s.mu.RLock()
	bound := s.conn != nil
	s.mu.RUnlock()
	if !bound {
		if err := s.bind(addr); err != nil {
			return err
		}
	}
	<-ctx.Done()
	return nil
}

func (s *udpSender) sendMessage(m codec.Message) error {
	wire, err := m.Encode()
	if err != nil {
		return fmt.Errorf("osc encode message: %w", err)
	}
	return s.sendBytes(wire)
}

func (s *udpSender) sendBundle(b codec.Bundle) error {
	wire, err := b.Encode()
	if err != nil {
		return fmt.Errorf("osc encode bundle: %w", err)
	}
	return s.sendBytes(wire)
}
