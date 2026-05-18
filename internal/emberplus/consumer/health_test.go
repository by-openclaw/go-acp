package emberplus

import (
	"context"
	"testing"
	"time"

	"dhs/internal/consumer"
)

// TestSessionHealth_NotConnected asserts a fresh plugin (no Connect
// yet) reports Reachable=false / Connected=false / Live=false.
func TestSessionHealth_NotConnected(t *testing.T) {
	p := &Plugin{}
	snap := p.SessionHealth(context.Background())
	if snap.Reachable || snap.Connected || snap.Live {
		t.Errorf("fresh plugin = %+v; want all false", snap)
	}
	if snap.StaleAfter != emberplusStaleAfter {
		t.Errorf("StaleAfter = %v; want %v", snap.StaleAfter, emberplusStaleAfter)
	}
}

// TestSessionHealth_ReachableNotConnected asserts a plugin with
// connIP set (Connect captured target) but session still in handshake
// reports Reachable=true / Connected=false / Live=false.
func TestSessionHealth_ReachableNotConnected(t *testing.T) {
	p := &Plugin{connIP: "127.0.0.1", connPort: 9000}
	snap := p.SessionHealth(context.Background())
	if !snap.Reachable {
		t.Errorf("Reachable=false; want true")
	}
	if snap.Connected || snap.Live {
		t.Errorf("Connected=%v Live=%v; want both false", snap.Connected, snap.Live)
	}
}

// TestSessionHealth_LiveAtThreshold asserts the IsLiveAt boundary
// matches the spec for the Ember+ window.
func TestSessionHealth_LiveAtThreshold(t *testing.T) {
	now := time.Now()
	stale := emberplusStaleAfter
	snap := consumer.SessionHealth{
		LastRx:     now.Add(-stale + time.Second), // just inside window
		StaleAfter: stale,
	}
	if !snap.IsLiveAt(now) {
		t.Errorf("just-inside-window snapshot not Live")
	}
	snap.LastRx = now.Add(-stale - time.Second) // just outside
	if snap.IsLiveAt(now) {
		t.Errorf("just-outside-window snapshot Live = true; want false")
	}
}

// TestSessionHealth_HealthCheckerSatisfied asserts the Plugin
// satisfies the consumer.HealthChecker interface so the CLI verb
// type-assert succeeds at runtime.
func TestSessionHealth_HealthCheckerSatisfied(t *testing.T) {
	var _ consumer.HealthChecker = &Plugin{}
}
