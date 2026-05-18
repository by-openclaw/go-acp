package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"
	"time"
)

// TestAsyncHandler_NonBlocking verifies Handle returns immediately even
// when the inner handler is intentionally slow. R15 #476 requires the
// hot path (Ember+ keepalive, Probel tally fan-out) to never block on
// stderr.
func TestAsyncHandler_NonBlocking(t *testing.T) {
	slow := &slowHandler{delay: 50 * time.Millisecond}
	h := NewAsyncHandlerN(slow, 100)
	defer h.Close() //nolint:errcheck

	start := time.Now()
	logger := slog.New(h)
	for i := 0; i < 50; i++ {
		logger.Info("ping", "i", i)
	}
	elapsed := time.Since(start)
	if elapsed > 10*time.Millisecond {
		t.Errorf("50 enqueues took %v; expected <10ms (handler is blocking)", elapsed)
	}
}

// TestAsyncHandler_DropsOnOverflow verifies records are dropped (not
// blocked) when the buffer fills.
func TestAsyncHandler_DropsOnOverflow(t *testing.T) {
	slow := &slowHandler{delay: 100 * time.Millisecond}
	h := NewAsyncHandlerN(slow, 4)
	defer h.Close() //nolint:errcheck

	logger := slog.New(h)
	const sent = 100
	for i := 0; i < sent; i++ {
		logger.Info("flood", "i", i)
	}
	// At least sent - buffer - 1 must have been dropped (one might be in the
	// drain goroutine when measured).
	got := h.DropCount()
	if got < uint64(sent-5-1) {
		t.Errorf("dropped=%d; expected most of %d sent records to drop", got, sent)
	}
}

// TestAsyncHandler_RendersLines verifies records actually reach the
// inner handler when given time to drain.
func TestAsyncHandler_RendersLines(t *testing.T) {
	var buf bytes.Buffer
	mu := sync.Mutex{}
	inner := slog.NewJSONHandler(&lockedWriter{w: &buf, mu: &mu}, nil)
	h := NewAsyncHandlerN(inner, 16)
	logger := slog.New(h)
	logger.Info("hello", "k", "v")

	if !h.FlushTimeout(500 * time.Millisecond) {
		t.Fatal("flush timeout")
	}
	if err := h.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	mu.Lock()
	out := buf.String()
	mu.Unlock()
	if out == "" {
		t.Fatal("no output rendered")
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode: %v: %q", err, out)
	}
	if got["msg"] != "hello" || got["k"] != "v" {
		t.Errorf("rendered = %v; want msg=hello k=v", got)
	}
}

// TestAsyncHandler_WithAttrsSharesGoroutine verifies child handlers
// returned by WithAttrs reuse the parent's channel + drain goroutine.
func TestAsyncHandler_WithAttrsSharesGoroutine(t *testing.T) {
	var buf bytes.Buffer
	mu := sync.Mutex{}
	inner := slog.NewJSONHandler(&lockedWriter{w: &buf, mu: &mu}, nil)
	parent := NewAsyncHandlerN(inner, 16)
	defer parent.Close() //nolint:errcheck

	child := parent.WithAttrs([]slog.Attr{slog.String("component", "test")})
	logger := slog.New(child)
	logger.Info("hi")

	if !parent.FlushTimeout(500 * time.Millisecond) {
		t.Fatal("flush timeout")
	}
	mu.Lock()
	out := buf.String()
	mu.Unlock()
	if out == "" {
		t.Fatal("no output rendered via child")
	}
}

// slowHandler is a slog.Handler whose Handle blocks for `delay`.
// Used to provoke the async-handler's drop path.
type slowHandler struct {
	delay time.Duration
}

func (s *slowHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (s *slowHandler) Handle(_ context.Context, _ slog.Record) error {
	time.Sleep(s.delay)
	return nil
}
func (s *slowHandler) WithAttrs(_ []slog.Attr) slog.Handler { return s }
func (s *slowHandler) WithGroup(_ string) slog.Handler     { return s }

// lockedWriter serialises writes from the drain goroutine + the test
// goroutine reading the buffer.
type lockedWriter struct {
	w  *bytes.Buffer
	mu *sync.Mutex
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}
