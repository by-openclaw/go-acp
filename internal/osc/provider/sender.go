package osc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"syscall"

	"acp/internal/osc/codec"
	"acp/internal/transport"
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
	if s.conn != nil {
		return errors.New("osc provider: already bound")
	}
	if addr == "" {
		addr = ":0"
	}
	lc := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			var opErr error
			if err := c.Control(func(fd uintptr) {
				if e := transport.SetSocketReuseAddr(fd); e != nil {
					opErr = e
					return
				}
				opErr = transport.SetSocketBroadcast(fd)
			}); err != nil {
				return err
			}
			return opErr
		},
	}
	pc, err := lc.ListenPacket(context.Background(), "udp", addr)
	if err != nil {
		return fmt.Errorf("osc provider: bind %q: %w", addr, err)
	}
	conn, ok := pc.(*net.UDPConn)
	if !ok {
		_ = pc.Close()
		return fmt.Errorf("osc provider: bind %q: unexpected conn type %T", addr, pc)
	}
	s.conn = conn
	return nil
}

func (s *udpSender) boundAddr() *net.UDPAddr {
	if s.conn == nil {
		return nil
	}
	return s.conn.LocalAddr().(*net.UDPAddr)
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
	if s.conn == nil {
		return errors.New("osc provider: not bound")
	}
	dests := s.destsSnapshot()
	if len(dests) == 0 {
		return errors.New("osc provider: no destinations configured")
	}
	var firstErr error
	for _, d := range dests {
		if _, err := s.conn.WriteToUDP(payload, d); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("write to %s: %w", d.String(), err)
		}
	}
	return firstErr
}

func (s *udpSender) close() error {
	var err error
	s.closeOnce.Do(func() {
		if s.conn != nil {
			err = s.conn.Close()
		}
	})
	return err
}

func (s *udpSender) serveBlock(ctx context.Context, addr string) error {
	if s.conn == nil {
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
