package acp1

import (
	"testing"
	"time"

	"dhs/internal/consumer"
)

// TestResolvedKeepAlive_Defaults asserts the per-knob fallback rules
// match the spec written into KeepAliveConfig: zero = plugin default,
// DisableInterval / DisableTimeout = explicit off.
func TestResolvedKeepAlive_Defaults(t *testing.T) {
	cases := []struct {
		name        string
		cfg         consumer.KeepAliveConfig
		wantInt     time.Duration
		wantTimeout time.Duration
	}{
		{
			name:        "zero/zero -> defaults",
			cfg:         consumer.KeepAliveConfig{},
			wantInt:     defaultKeepAliveInterval,
			wantTimeout: 3 * defaultKeepAliveInterval,
		},
		{
			name:        "explicit interval -> 3x default timeout",
			cfg:         consumer.KeepAliveConfig{Interval: 2 * time.Second},
			wantInt:     2 * time.Second,
			wantTimeout: 6 * time.Second,
		},
		{
			name:        "explicit interval + explicit timeout",
			cfg:         consumer.KeepAliveConfig{Interval: 2 * time.Second, Timeout: 30 * time.Second},
			wantInt:     2 * time.Second,
			wantTimeout: 30 * time.Second,
		},
		{
			name:        "DisableInterval -> no probes, default timeout",
			cfg:         consumer.KeepAliveConfig{Interval: consumer.DisableInterval},
			wantInt:     0,
			wantTimeout: defaultKeepAliveTimeout,
		},
		{
			name:        "DisableTimeout -> probes only",
			cfg:         consumer.KeepAliveConfig{Interval: 5 * time.Second, Timeout: consumer.DisableTimeout},
			wantInt:     5 * time.Second,
			wantTimeout: 0,
		},
		{
			name:        "DisableInterval + DisableTimeout -> fully off",
			cfg:         consumer.KeepAliveConfig{Interval: consumer.DisableInterval, Timeout: consumer.DisableTimeout},
			wantInt:     0,
			wantTimeout: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &Plugin{kaCfg: tc.cfg}
			gotInt, gotTimeout := p.resolvedKeepAlive()
			if gotInt != tc.wantInt {
				t.Errorf("interval = %v, want %v", gotInt, tc.wantInt)
			}
			if gotTimeout != tc.wantTimeout {
				t.Errorf("timeout = %v, want %v", gotTimeout, tc.wantTimeout)
			}
		})
	}
}

// TestSessionLive_NoSink covers the "not yet connected" case.
func TestSessionLive_NoSink(t *testing.T) {
	p := &Plugin{}
	if p.SessionLive() {
		t.Error("SessionLive should be false when tsSink is nil")
	}
}

// TestSessionLive_NoTimeout covers the watchdog-disabled case: a
// configured zero timeout means "never declare dead", so SessionLive
// returns true unconditionally.
func TestSessionLive_NoTimeout(t *testing.T) {
	p := &Plugin{
		tsSink: &timestampSink{},
		kaCfg:  consumer.KeepAliveConfig{Timeout: consumer.DisableTimeout},
	}
	if !p.SessionLive() {
		t.Error("SessionLive should be true when timeout is disabled")
	}
}

// TestSessionLive_FreshThenStale walks through the live -> cache
// transition the watchdog drives the watch verb's freshness column on.
func TestSessionLive_FreshThenStale(t *testing.T) {
	p := &Plugin{
		tsSink: &timestampSink{},
		kaCfg:  consumer.KeepAliveConfig{Interval: 100 * time.Millisecond, Timeout: 250 * time.Millisecond},
	}
	// Prime — same trick the real Connect path uses to skip the
	// connect-to-first-rx grace window.
	p.tsSink.recordRx()
	if !p.SessionLive() {
		t.Fatal("just-primed session should be live")
	}
	// Wait past the configured timeout.
	time.Sleep(300 * time.Millisecond)
	if p.SessionLive() {
		t.Error("session should be stale after timeout elapses without rx")
	}
	// Touch lastRx → live again.
	p.tsSink.recordRx()
	if !p.SessionLive() {
		t.Error("session should be live again after fresh rx")
	}
}
