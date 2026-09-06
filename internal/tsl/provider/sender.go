package tsl

import (
	"context"
	"dhs/internal/transport"
	"errors"
	"fmt"
	"net"
	"sync"

	"dhs/internal/tsl/codec"
)

// udpSender is shared across versions. It owns a single outbound UDP
// socket bound to a local (optional) address and fans frames out to a
// configurable set of destinations.
type udpSender struct {
	mu    sync.RWMutex
	conn  *net.UDPConn
	dests []*net.UDPAddr

	closeOnce sync.Once
}

func newUDPSender() *udpSender {
	return &udpSender{}
}

// bind opens a UDP socket for outbound sends. addr may be empty (or
// "0.0.0.0:0") for an ephemeral local port.
//
// SO_REUSEADDR is set so multiple dhs producers can coexist on the same
// local port. SO_BROADCAST is set so sends to the limited broadcast
// address 255.255.255.255 or subnet broadcasts (e.g. 192.168.1.255) are
// accepted by the kernel — matches the ACP1 producer contract.
func (s *udpSender) bind(addr string) error {
	// One-time bind: hold the write lock across the nil-check + ListenPacket
	// + assignment so two concurrent binds cannot both open a socket. This
	// is the only place s.mu is held across I/O, and bind is not on any
	// hot path.
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != nil {
		return errors.New("tsl provider: already bound")
	}
	if addr == "" {
		addr = ":0"
	}
	conn, err := transport.ListenUDPAddr(context.Background(), "udp", addr,
		transport.UDPBindOptions{ReuseAddr: true, Broadcast: true})
	if err != nil {
		return fmt.Errorf("tsl provider: bind %q: %w", addr, err)
	}
	s.conn = conn
	return nil
}

// boundAddr returns the actual local address (ephemeral resolution).
func (s *udpSender) boundAddr() *net.UDPAddr {
	s.mu.RLock()
	conn := s.conn
	s.mu.RUnlock()
	if conn == nil {
		return nil
	}
	return conn.LocalAddr().(*net.UDPAddr)
}

// addDest registers a destination (host:port).
func (s *udpSender) addDest(host string, port int) error {
	addr := fmt.Sprintf("%s:%d", host, port)
	raddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return fmt.Errorf("tsl provider: resolve dest %q: %w", addr, err)
	}
	s.mu.Lock()
	s.dests = append(s.dests, raddr)
	s.mu.Unlock()
	return nil
}

// destsSnapshot returns a copy of current destinations.
func (s *udpSender) destsSnapshot() []*net.UDPAddr {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*net.UDPAddr, len(s.dests))
	copy(out, s.dests)
	return out
}

// sendBytes writes payload to every configured destination. Returns the
// first error encountered but continues sending to the rest.
func (s *udpSender) sendBytes(payload []byte) error {
	// Capture the conn pointer under the read lock, then release it before
	// the network writes — the lock never spans I/O. destsSnapshot takes
	// its own lock separately.
	s.mu.RLock()
	conn := s.conn
	s.mu.RUnlock()
	if conn == nil {
		return errors.New("tsl provider: not bound")
	}
	dests := s.destsSnapshot()
	if len(dests) == 0 {
		return errors.New("tsl provider: no destinations configured")
	}
	var firstErr error
	for _, d := range dests {
		if _, err := conn.WriteToUDP(payload, d); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("write to %s: %w", d.String(), err)
		}
	}
	return firstErr
}

// close shuts the socket. Safe to call multiple times.
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

// serveBlock is the shared Serve body — binds (if addr set) and blocks
// on ctx. Version-specific Server.Serve wraps this.
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

// encodeAndSendV31 encodes a v3.1 frame and fans it out to destinations.
func (s *udpSender) encodeAndSendV31(f codec.V31Frame) error {
	payload, err := f.Encode()
	if err != nil {
		return fmt.Errorf("tsl v3.1 encode: %w", err)
	}
	return s.sendBytes(payload)
}

// encodeAndSendV40 encodes a v4.0 frame and fans it out.
func (s *udpSender) encodeAndSendV40(f codec.V40Frame) error {
	payload, err := f.Encode()
	if err != nil {
		return fmt.Errorf("tsl v4.0 encode: %w", err)
	}
	return s.sendBytes(payload)
}

// encodeAndSendV50UDP encodes a v5.0 packet and fans it out via UDP.
// No DLE/STX wrapper (UDP is self-framed per spec §Phy).
func (s *udpSender) encodeAndSendV50UDP(p codec.V50Packet) error {
	payload, err := p.Encode()
	if err != nil {
		return fmt.Errorf("tsl v5.0 encode: %w", err)
	}
	return s.sendBytes(payload)
}
