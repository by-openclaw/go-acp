package emberplus

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"
)

// TestSession_DtdVersion_Latches pins the R6 #470 invariant: noteDtd
// records the FIRST non-zero DTD minor/major seen and ignores
// subsequent observations. A provider that downgrades mid-session
// (rare; spec forbids) must not retroactively flip the version.
func TestSession_DtdVersion_Latches(t *testing.T) {
	s := NewSession(slog.New(slog.NewTextHandler(io.Discard, nil)))

	if got := s.DtdVersion(); got != "" {
		t.Fatalf("pre-note DtdVersion: got %q, want empty", got)
	}

	s.noteDtd(0x3C, 0x02) // 2.60
	if got := s.DtdVersion(); got != "2.60" {
		t.Errorf("after first note: got %q, want 2.60", got)
	}

	// Second observation with different bytes must be ignored.
	s.noteDtd(0x0A, 0x02) // 2.10 — should NOT overwrite
	if got := s.DtdVersion(); got != "2.60" {
		t.Errorf("after second note: got %q, want 2.60 (latch broken)", got)
	}
}

// TestSession_NoteDtd_ZeroBytesIgnored guarantees the latch is not
// poisoned by a frame that arrived without app-bytes (5-byte header
// variant). A subsequent frame carrying the real bytes must still
// latch successfully.
func TestSession_NoteDtd_ZeroBytesIgnored(t *testing.T) {
	s := NewSession(slog.New(slog.NewTextHandler(io.Discard, nil)))

	s.noteDtd(0, 0)
	if got := s.DtdVersion(); got != "" {
		t.Errorf("zero-byte note populated version: %q", got)
	}

	s.noteDtd(0x3C, 0x02)
	if got := s.DtdVersion(); got != "2.60" {
		t.Errorf("post-zero note: got %q, want 2.60", got)
	}
}

// TestSession_WaitForDtdVersion_Notifies covers the probe path
// GetDeviceInfo uses: a caller blocks on WaitForDtdVersion while a
// concurrent goroutine (the readLoop in production) feeds the first
// frame's app-bytes via noteDtd. The wait must unblock and return the
// captured version.
func TestSession_WaitForDtdVersion_Notifies(t *testing.T) {
	s := NewSession(slog.New(slog.NewTextHandler(io.Discard, nil)))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan string, 1)
	go func() { done <- s.WaitForDtdVersion(ctx) }()

	// Give the waiter time to park on dtdReady.
	time.Sleep(20 * time.Millisecond)
	s.noteDtd(0x32, 0x02) // 2.50

	select {
	case got := <-done:
		if got != "2.50" {
			t.Errorf("WaitForDtdVersion: got %q, want 2.50", got)
		}
	case <-time.After(time.Second):
		t.Fatal("WaitForDtdVersion did not unblock after noteDtd")
	}
}

// TestSession_WaitForDtdVersion_ContextCancelled covers the
// no-app-bytes path: GetDeviceInfo's bounded probe must return ""
// instead of hanging when the provider never sends an EmBER frame.
func TestSession_WaitForDtdVersion_ContextCancelled(t *testing.T) {
	s := NewSession(slog.New(slog.NewTextHandler(io.Discard, nil)))

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	got := s.WaitForDtdVersion(ctx)
	if got != "" {
		t.Errorf("context-cancelled wait: got %q, want empty", got)
	}
}

// TestSession_WaitForDtdVersion_AlreadyLatched: a caller that arrives
// after the first frame must not block at all.
func TestSession_WaitForDtdVersion_AlreadyLatched(t *testing.T) {
	s := NewSession(slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.noteDtd(0x3C, 0x02)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	got := s.WaitForDtdVersion(ctx)
	if got != "2.60" {
		t.Errorf("post-latch wait: got %q, want 2.60", got)
	}
	if elapsed := time.Since(start); elapsed > 20*time.Millisecond {
		t.Errorf("post-latch wait blocked for %s (expected near-instant)", elapsed)
	}
}
