package provider

// The --heartbeat operator knob (issue #855): the cadence fallback
// chain (IS-09 live value > --heartbeat default > IS-04 §6.1 5 s),
// the tick/slack scaling that makes sub-second cadences beat on time,
// and the loop proof against a counting fake Registration API — the
// node-side half of "the registry's GC is testable" (the registry's
// eviction half is pinned by its own store tests).

import (
	"context"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"dhs/internal/amwa/codec/is09"
)

func TestHeartbeatDefaultIntervalPrecedence(t *testing.T) {
	s := newTestNodeServer(t)
	rc := NewRegistrationClient(nil, "http://reg.invalid:8235", "v1.3", validBundle())
	rc.SetHeartbeatIntervalFn(s.systemHeartbeatInterval)

	// No IS-09 global, no flag: the spec default.
	if got := rc.heartbeatInterval(); got != HeartbeatInterval {
		t.Fatalf("bare: interval = %v, want %v", got, HeartbeatInterval)
	}

	// The --heartbeat default replaces the 5 s fallback.
	rc.SetDefaultHeartbeatInterval(2 * time.Second)
	if got := rc.heartbeatInterval(); got != 2*time.Second {
		t.Fatalf("with --heartbeat 2s: interval = %v, want 2s", got)
	}

	// An IS-09 global still outranks the flag — the System API is the
	// plant-wide operator config.
	s.applySystemGlobal(&is09.Global{
		ID: "0d0a0f0e-0000-4000-8000-000000000855", Version: "1:0",
		IS04: is09.IS04Config{HeartbeatInterval: 7},
	}, "http://sys.test/x-nmos/system/v1.0/global")
	if got := rc.heartbeatInterval(); got != 7*time.Second {
		t.Fatalf("IS-09 over flag: interval = %v, want 7s", got)
	}

	// Non-positive flag values keep the spec default in force.
	rc2 := NewRegistrationClient(nil, "http://reg.invalid:8235", "v1.3", validBundle())
	rc2.SetDefaultHeartbeatInterval(0)
	if got := rc2.heartbeatInterval(); got != HeartbeatInterval {
		t.Fatalf("zero flag: interval = %v, want %v", got, HeartbeatInterval)
	}
}

func TestHeartbeatTickAndSlackScale(t *testing.T) {
	cases := []struct {
		cadence, tick, slack time.Duration
	}{
		// The 5 s default keeps the historic loop numbers exactly.
		{5 * time.Second, time.Second, 500 * time.Millisecond},
		{2 * time.Second, time.Second, 500 * time.Millisecond},
		// Sub-2 s: both scale so the slack can't swallow the beat.
		{time.Second, 500 * time.Millisecond, 250 * time.Millisecond},
		{500 * time.Millisecond, 250 * time.Millisecond, 125 * time.Millisecond},
		// Floors: a pathological cadence cannot spin the loop.
		{20 * time.Millisecond, 50 * time.Millisecond, 10 * time.Millisecond},
	}
	for _, tc := range cases {
		if got := heartbeatTick(tc.cadence); got != tc.tick {
			t.Errorf("tick(%v) = %v, want %v", tc.cadence, got, tc.tick)
		}
		if got := heartbeatSlack(tc.cadence); got != tc.slack {
			t.Errorf("slack(%v) = %v, want %v", tc.cadence, got, tc.slack)
		}
	}
}

// countingRegistry is a minimal Registration API accepting resource
// POSTs and counting /health POSTs.
func countingRegistry(t *testing.T) (*httptest.Server, *atomic.Uint64) {
	t.Helper()
	var heartbeats atomic.Uint64
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/health/nodes/"):
			heartbeats.Add(1)
			w.WriteHeader(stdhttp.StatusOK)
			_, _ = w.Write([]byte(`{"health":"1"}`))
		case strings.HasSuffix(r.URL.Path, "/resource"):
			w.WriteHeader(stdhttp.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		default:
			stdhttp.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &heartbeats
}

// TestHeartbeatCadenceHonored drives the real Run loop against the
// counting fake: a 200 ms cadence must land several beats inside a
// window where the 5 s default lands at most the post-registration
// one — the tick and slack provably scaled down.
func TestHeartbeatCadenceHonored(t *testing.T) {
	run := func(cadence time.Duration) uint64 {
		srv, beats := countingRegistry(t)
		rc := NewRegistrationClient(nil, srv.URL, "v1.3", validBundle())
		if cadence > 0 {
			rc.SetDefaultHeartbeatInterval(cadence)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 1100*time.Millisecond)
		defer cancel()
		rc.Run(ctx)
		return beats.Load()
	}

	if fast := run(200 * time.Millisecond); fast < 4 {
		t.Errorf("cadence 200ms: %d heartbeats in ~1.1s, want >= 4", fast)
	}
	if slow := run(0); slow > 2 {
		t.Errorf("default cadence: %d heartbeats in ~1.1s, want <= 2", slow)
	}
}
