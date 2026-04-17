package emberplus

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"acp/internal/protocol/emberplus/glow"
	"acp/internal/protocol/emberplus/s101"
)

// Session manages a single TCP connection to an Ember+ provider.
type Session struct {
	conn     net.Conn
	reader   *s101.Reader
	writer   *s101.Writer
	logger   *slog.Logger
	mu       sync.Mutex
	closed   bool

	// Callbacks for received elements.
	onElement func([]glow.Element)

	// Keep-alive.
	keepAliveInterval time.Duration
	keepAliveDone     chan struct{}
}

// NewSession creates a session (not yet connected).
func NewSession(logger *slog.Logger) *Session {
	return &Session{
		logger:            logger,
		keepAliveInterval: 10 * time.Second,
	}
}

// Connect dials the Ember+ provider and starts the read loop.
func (s *Session) Connect(ctx context.Context, host string, port int) error {
	addr := fmt.Sprintf("%s:%d", host, port)
	d := net.Dialer{Timeout: 10 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("emberplus: connect %s: %w", addr, err)
	}

	s.mu.Lock()
	s.conn = conn
	s.reader = s101.NewReader(conn)
	s.writer = s101.NewWriter(conn)
	s.closed = false
	s.keepAliveDone = make(chan struct{})
	s.mu.Unlock()

	// Start read loop.
	go s.readLoop()

	// Start keep-alive.
	go s.keepAliveLoop()

	s.logger.Info("emberplus: connected", "host", host, "port", port)
	return nil
}

// Disconnect closes the TCP connection.
func (s *Session) Disconnect() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.keepAliveDone != nil {
		close(s.keepAliveDone)
	}
	if s.conn != nil {
		return s.conn.Close()
	}
	return nil
}

// SetOnElement sets the callback for received Glow elements.
func (s *Session) SetOnElement(fn func([]glow.Element)) {
	s.mu.Lock()
	s.onElement = fn
	s.mu.Unlock()
}

// SendGetDirectory sends a GetDirectory command for the root.
func (s *Session) SendGetDirectory() error {
	payload := glow.EncodeGetDirectory()
	s.logger.Debug("emberplus: sending GetDirectory",
		"payload_len", len(payload),
		"payload_hex", fmt.Sprintf("%x", payload))
	return s.sendEmBER(payload)
}

// SendGetDirectoryFor sends a GetDirectory command for a specific path.
func (s *Session) SendGetDirectoryFor(path []int32) error {
	payload := glow.EncodeGetDirectoryFor(path)
	s.logger.Debug("emberplus: sending GetDirectoryFor",
		"path", path,
		"payload_len", len(payload),
		"payload_hex", fmt.Sprintf("%x", payload))
	return s.sendEmBER(payload)
}

// SendSubscribe sends a Subscribe command for a parameter path.
func (s *Session) SendSubscribe(path []int32) error {
	payload := glow.EncodeSubscribe(path)
	return s.sendEmBER(payload)
}

// SendUnsubscribe sends an Unsubscribe command.
func (s *Session) SendUnsubscribe(path []int32) error {
	payload := glow.EncodeUnsubscribe(path)
	return s.sendEmBER(payload)
}

// SendSetValue sends a set-value command for a parameter.
func (s *Session) SendSetValue(path []int32, value interface{}) error {
	payload := glow.EncodeSetValue(path, value)
	return s.sendEmBER(payload)
}

// SendMatrixConnect sends a matrix connection command.
func (s *Session) SendMatrixConnect(matrixPath []int32, target int32, sources []int32, operation int64) error {
	payload := glow.EncodeMatrixConnect(matrixPath, target, sources, operation)
	return s.sendEmBER(payload)
}

// SendInvoke sends a function invocation.
func (s *Session) SendInvoke(funcPath []int32, invocationID int32, args []interface{}) error {
	payload := glow.EncodeInvoke(funcPath, invocationID, args)
	return s.sendEmBER(payload)
}

func (s *Session) sendEmBER(payload []byte) error {
	s.mu.Lock()
	w := s.writer
	s.mu.Unlock()
	if w == nil {
		return fmt.Errorf("emberplus: not connected")
	}
	frame := s101.NewEmBERFrame(payload)
	return w.WriteFrame(frame)
}

func (s *Session) readLoop() {
	for {
		s.mu.Lock()
		r := s.reader
		closed := s.closed
		s.mu.Unlock()
		if closed || r == nil {
			return
		}

		frame, err := r.ReadFrame()
		if err != nil {
			s.mu.Lock()
			closed = s.closed
			s.mu.Unlock()
			if !closed {
				s.logger.Debug("emberplus: raw read error detail", "err", err.Error())
				s.logger.Debug("emberplus: read error", "err", err)
			}
			return
		}

		if frame.IsKeepAlive() {
			s.logger.Debug("emberplus: keep-alive rx", "cmd", frame.Command)
			if frame.Command == s101.CmdKeepAliveReq {
				resp := s101.NewKeepAliveResponse()
				s.mu.Lock()
				w := s.writer
				s.mu.Unlock()
				if w != nil {
					if err := w.WriteFrame(resp); err != nil {
						s.logger.Debug("emberplus: keep-alive resp failed", "err", err)
					} else {
						s.logger.Debug("emberplus: keep-alive resp sent")
					}
				}
			}
			continue
		}

		if frame.IsEmBER() && len(frame.Payload) > 0 {
			s.logger.Debug("emberplus: received EmBER frame",
				"payload_len", len(frame.Payload),
				"dtd", frame.DTD,
				"version", frame.Version,
				"hex", fmt.Sprintf("%x", frame.Payload))
			elements, err := glow.DecodeRoot(frame.Payload)
			if err != nil {
				s.logger.Debug("emberplus: glow decode error",
					"err", err,
					"payload_len", len(frame.Payload))
				continue
			}
			s.logger.Debug("emberplus: decoded glow elements", "count", len(elements))
			s.mu.Lock()
			fn := s.onElement
			s.mu.Unlock()
			if fn != nil && len(elements) > 0 {
				fn(elements)
			}
		}
	}
}

func (s *Session) keepAliveLoop() {
	ticker := time.NewTicker(s.keepAliveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.keepAliveDone:
			return
		case <-ticker.C:
			s.mu.Lock()
			w := s.writer
			closed := s.closed
			s.mu.Unlock()
			if closed || w == nil {
				return
			}
			req := s101.NewKeepAliveRequest()
			if err := w.WriteFrame(req); err != nil {
				s.logger.Debug("emberplus: keep-alive failed", "err", err)
				return
			}
		}
	}
}
