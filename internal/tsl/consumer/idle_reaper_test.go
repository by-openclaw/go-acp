package tsl

// The idle reaper on the passive v5.0 TCP listener.
//
// Unlike the client-side connectors, this one is OFF by default and that is
// the tested contract: TSL defines no heartbeat (see internal/tsl/CLAUDE.md —
// "pcap audit confirmed VSM never sends one"), so a tally link that is silent
// for hours is healthy, and a default-on deadline would disconnect working
// producers. OS SO_KEEPALIVE remains the always-on detector.

import (
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
