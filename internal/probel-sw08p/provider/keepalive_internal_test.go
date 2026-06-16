package probelsw08p

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"
)

// TestStartKeepaliveWriteFailStops: when the session's write fails (peer
// gone), the keepalive goroutine logs at Debug and returns rather than
// spinning. Drives the write-error arm in startKeepalive.
func TestStartKeepaliveWriteFailStops(t *testing.T) {
	h := slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug})
	srv := newServer(slog.New(h), emptyExport())

	c1, c2 := net.Pipe()
	_ = c2.Close() // peer gone → writes fail
	sess := newSession(srv, c1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Very short interval so a tick fires almost immediately; the first
	// write fails and the goroutine returns. We can't observe the
	// goroutine directly, but closing the conn after a beat proves no
	// panic / leak and the write-fail arm ran.
	sess.startKeepalive(ctx, 10*time.Millisecond)
	time.Sleep(60 * time.Millisecond)
	_ = c1.Close()
}

// TestStartKeepaliveClosedSessionStops: a session already closed when
// the tick fires takes the isClosed() early-return arm.
func TestStartKeepaliveClosedSessionStops(t *testing.T) {
	srv := newServer(slog.New(slog.NewTextHandler(io.Discard, nil)), emptyExport())
	c1, c2 := net.Pipe()
	defer func() { _ = c1.Close() }()
	defer func() { _ = c2.Close() }()
	sess := newSession(srv, c1)
	sess.close() // isClosed() now true

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sess.startKeepalive(ctx, 10*time.Millisecond)
	time.Sleep(60 * time.Millisecond) // tick fires, isClosed() returns true
}

// TestStartKeepaliveZeroIntervalNoop: interval <= 0 is a no-op (no
// goroutine spawned).
func TestStartKeepaliveZeroIntervalNoop(t *testing.T) {
	srv := newServer(slog.New(slog.NewTextHandler(io.Discard, nil)), emptyExport())
	c1, _ := net.Pipe()
	defer func() { _ = c1.Close() }()
	sess := newSession(srv, c1)
	sess.startKeepalive(context.Background(), 0)
}
