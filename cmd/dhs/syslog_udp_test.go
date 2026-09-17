package main

import (
	"context"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"
)

// TestSyslogUDPDeliversRFC5424 asserts that one log record arrives at
// the collector as one RFC 5424 datagram, byte-compatible with the
// `--log-format syslog` stderr line minus the trailing newline
// (RFC 5426 §3.1: no line terminator in the datagram).
func TestSyslogUDPDeliversRFC5424(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = pc.Close() }()

	q, err := dialSyslogUDP(pc.LocalAddr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	logger := slog.New(q.Handler(slog.LevelInfo))
	logger.Info("route set", "dst", 7, "src", "cam 1")
	q.Close()

	_ = pc.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 2048)
	n, _, err := pc.ReadFrom(buf)
	if err != nil {
		t.Fatalf("read datagram: %v", err)
	}
	got := string(buf[:n])

	// <134> = facility local0(16)*8 + severity info(6), per the stderr
	// handler's mapping (#751 G6).
	if !strings.HasPrefix(got, "<134>1 ") {
		t.Fatalf("datagram PRI/version = %q, want prefix %q", got, "<134>1 ")
	}
	if !strings.Contains(got, " dhs ") {
		t.Fatalf("datagram missing APP-NAME 'dhs': %q", got)
	}
	if !strings.Contains(got, "route set dst=7 src=\"cam 1\"") {
		t.Fatalf("datagram MSG = %q, want message + key=val attrs", got)
	}
	if strings.HasSuffix(got, "\n") {
		t.Fatalf("datagram must not carry a trailing newline: %q", got)
	}
}

// TestSyslogUDPNeverBlocks pins the non-blocking contract: with no
// writer draining the queue, enqueues past the depth return immediately
// and are counted as drops instead of stalling the caller.
func TestSyslogUDPNeverBlocks(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = pc.Close() }()
	conn, err := net.Dial("udp", pc.LocalAddr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	// Hand-built queue with capacity 2 and NO writer goroutine, so the
	// third enqueue must take the drop path, not block.
	q := &syslogUDP{addr: "test", conn: conn, ch: make(chan string, 2)}

	done := make(chan struct{})
	go func() {
		defer close(done)
		h := q.Handler(slog.LevelInfo)
		r := slog.NewRecord(time.Now(), slog.LevelInfo, "m", 0)
		for i := 0; i < 5; i++ {
			_ = h.Handle(context.Background(), r)
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Handle blocked on a full queue")
	}
	if got := q.dropped.Load(); got != 3 {
		t.Fatalf("dropped = %d, want 3 (5 enqueues into cap-2 queue)", got)
	}
	_ = conn.Close()
}

// TestTeeHandlerFansOut asserts both underlying handlers see the
// record and that per-handler Enabled gating holds.
func TestTeeHandlerFansOut(t *testing.T) {
	var a, b strings.Builder
	tee := teeHandler{
		newSyslogHandler(&a, slog.LevelInfo),
		newSyslogHandler(&b, slog.LevelError),
	}
	logger := slog.New(tee)
	logger.Info("only-a")
	logger.Error("both")

	if !strings.Contains(a.String(), "only-a") || !strings.Contains(a.String(), "both") {
		t.Fatalf("handler a saw %q, want both records", a.String())
	}
	if strings.Contains(b.String(), "only-a") {
		t.Fatalf("handler b (error-gated) must not see info records: %q", b.String())
	}
	if !strings.Contains(b.String(), "both") {
		t.Fatalf("handler b missing error record: %q", b.String())
	}
}
