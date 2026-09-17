package osc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"

	"dhs/internal/transport"
	"time"

	"dhs/internal/osc/codec"
)

// tcpKeepalivePeriod sets SO_KEEPALIVE on accepted TCP connections.
// OSC carries no in-protocol keep-alive, so the OS-layer probe is the
// dead-socket detector for half-open sessions.
const tcpKeepalivePeriod = 30 * time.Second

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

	// idleTimeout, when > 0, closes an accepted connection that has sent
	// nothing for that long, reaping half-open peers (a NAT or firewall
	// drop with no RST) that would otherwise hold a goroutine and a socket
	// forever.
	//
	// It defaults to 0 (OFF), deliberately. This is a PASSIVE receiver and
	// OSC 1.0/1.1 define no heartbeat — a control surface that sends
	// nothing until someone moves a fader is perfectly healthy, so silence
	// carries no information about liveness and an on-by-default deadline
	// would disconnect working peers. OS-level SO_KEEPALIVE (set on every
	// accepted connection) stays the always-on detector; this is the opt-in
	// for deployments that would rather reap aggressively.
	idle transport.Idle

	// onRx, when set, is called on every packet received, with the byte
	// count that arrived. It is how the plugin's inherited Health learns
	// the peer is alive and how the metrics connector counts rx; fired on
	// BYTES received rather than on a successful decode, because a peer
	// sending malformed frames is still a peer that is there.
	onRx func(n int)
}

func newTCPSession(f framerKind) *tcpSession {
	return &tcpSession{framer: f}
}

// SetIdleTimeout arms (d > 0) or disables (d <= 0) the per-connection idle
// reaper. See the field comment for why this is off by default.
func (s *tcpSession) SetIdleTimeout(d time.Duration) {
	s.idle.Set(d)
}

// IdleTimeout reports the currently armed idle reaper window.
func (s *tcpSession) IdleTimeout() time.Duration {
	return s.idle.Get()
}

func (s *tcpSession) listen(ctx context.Context, addr string) error {
	// The listener applies SO_KEEPALIVE to every connection it accepts, so
	// the accept loop below no longer carries its own copy of that policy.
	l, err := transport.ListenTCP(ctx, "tcp", addr, transport.SocketOptions{
		KeepalivePeriod: tcpKeepalivePeriod,
	})
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
		// Arm via the shared bound (transport.Idle): Arm and SetOn share a
		// mutex there, so a concurrent change cannot be clobbered by a stale
		// value read here. Disabled is a no-op, leaving any caller deadline.
		_ = s.idle.Arm(conn)
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
		if s.onRx != nil {
			s.onRx(len(pkt))
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
