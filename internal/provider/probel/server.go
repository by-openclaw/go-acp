package probel

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"

	"acp/internal/export/canonical"
)

// Server is the exported alias for the concrete Probel provider. Mirrors
// the acp1 / acp2 provider convention so cmd/acp-provider can reach
// protocol-specific helpers (e.g. demo hooks added by follow-up PRs)
// via a type assertion.
type Server = server

// server is the concrete provider.Provider for Probel SW-P-08 over TCP.
// One listener accepts many client sessions; each session runs in its
// own goroutine reading framed commands and dispatching them to per-CMD
// handlers (added per-command PRs).
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
	t, err := newTree(exp)
	if err != nil {
		logger.Error("probel provider: tree build failed", slog.String("err", err.Error()))
		t = &tree{matrices: map[matrixKey]*matrixState{}}
	}
	return &server{
		logger:   logger,
		tree:     t,
		sessions: map[*session]struct{}{},
		stopped:  make(chan struct{}),
	}
}

// Serve binds addr and accepts client sessions until ctx is cancelled.
func (s *server) Serve(ctx context.Context, addr string) error {
	lc := &net.ListenConfig{}
	ln, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("probel provider: listen %q: %w", addr, err)
	}
	s.mu.Lock()
	s.listener = ln
	s.mu.Unlock()

	s.logger.Info("probel provider listening",
		slog.String("addr", ln.Addr().String()),
		slog.Int("matrices", s.tree.Size()),
	)

	go func() {
		<-ctx.Done()
		s.mu.Lock()
		if !s.closed {
			s.closed = true
			_ = ln.Close()
		}
		s.mu.Unlock()
	}()

	err = s.acceptLoop(ctx, ln)
	close(s.stopped)
	if errors.Is(err, net.ErrClosed) || errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

// Stop closes the listener and drops all active sessions.
func (s *server) Stop() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	ln := s.listener
	sessions := make([]*session, 0, len(s.sessions))
	for sess := range s.sessions {
		sessions = append(sessions, sess)
	}
	s.mu.Unlock()
	for _, sess := range sessions {
		sess.close()
	}
	if ln != nil {
		return ln.Close()
	}
	return nil
}

// SetValue mutates the served tree from the API path (acp-srv / tests).
// Path format for Probel: "<matrix>.<level>.<dst>" — all decimal.
// The value must be a source index (int, int64, uint64, string
// convertible). Per-command PRs may broaden this (name updates, salvos).
func (s *server) SetValue(_ context.Context, path string, val any) (any, error) {
	m, l, dst, err := parseCrosspointPath(path)
	if err != nil {
		return nil, err
	}
	src, err := coerceSource(val)
	if err != nil {
		return nil, err
	}
	if err := s.tree.applyConnect(m, l, dst, src); err != nil {
		return nil, err
	}
	s.logger.Info("probel set crosspoint",
		slog.Int("matrix", int(m)),
		slog.Int("level", int(l)),
		slog.Int("dst", int(dst)),
		slog.Int("src", int(src)),
	)
	return map[string]uint16{"src": src}, nil
}

func (s *server) acceptLoop(ctx context.Context, ln net.Listener) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		sess := newSession(s, conn)
		s.mu.Lock()
		s.sessions[sess] = struct{}{}
		s.mu.Unlock()
		go func() {
			sess.run(ctx)
			s.mu.Lock()
			delete(s.sessions, sess)
			s.mu.Unlock()
		}()
	}
}

// parseCrosspointPath parses "matrix.level.dst" into uint8/uint8/uint16.
func parseCrosspointPath(path string) (uint8, uint8, uint16, error) {
	var m, l int
	var dst int
	if _, err := fmt.Sscanf(path, "%d.%d.%d", &m, &l, &dst); err != nil {
		return 0, 0, 0, fmt.Errorf("probel: path %q must be \"matrix.level.dst\"", path)
	}
	if m < 0 || m > 255 || l < 0 || l > 255 || dst < 0 || dst > 0xFFFF {
		return 0, 0, 0, fmt.Errorf("probel: path %q has out-of-range component", path)
	}
	return uint8(m), uint8(l), uint16(dst), nil
}

func coerceSource(val any) (uint16, error) {
	switch v := val.(type) {
	case int:
		return uint16(v), nil
	case int32:
		return uint16(v), nil
	case int64:
		return uint16(v), nil
	case uint16:
		return v, nil
	case uint32:
		return uint16(v), nil
	case uint64:
		return uint16(v), nil
	case float64:
		return uint16(v), nil
	}
	return 0, fmt.Errorf("probel: cannot coerce %T to source index", val)
}

