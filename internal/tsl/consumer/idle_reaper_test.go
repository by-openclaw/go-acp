package tsl

// The idle reaper on the passive v5.0 TCP listener.
//
// Unlike the client-side connectors, this one is OFF by default and that is
// the tested contract: TSL defines no heartbeat (see internal/tsl/CLAUDE.md —
// "pcap audit confirmed VSM never sends one"), so a tally link that is silent
// for hours is healthy, and a default-on deadline would disconnect working
// producers. OS SO_KEEPALIVE remains the always-on detector.

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"
)

func TestTCPSessionIdleReaperDefaultsOff(t *testing.T) {
	s := newTCPSession()
	if got := s.IdleTimeout(); got != 0 {
		t.Fatalf("IdleTimeout = %v on a fresh session, want 0 (off) — a silent "+
			"TSL producer is healthy, so reaping must be opt-in", got)
	}
}

func TestTCPSessionSetIdleTimeout(t *testing.T) {
	s := newTCPSession()
	s.SetIdleTimeout(90 * time.Second)
	if got := s.IdleTimeout(); got != 90*time.Second {
		t.Fatalf("IdleTimeout = %v, want 90s", got)
	}
	s.SetIdleTimeout(0)
	if got := s.IdleTimeout(); got != 0 {
		t.Fatalf("IdleTimeout = %v after disable, want 0", got)
	}
	s.SetIdleTimeout(-5 * time.Second)
	if got := s.IdleTimeout(); got != 0 {
		t.Fatalf("IdleTimeout = %v for a negative input, want 0", got)
	}
}

// SetTCPIdleTimeout is the plugin-level knob --idle-timeout drives. It must
// work before Connect (stored, applied at listen) and after (pushed straight
// through to the live session).
func TestSetTCPIdleTimeoutBeforeAndAfterConnect(t *testing.T) {
	p := NewPluginV50(slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Before Connect: recorded only.
	p.SetTCPIdleTimeout(70 * time.Second)
	p.SetTCPIdleTimeout(-1) // negative disables
	p.SetTCPIdleTimeout(200 * time.Millisecond)

	if err := p.ConnectV50TCP(context.Background(), "127.0.0.1", 0); err != nil {
		t.Fatalf("ConnectV50TCP: %v", err)
	}
	defer func() { _ = p.Disconnect() }()

	if got := p.tcpSession.IdleTimeout(); got != 200*time.Millisecond {
		t.Fatalf("session IdleTimeout = %v, want the pre-Connect value 200ms", got)
	}
	// After Connect: reaches the live session.
	p.SetTCPIdleTimeout(400 * time.Millisecond)
	if got := p.tcpSession.IdleTimeout(); got != 400*time.Millisecond {
		t.Fatalf("session IdleTimeout = %v after live update, want 400ms", got)
	}
}

// With the reaper armed, a producer that connects and then says nothing is
// disconnected instead of holding a goroutine and socket forever.
func TestTCPConnLoopReapsSilentProducer(t *testing.T) {
	p := NewPluginV50(slog.New(slog.NewTextHandler(io.Discard, nil)))
	p.SetTCPIdleTimeout(120 * time.Millisecond)
	if err := p.ConnectV50TCP(context.Background(), "127.0.0.1", 0); err != nil {
		t.Fatalf("ConnectV50TCP: %v", err)
	}
	defer func() { _ = p.Disconnect() }()

	addr := p.BoundTCPAddr()
	conn, err := net.Dial("tcp", addr.String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// Never write. The listener must close our connection once the window
	// elapses; a read then returns EOF/reset rather than blocking forever.
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	buf := make([]byte, 1)
	if _, err := conn.Read(buf); err == nil {
		t.Fatal("expected the reaper to close the idle connection")
	}
}
