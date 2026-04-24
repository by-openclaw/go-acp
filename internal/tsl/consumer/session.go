package tsl

import (
	"context"
	"fmt"
	"net"
	"sync"
	"syscall"

	"acp/internal/transport"
	"acp/internal/tsl/codec"
)

// FrameV31Event is delivered to registered v3.1 handlers.
type FrameV31Event struct {
	Remote *net.UDPAddr
	Frame  codec.V31Frame
	Raw    []byte
}

// FrameV40Event is delivered to registered v4.0 handlers. v4.0 wire
// frames are 20-22 bytes; the nested V31 part lives on Frame.V31.
type FrameV40Event struct {
	Remote *net.UDPAddr
	Frame  codec.V40Frame
	Raw    []byte
}

// V31Handler is invoked for each received v3.1 frame.
type V31Handler func(FrameV31Event)

// V40Handler is invoked for each received v4.0 frame.
type V40Handler func(FrameV40Event)

// udpSession is a UDP listener shared across versions. Each TSL version
// owns a decode function and a version-specific handler channel; the
// session takes care of socket lifecycle only.
type udpSession struct {
	conn   *net.UDPConn
	cancel context.CancelFunc

	mu        sync.RWMutex
	v31Subs   []V31Handler
	v40Subs   []V40Handler
	closeOnce sync.Once
}

func newUDPSession() *udpSession {
	return &udpSession{}
}

// listen binds to addr and starts a background read goroutine. addr
// format is "host:port"; host may be empty for bind-all.
//
// SO_REUSEADDR is set on the socket so multiple dhs instances (or other
// TSL receivers) on the same host can share the port and each receive
// the broadcast datagrams — matches the ACP1 multi-listener contract.
// On Linux SO_REUSEPORT is also set best-effort (see
// internal/transport/sockopt_unix.go).
func (s *udpSession) listen(ctx context.Context, addr string, decode func(*net.UDPAddr, []byte, *udpSession)) error {
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
		return fmt.Errorf("tsl: listen %q: %w", addr, err)
	}
	conn, ok := pc.(*net.UDPConn)
	if !ok {
		_ = pc.Close()
		return fmt.Errorf("tsl: listen %q: unexpected conn type %T", addr, pc)
	}
	s.conn = conn

	ctx, s.cancel = context.WithCancel(ctx)
	go s.readLoop(ctx, decode)
	return nil
}

// boundAddr returns the actual bound address (useful when caller passed
// port 0 for ephemeral).
func (s *udpSession) boundAddr() *net.UDPAddr {
	if s.conn == nil {
		return nil
	}
	return s.conn.LocalAddr().(*net.UDPAddr)
}

func (s *udpSession) readLoop(ctx context.Context, decode func(*net.UDPAddr, []byte, *udpSession)) {
	// TSL v3.1 is 18B; v4.0 up to 18+1+1+16 ≈ 36B; v5.0 up to 2048. Use
	// 2048 unconditionally so the same session works for all three.
	buf := make([]byte, 2048)
	for {
		if ctx.Err() != nil {
			return
		}
		n, remote, err := s.conn.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			// Transient read error — log and continue.
			continue
		}
		pkt := make([]byte, n)
		copy(pkt, buf[:n])
		decode(remote, pkt, s)
	}
}

// subscribeV31 registers a handler for v3.1 frames.
func (s *udpSession) subscribeV31(h V31Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.v31Subs = append(s.v31Subs, h)
}

// subscribeV40 registers a handler for v4.0 frames.
func (s *udpSession) subscribeV40(h V40Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.v40Subs = append(s.v40Subs, h)
}

// dispatchV31 fans an event out to all v3.1 subscribers.
func (s *udpSession) dispatchV31(ev FrameV31Event) {
	s.mu.RLock()
	subs := make([]V31Handler, len(s.v31Subs))
	copy(subs, s.v31Subs)
	s.mu.RUnlock()
	for _, h := range subs {
		h(ev)
	}
}

// dispatchV40 fans an event out to all v4.0 subscribers.
func (s *udpSession) dispatchV40(ev FrameV40Event) {
	s.mu.RLock()
	subs := make([]V40Handler, len(s.v40Subs))
	copy(subs, s.v40Subs)
	s.mu.RUnlock()
	for _, h := range subs {
		h(ev)
	}
}

// close stops the read loop and releases the socket.
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

// decodeV40Payload decodes a v4.0 frame and dispatches. Minimum size 20
// bytes (v3.1 + CHKSUM + VBC), extra XDATA appended.
func decodeV40Payload(remote *net.UDPAddr, pkt []byte, s *udpSession) {
	frame, err := codec.DecodeV40(pkt)
	if err != nil {
		s.dispatchV40(FrameV40Event{
			Remote: remote,
			Frame: codec.V40Frame{
				Notes: []codec.ComplianceNote{{
					Kind:   "tsl_decode_error",
					Detail: err.Error(),
				}},
			},
			Raw: pkt,
		})
		return
	}
	s.dispatchV40(FrameV40Event{Remote: remote, Frame: frame, Raw: pkt})
}

// decodeV31Payload validates size + decodes a v3.1 frame and dispatches
// it to subscribers.
func decodeV31Payload(remote *net.UDPAddr, pkt []byte, s *udpSession) {
	if len(pkt) != codec.V31FrameSize {
		// Size mismatch is a structural problem — fire a best-effort note
		// through an empty frame with a synthetic compliance note.
		s.dispatchV31(FrameV31Event{
			Remote: remote,
			Frame: codec.V31Frame{
				Notes: []codec.ComplianceNote{{
					Kind:   "tsl_label_length_mismatch",
					Detail: fmt.Sprintf("v3.1 frame size %d, expected %d", len(pkt), codec.V31FrameSize),
				}},
			},
			Raw: pkt,
		})
		return
	}
	frame, err := codec.DecodeV31(pkt)
	if err != nil {
		// Structurally unparseable — still surface via a note.
		s.dispatchV31(FrameV31Event{
			Remote: remote,
			Frame: codec.V31Frame{
				Notes: []codec.ComplianceNote{{
					Kind:   "tsl_decode_error",
					Detail: err.Error(),
				}},
			},
			Raw: pkt,
		})
		return
	}
	s.dispatchV31(FrameV31Event{Remote: remote, Frame: frame, Raw: pkt})
}
