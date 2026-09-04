package osc

// The idle reaper on the passive TCP listener. Off by default: OSC defines no
// heartbeat, so a control surface that sends nothing until someone moves a
// fader is healthy, and a default-on deadline would disconnect working peers.

import (
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
