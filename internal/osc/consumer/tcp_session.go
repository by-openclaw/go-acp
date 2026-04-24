package osc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"

	"acp/internal/osc/codec"
)

// packetReader abstracts over the two TCP framings we support:
//
//	length-prefix (OSC 1.0): codec.LenPrefixReader
//	SLIP double-END (OSC 1.1): codec.SLIPReader
type packetReader interface {
	ReadPacket() ([]byte, error)
}

// framerKind picks which TCP framing to apply. Internal to the consumer
// + provider pair.
type framerKind int

const (
	framerLenPrefix framerKind = iota
	framerSLIP
)

// tcpSession accepts incoming TCP connections and de-frames each
// according to its version's wire framing, dispatching decoded packets
// to pattern-matched subscribers.
type tcpSession struct {
	listener  net.Listener
	framer    framerKind
	cancel    context.CancelFunc
	mu        sync.RWMutex
	subs      []subscriber
	wg        sync.WaitGroup
	closeOnce sync.Once
}

func newTCPSession(f framerKind) *tcpSession {
	return &tcpSession{framer: f}
}

func (s *tcpSession) listen(ctx context.Context, addr string) error {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("osc tcp: listen %q: %w", addr, err)
	}
	s.listener = l
	ctx, s.cancel = context.WithCancel(ctx)
	s.wg.Add(1)
	go s.acceptLoop(ctx)
	return nil
}

func (s *tcpSession) boundAddr() *net.TCPAddr {
	if s.listener == nil {
		return nil
	}
	return s.listener.Addr().(*net.TCPAddr)
}

func (s *tcpSession) acceptLoop(ctx context.Context) {
	defer s.wg.Done()
	for {
		if ctx.Err() != nil {
			return
		}
		conn, err := s.listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		s.wg.Add(1)
		go s.connLoop(ctx, conn)
	}
}

func (s *tcpSession) connLoop(ctx context.Context, conn net.Conn) {
	defer s.wg.Done()
	defer func() { _ = conn.Close() }()

	var rd packetReader
	switch s.framer {
	case framerLenPrefix:
		rd = codec.NewLenPrefixReader(conn, 64*1024)
	case framerSLIP:
		rd = codec.NewSLIPReader(conn, 64*1024)
	}
	for {
		if ctx.Err() != nil {
			return
		}
		pkt, err := rd.ReadPacket()
		if err != nil {
			if err == io.EOF || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) {
				return
			}
			// Framing error — surface via "" subscribers and close
			// (malformed stream can't be recovered).
			s.fireDecodeError(conn.RemoteAddr(), err)
			return
		}
		s.dispatchPacket(conn.RemoteAddr(), pkt)
	}
}

func (s *tcpSession) dispatchPacket(remote net.Addr, pkt []byte) {
	p, err := codec.DecodePacket(pkt)
	if err != nil {
		s.fireDecodeError(remote, err)
		return
	}
	switch v := p.(type) {
	case codec.Message:
		s.fireMatching(remote, p, pkt, v)
	case codec.Bundle:
		s.fireBundle(remote, v, pkt)
	}
}

func (s *tcpSession) fireBundle(remote net.Addr, b codec.Bundle, raw []byte) {
	for _, el := range b.Elements {
		switch v := el.(type) {
		case codec.Message:
			s.fireMatching(remote, b, raw, v)
		case codec.Bundle:
			s.fireBundle(remote, v, raw)
		}
	}
}

func (s *tcpSession) fireMatching(remote net.Addr, wrap codec.Packet, raw []byte, m codec.Message) {
	s.mu.RLock()
	subs := make([]subscriber, len(s.subs))
	copy(subs, s.subs)
	s.mu.RUnlock()
	for _, sub := range subs {
		if addressMatches(sub.pattern, m.Address) {
			sub.fn(PacketEvent{Remote: remote, Packet: wrap, Raw: raw, Matched: m.Address, Msg: m})
		}
	}
}

func (s *tcpSession) fireDecodeError(remote net.Addr, err error) {
	ev := PacketEvent{
		Remote: remote,
		Packet: codec.Message{Notes: []codec.ComplianceNote{{
			Kind: "osc_decode_error", Detail: err.Error(),
		}}},
	}
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

func (s *tcpSession) subscribe(pattern string, fn Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subs = append(s.subs, subscriber{pattern: pattern, fn: fn})
}

func (s *tcpSession) close() error {
	var err error
	s.closeOnce.Do(func() {
		if s.cancel != nil {
			s.cancel()
		}
		if s.listener != nil {
			err = s.listener.Close()
		}
		s.wg.Wait()
	})
	return err
}
