package tsl

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"syscall"

	"acp/internal/transport"
	"acp/internal/tsl/codec"
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
	if s.conn != nil {
		return errors.New("tsl provider: already bound")
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
		return fmt.Errorf("tsl provider: bind %q: %w", addr, err)
	}
	conn, ok := pc.(*net.UDPConn)
	if !ok {
		_ = pc.Close()
		return fmt.Errorf("tsl provider: bind %q: unexpected conn type %T", addr, pc)
	}
	s.conn = conn
	return nil
}

// boundAddr returns the actual local address (ephemeral resolution).
func (s *udpSender) boundAddr() *net.UDPAddr {
	if s.conn == nil {
		return nil
	}
	return s.conn.LocalAddr().(*net.UDPAddr)
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
	if s.conn == nil {
		return errors.New("tsl provider: not bound")
	}
	dests := s.destsSnapshot()
	if len(dests) == 0 {
		return errors.New("tsl provider: no destinations configured")
	}
	var firstErr error
	for _, d := range dests {
		if _, err := s.conn.WriteToUDP(payload, d); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("write to %s: %w", d.String(), err)
		}
	}
	return firstErr
}

// close shuts the socket. Safe to call multiple times.
func (s *udpSender) close() error {
	var err error
	s.closeOnce.Do(func() {
		if s.conn != nil {
			err = s.conn.Close()
		}
	})
	return err
}

// serveBlock is the shared Serve body — binds (if addr set) and blocks
// on ctx. Version-specific Server.Serve wraps this.
func (s *udpSender) serveBlock(ctx context.Context, addr string) error {
	if s.conn == nil {
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

