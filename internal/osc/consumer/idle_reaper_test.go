package osc

// The idle reaper on the passive TCP listener. Off by default: OSC defines no
// heartbeat, so a control surface that sends nothing until someone moves a
// fader is healthy, and a default-on deadline would disconnect working peers.

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestTCPSessionIdleReaperDefaultsOff(t *testing.T) {
	s := newTCPSession(framerLenPrefix)
	if got := s.IdleTimeout(); got != 0 {
		t.Fatalf("IdleTimeout = %v on a fresh session, want 0 (off) — a silent "+
			"OSC peer is healthy, so reaping must be opt-in", got)
	}
}

func TestTCPSessionSetIdleTimeout(t *testing.T) {
	s := newTCPSession(framerSLIP)
	s.SetIdleTimeout(45 * time.Second)
	if got := s.IdleTimeout(); got != 45*time.Second {
		t.Fatalf("IdleTimeout = %v, want 45s", got)
	}
	s.SetIdleTimeout(-1)
	if got := s.IdleTimeout(); got != 0 {
		t.Fatalf("IdleTimeout = %v for a negative input, want 0", got)
	}
}

// With the reaper armed, a peer that connects and then says nothing is
// disconnected instead of holding a goroutine and a socket indefinitely.
func TestTCPConnLoopReapsSilentPeer(t *testing.T) {
	s := newTCPSession(framerLenPrefix)
	s.SetIdleTimeout(120 * time.Millisecond)
	if err := s.listen(context.Background(), "127.0.0.1:0"); err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = s.close() }()

	conn, err := net.Dial("tcp", s.boundAddr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	if _, err := conn.Read(make([]byte, 1)); err == nil {
		t.Fatal("expected the reaper to close the idle connection")
	}
}
