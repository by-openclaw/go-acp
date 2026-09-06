package tsl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"

	"dhs/internal/transport"
	"time"

	"dhs/internal/tsl/codec"
)

// tcpKeepalivePeriod sets SO_KEEPALIVE on accepted TCP connections.
// TSL v5.0 carries no in-protocol keep-alive — the OS-layer probe is
// the dead-socket detector when a producer goes away without FIN.
const tcpKeepalivePeriod = 30 * time.Second

// tcpSession accepts incoming TCP connections from TSL v5.0 producers
// and de-frames the DLE/STX-wrapped stream per spec §5.0. Each accepted
// connection gets its own reader goroutine.
type tcpSession struct {
	listener  net.Listener
	cancel    context.CancelFunc
	mu        sync.RWMutex
	v50Subs   []V50Handler
	wg        sync.WaitGroup
	closeOnce sync.Once

	// idleTimeout, when > 0, closes an accepted connection that has sent
	// nothing for that long, reaping half-open producer links (a NAT or
	// firewall drop with no RST) that would otherwise hold a goroutine and
	// a socket forever.
	//
	// It defaults to 0 (OFF), and that default is deliberate. Unlike every
	// other connector we fixed, this is a PASSIVE receiver and TSL UMD
	// defines no heartbeat in any of v3.1/v4.0/v5.0 — a tally link that
	// sends nothing for hours is perfectly healthy, because tallies are
	// emitted on change. With no heartbeat there is nothing to distinguish
	// quiet from dead, so an on-by-default deadline would disconnect
	// working producers. OS-level SO_KEEPALIVE (set on every accepted
	// connection) stays the always-on detector; this is the opt-in for
	// deployments that would rather reap aggressively.
	idle transport.Idle

	// onRx, when set, is called on every packet received, with the byte
	// count that arrived. It is how the plugin's inherited Health learns
	// the peer is alive and how the metrics connector counts rx; fired on
	// BYTES received rather than on a successful decode, because a peer
	// sending malformed frames is still a peer that is there.
	onRx func(n int)
}

func newTCPSession() *tcpSession {
	return &tcpSession{}
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

// listen binds a TCP listener on addr and accepts connections until ctx
// is cancelled or the listener is closed.
func (s *tcpSession) listen(ctx context.Context, addr string) error {
	// The listener applies SO_KEEPALIVE to every connection it accepts, so
	// the accept loop below no longer carries its own copy of that policy.
	l, err := transport.ListenTCP(ctx, "tcp", addr, transport.SocketOptions{
		KeepalivePeriod: tcpKeepalivePeriod,
	})
	if err != nil {
		return fmt.Errorf("tsl v5.0 TCP: listen %q: %w", addr, err)
	}
	s.listener = l
	ctx, s.cancel = context.WithCancel(ctx)
	s.wg.Add(1)
	go s.acceptLoop(ctx)
	return nil
}

// boundAddr returns the listener's local address.
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
			if ctx.Err() != nil {
				return
			}
			// Transient error — retry unless the listener is closed.
			if errors.Is(err, net.ErrClosed) {
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

	dec := codec.NewDLEStreamDecoder(conn, codec.V50MaxPacketSize)
	for {
		if ctx.Err() != nil {
			return
		}
		// Arm via the shared bound (transport.Idle): Arm and SetOn share a
		// mutex there, so a concurrent change cannot be clobbered by a stale
		// value read here. Disabled is a no-op, leaving any caller deadline.
		_ = s.idle.Arm(conn)
		pkt, err := dec.ReadFrame()
		if err != nil {
			if err == io.EOF || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) {
				return
			}
			// Framing error — surface as a compliance note + close
			// the connection (malformed stream can't be recovered).
			s.dispatch(FrameV50Event{
				Remote: conn.RemoteAddr(),
				Frame: codec.V50Packet{
					Notes: []codec.ComplianceNote{{
						Kind:   "tsl_v5_tcp_framing_error",
						Detail: err.Error(),
					}},
				},
			})
			return
		}
		if s.onRx != nil {
			s.onRx(len(pkt))
		}
		frame, derr := codec.DecodeV50(pkt)
		if derr != nil {
			s.dispatch(FrameV50Event{
				Remote: conn.RemoteAddr(),
				Frame: codec.V50Packet{
					Notes: []codec.ComplianceNote{{
						Kind:   "tsl_decode_error",
						Detail: derr.Error(),
					}},
				},
				Raw: pkt,
			})
			continue
		}
		s.dispatch(FrameV50Event{Remote: conn.RemoteAddr(), Frame: frame, Raw: pkt})
	}
}

func (s *tcpSession) subscribeV50(h V50Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.v50Subs = append(s.v50Subs, h)
}

func (s *tcpSession) dispatch(ev FrameV50Event) {
	s.mu.RLock()
	subs := make([]V50Handler, len(s.v50Subs))
	copy(subs, s.v50Subs)
	s.mu.RUnlock()
	for _, h := range subs {
		h(ev)
	}
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
