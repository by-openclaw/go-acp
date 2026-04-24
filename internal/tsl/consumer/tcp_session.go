package tsl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"

	"acp/internal/tsl/codec"
)

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
}

func newTCPSession() *tcpSession {
	return &tcpSession{}
}

// listen binds a TCP listener on addr and accepts connections until ctx
// is cancelled or the listener is closed.
func (s *tcpSession) listen(ctx context.Context, addr string) error {
	l, err := net.Listen("tcp", addr)
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
