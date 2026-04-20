package acp2

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"

	"acp/internal/export/canonical"
	iacp2 "acp/internal/protocol/acp2"
)

// Server is the exported alias for the concrete provider — lets
// `cmd/acp-provider/main.go` reach the ACP2-specific helpers
// (e.g. RunAnnounceDemo) via a type assertion without widening the
// cross-protocol provider.Provider interface.
type Server = server

// server is the concrete provider.Provider for ACP2 over AN2/TCP.
//
// Concurrency model:
//   - Serve runs the TCP accept loop on one goroutine.
//   - Each accepted connection is handled by a session goroutine that
//     owns the conn and serialises all writes — no per-write lock needed.
//   - Announcements fan out across sessions under s.mu.
//
// Tree mutation (via Provider.SetValue or incoming set_property) takes
// tree.mu's write lock; reads take RLock. Consistent with the emberplus
// + acp1 providers.
type server struct {
	logger *slog.Logger
	tree   *tree

	mu       sync.Mutex
	listener net.Listener
	sessions map[*session]struct{}
	closed   bool
	stopped  chan struct{}
}

func newServer(logger *slog.Logger, exp *canonical.Export) *server {
	if logger == nil {
		logger = slog.Default()
	}
	s := &server{
		logger:   logger,
		sessions: map[*session]struct{}{},
		stopped:  make(chan struct{}),
	}
	t, err := newTree(exp)
	if err != nil {
		logger.Error("acp2 provider: tree build failed", slog.String("err", err.Error()))
		s.tree = emptyTree()
		return s
	}
	s.tree = t
	return s
}

// Serve binds addr (e.g. "0.0.0.0:2072") and blocks until ctx is
// cancelled or a fatal listen error occurs.
func (s *server) Serve(ctx context.Context, addr string) error {
	ln, err := net.Listen("tcp4", addr)
	if err != nil {
		return fmt.Errorf("acp2 provider: listen %q: %w", addr, err)
	}

	s.mu.Lock()
	s.listener = ln
	s.mu.Unlock()

	s.logger.Info("acp2 provider listening",
		slog.String("addr", ln.Addr().String()),
		slog.Int("objects", s.tree.count()),
	)

	// Close listener when ctx goes away; unblocks Accept.
	go func() {
		<-ctx.Done()
		s.mu.Lock()
		if !s.closed {
			s.closed = true
			_ = ln.Close()
		}
		s.mu.Unlock()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) || errors.Is(err, context.Canceled) {
				close(s.stopped)
				return nil
			}
			close(s.stopped)
			return err
		}
		sess := newSession(s, conn)
		s.registerSession(sess)
		go func() {
			sess.run()
			s.unregisterSession(sess)
		}()
	}
}

// Stop closes the listener and all active sessions. Safe to call
// multiple times.
func (s *server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	var err error
	if s.listener != nil {
		err = s.listener.Close()
	}
	for sess := range s.sessions {
		_ = sess.conn.Close()
	}
	return err
}

// SetValue mutates the served tree via the API path and fans the
// change out to every session that has EnableProtocolEvents([2])
// subscribed. Ships in Step 2e; this commit leaves it unimplemented.
func (s *server) SetValue(_ context.Context, path string, val any) (any, error) {
	_, _ = path, val
	return nil, errors.New("acp2 provider: SetValue ships in Step 2e")
}

// broadcastAnnounce wraps the announce ACP2 message in an AN2 data
// frame and sends it to every session that has EnableProtocolEvents
// ([ACP2]) subscribed — spec §"ACP2 Announces" p.88. Sessions that
// haven't subscribed are silently skipped (matching how a real Axon
// device ignores unregistered listeners).
func (s *server) broadcastAnnounce(slot uint8, ann *iacp2.ACP2Message) {
	// Bypass EncodeACP2Message (which is request-shaped for the four
	// ACP2 funcs) and build the reply/announce frame manually. See
	// replyACP2 for the same rationale.
	raw := make([]byte, 4+len(ann.Body))
	raw[0] = byte(ann.Type)
	raw[1] = ann.MTID
	raw[2] = byte(ann.Func)
	raw[3] = ann.PID
	copy(raw[4:], ann.Body)
	frame := &iacp2.AN2Frame{
		Proto:   iacp2.AN2ProtoACP2,
		Slot:    slot,
		MTID:    0,
		Type:    iacp2.AN2TypeData,
		Payload: raw,
	}

	s.mu.Lock()
	totalSessions := len(s.sessions)
	targets := make([]*session, 0, len(s.sessions))
	for sess := range s.sessions {
		if sess.enabled[iacp2.AN2ProtoACP2] {
			targets = append(targets, sess)
		}
	}
	s.mu.Unlock()

	s.logger.Info("acp2 announce fanout",
		slog.Int("slot", int(slot)),
		slog.Int("sessions_total", totalSessions),
		slog.Int("sessions_subscribed", len(targets)),
		slog.Int("frame_bytes", len(raw)+8),
	)

	for _, sess := range targets {
		if err := sess.write(frame); err != nil {
			s.logger.Warn("acp2 announce send failed",
				slog.String("err", err.Error()),
			)
		}
	}
}

func (s *server) registerSession(sess *session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[sess] = struct{}{}
}

func (s *server) unregisterSession(sess *session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, sess)
}
