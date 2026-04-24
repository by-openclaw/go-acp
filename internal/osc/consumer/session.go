package osc

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"syscall"

	"acp/internal/osc/codec"
	"acp/internal/transport"
)

// PacketEvent is delivered to every subscriber whose pattern matches
// either a raw Message's address or (for Bundles) any of the nested
// Message addresses.
type PacketEvent struct {
	Remote  net.Addr
	Packet  codec.Packet // codec.Message or codec.Bundle
	Raw     []byte
	Matched string // address that matched the subscriber's pattern
	Msg     codec.Message // convenience: the matching Message (bundle-flattened)
}

// Handler is invoked for each received packet-with-matching-address.
type Handler func(PacketEvent)

// udpSession owns the OSC UDP listener and routes received Messages /
// Bundle-nested Messages to pattern-matched subscribers. SO_REUSEADDR
// is set so multiple dhs instances can share the port (matches the
// ACP1 / TSL multi-listener contract).
type udpSession struct {
	conn      *net.UDPConn
	cancel    context.CancelFunc
	mu        sync.RWMutex
	subs      []subscriber
	closeOnce sync.Once
}

type subscriber struct {
	pattern string // "" = match any address
	fn      Handler
}

func newUDPSession() *udpSession {
	return &udpSession{}
}

func (s *udpSession) listen(ctx context.Context, addr string) error {
	lc := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			var opErr error
			if err := c.Control(func(fd uintptr) {
				opErr = transport.SetSocketReuseAddr(fd)
			}); err != nil {
				return err
			}
			return opErr
		},
	}
	pc, err := lc.ListenPacket(ctx, "udp", addr)
	if err != nil {
		return fmt.Errorf("osc: listen %q: %w", addr, err)
	}
	conn, ok := pc.(*net.UDPConn)
	if !ok {
		_ = pc.Close()
		return fmt.Errorf("osc: listen %q: unexpected conn type %T", addr, pc)
	}
	s.conn = conn
	ctx, s.cancel = context.WithCancel(ctx)
	go s.readLoop(ctx)
	return nil
}

func (s *udpSession) boundAddr() *net.UDPAddr {
	if s.conn == nil {
		return nil
	}
	return s.conn.LocalAddr().(*net.UDPAddr)
}

func (s *udpSession) readLoop(ctx context.Context) {
	buf := make([]byte, 64*1024) // generous; UDP MTU is smaller but we allow jumbos
	for {
		if ctx.Err() != nil {
			return
		}
		n, remote, err := s.conn.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			continue
		}
		pkt := make([]byte, n)
		copy(pkt, buf[:n])
		s.dispatch(remote, pkt)
	}
}

// dispatch decodes one packet and routes to pattern-matched subscribers.
// A Bundle flattens into its constituent Messages — each is matched
// against every subscriber. Decode errors surface as a synthetic
// compliance-note event to the "" (match-all) subscribers only.
func (s *udpSession) dispatch(remote net.Addr, raw []byte) {
	p, err := codec.DecodePacket(raw)
	if err != nil {
		s.fireAll(PacketEvent{
			Remote: remote,
			Packet: codec.Message{Notes: []codec.ComplianceNote{{
				Kind: "osc_decode_error", Detail: err.Error(),
			}}},
			Raw: raw,
		})
		return
	}
	switch v := p.(type) {
	case codec.Message:
		s.fireMatching(remote, p, raw, v)
	case codec.Bundle:
		s.fireBundle(remote, v, raw)
	}
}

// fireBundle walks a Bundle and fires subscribers for each nested Message.
// Nested Bundles recurse.
func (s *udpSession) fireBundle(remote net.Addr, b codec.Bundle, raw []byte) {
	for _, el := range b.Elements {
		switch v := el.(type) {
		case codec.Message:
			s.fireMatching(remote, b, raw, v)
		case codec.Bundle:
			s.fireBundle(remote, v, raw)
		}
	}
}

func (s *udpSession) fireMatching(remote net.Addr, wrap codec.Packet, raw []byte, m codec.Message) {
	s.mu.RLock()
	subs := make([]subscriber, len(s.subs))
	copy(subs, s.subs)
	s.mu.RUnlock()
	for _, sub := range subs {
		if addressMatches(sub.pattern, m.Address) {
			sub.fn(PacketEvent{
				Remote: remote, Packet: wrap, Raw: raw,
				Matched: m.Address, Msg: m,
			})
		}
	}
}

func (s *udpSession) fireAll(ev PacketEvent) {
	s.mu.RLock()
	subs := make([]subscriber, len(s.subs))
	copy(subs, s.subs)
	s.mu.RUnlock()
	for _, sub := range subs {
		if sub.pattern == "" {
			sub.fn(ev)
		}
	}
}

func (s *udpSession) subscribe(pattern string, fn Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subs = append(s.subs, subscriber{pattern: pattern, fn: fn})
}

func (s *udpSession) close() error {
	var err error
	s.closeOnce.Do(func() {
		if s.cancel != nil {
			s.cancel()
		}
		if s.conn != nil {
			err = s.conn.Close()
		}
	})
	return err
}

// addressMatches implements a minimal OSC address-pattern test:
//   - empty pattern (`""`) matches anything
//   - exact equality matches
//   - trailing `/*` glob matches any single-segment extension under the prefix
//
// The full OSC pattern language (`?`, `*`, `[...]`, `{...}`) is a
// follow-up; keep v1.0 lean.
func addressMatches(pattern, addr string) bool {
	if pattern == "" || pattern == addr {
		return true
	}
	if strings.HasSuffix(pattern, "/*") {
		prefix := strings.TrimSuffix(pattern, "/*")
		if !strings.HasPrefix(addr, prefix+"/") {
			return false
		}
		// Exactly one segment beyond the prefix.
		rest := addr[len(prefix)+1:]
		return !strings.Contains(rest, "/") && len(rest) > 0
	}
	return false
}
