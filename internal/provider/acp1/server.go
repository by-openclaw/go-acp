package acp1

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"

	"acp/internal/export/canonical"
)

// server is the concrete provider.Provider for ACP1 over UDP. One
// datagram in -> dispatch -> one datagram out, plus broadcast announce
// on successful mutating methods.
//
// Concurrency model:
//   - Serve runs a single read loop (ReadFromUDP is goroutine-unsafe on
//     some platforms anyway). Each inbound datagram is dispatched inline
//     — ACP1 messages are fixed-tiny (<=141 bytes) and all work is in-
//     process so a serial handler is well within budget.
//   - Announcements are sent from the same goroutine right after the
//     reply so consumer-visible ordering is always (reply, announce).
//   - SetValue() is called from the embedding API (acp-srv) off-thread;
//     it grabs tree.mu for the mutation then enqueues an announce.
type server struct {
	logger *slog.Logger
	tree   *tree

	mu      sync.Mutex
	conn    *net.UDPConn
	closed  bool
	stopped chan struct{}
}

func newServer(logger *slog.Logger, exp *canonical.Export) *server {
	if logger == nil {
		logger = slog.Default()
	}
	s := &server{
		logger:  logger,
		stopped: make(chan struct{}),
	}
	t, err := newTree(exp)
	if err != nil {
		logger.Error("acp1 provider: tree build failed", "err", err.Error())
		// Surface later: Serve will fail fast with the same error.
		s.tree = &tree{entries: map[objectKey]*entry{}, slots: map[uint8]*slotCounts{}}
		return s
	}
	s.tree = t
	return s
}

// Serve binds addr (e.g. "0.0.0.0:2071") and runs until ctx is cancelled
// or a fatal listen error occurs.
func (s *server) Serve(ctx context.Context, addr string) error {
	udpAddr, err := net.ResolveUDPAddr("udp4", addr)
	if err != nil {
		return fmt.Errorf("acp1 provider: resolve %q: %w", addr, err)
	}
	conn, err := net.ListenUDP("udp4", udpAddr)
	if err != nil {
		return fmt.Errorf("acp1 provider: listen %q: %w", addr, err)
	}

	s.mu.Lock()
	s.conn = conn
	s.mu.Unlock()

	s.logger.Info("acp1 provider listening",
		slog.String("addr", conn.LocalAddr().String()),
		slog.Int("objects", len(s.tree.entries)),
	)

	// Close the socket when ctx goes away; unblocks the read loop.
	go func() {
		<-ctx.Done()
		s.mu.Lock()
		if !s.closed {
			s.closed = true
			_ = conn.Close()
		}
		s.mu.Unlock()
	}()

	err = s.readLoop(ctx, conn)
	close(s.stopped)
	if errors.Is(err, net.ErrClosed) || errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

// Stop closes the listening socket. Safe to call multiple times.
func (s *server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.conn != nil {
		return s.conn.Close()
	}
	return nil
}

// SetValue mutates the served tree and (later) fans the change out.
// Skeleton: currently only validates the path — session.go will wire
// the actual value mutation + announcement in a follow-up commit.
func (s *server) SetValue(_ context.Context, path string, val any) (any, error) {
	key, err := parsePath(path)
	if err != nil {
		return nil, err
	}
	e, ok := s.tree.lookup(key)
	if !ok {
		return nil, fmt.Errorf("acp1 provider: object not found: %s", path)
	}
	_ = e
	_ = val
	return nil, errors.New("acp1 provider: SetValue not yet implemented")
}

// readLoop reads datagrams until ctx is cancelled or the conn is closed.
// Each message is dispatched inline. Session.go owns the dispatch logic;
// this file only does transport.
func (s *server) readLoop(ctx context.Context, conn *net.UDPConn) error {
	buf := make([]byte, 1500) // covers the 141-byte ACP1 max + headroom
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, src, err := conn.ReadFromUDP(buf)
		if err != nil {
			return err
		}
		if n == 0 {
			continue
		}
		s.handleDatagram(conn, src, append([]byte(nil), buf[:n]...))
	}
}

// handleDatagram is the per-request entry point. Implemented in session.go
// so encoder.go / value.go can stay focused.
func (s *server) handleDatagram(conn *net.UDPConn, src *net.UDPAddr, data []byte) {
	// Stub: log-and-drop until session.go lands in the next commit.
	s.logger.Debug("acp1 provider: received datagram (dispatch not yet wired)",
		slog.String("src", src.String()),
		slog.Int("bytes", len(data)),
	)
	_ = conn
}
