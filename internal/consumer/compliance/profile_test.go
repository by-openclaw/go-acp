package compliance

import (
	"log/slog"
	"testing"
	"time"
)

// TestProfile_NoteTimestamps verifies Note records first/last
// timestamps and the counter increments.
func TestProfile_NoteTimestamps(t *testing.T) {
	p := &Profile{}
	t0 := time.Now()
	p.Note("kind-A")
	time.Sleep(2 * time.Millisecond)
	p.Note("kind-A")
	time.Sleep(2 * time.Millisecond)
	p.Note("kind-B")

	ec := p.SnapshotEvents()
	if len(ec) != 2 {
		t.Fatalf("want 2 kinds; got %d (%v)", len(ec), ec)
	}
	if ec[0].Kind != "kind-A" || ec[1].Kind != "kind-B" {
		t.Errorf("kinds not sorted: %v", ec)
	}
	if ec[0].Count != 2 {
		t.Errorf("kind-A count = %d; want 2", ec[0].Count)
	}
	if ec[0].FirstSeen.Before(t0) {
		t.Errorf("first_seen %v < t0 %v", ec[0].FirstSeen, t0)
	}
	if !ec[0].LastSeen.After(ec[0].FirstSeen) {
		t.Errorf("last_seen %v not after first_seen %v", ec[0].LastSeen, ec[0].FirstSeen)
	}
}

// TestProfile_Observe_RingPopulated verifies Observe pushes onto the
// ring with full attrs preserved.
func TestProfile_Observe_RingPopulated(t *testing.T) {
	p := NewProfileWithRing(4)
	p.Observe("steal",
		slog.String("matrix", "router.matrix"),
		slog.Int64("target", 5),
	)
	p.Observe("steal",
		slog.String("matrix", "router.matrix"),
		slog.Int64("target", 6),
	)

	obs := p.Observations(0)
	if len(obs) != 2 {
		t.Fatalf("want 2 observations; got %d", len(obs))
	}
	if obs[0].Kind != "steal" || obs[1].Kind != "steal" {
		t.Errorf("kinds = %+v", obs)
	}
	if obs[0].Attrs[1].Value.Int64() != 5 {
		t.Errorf("first observation target = %v; want 5", obs[0].Attrs[1].Value)
	}
	if obs[1].Attrs[1].Value.Int64() != 6 {
		t.Errorf("second observation target = %v; want 6", obs[1].Attrs[1].Value)
	}
}

// TestProfile_Observe_RingWrap verifies the ring wraps and returns
// the newer entries in chronological order after overflow.
func TestProfile_Observe_RingWrap(t *testing.T) {
	p := NewProfileWithRing(3)
	for i := 0; i < 5; i++ {
		p.Observe("k", slog.Int("i", i))
	}
	obs := p.Observations(0)
	if len(obs) != 3 {
		t.Fatalf("ring size 3, got %d", len(obs))
	}
	// Oldest surviving entry should be i=2 (i=0 and i=1 evicted).
	if got := obs[0].Attrs[0].Value.Int64(); got != 2 {
		t.Errorf("oldest i = %d; want 2", got)
	}
	if got := obs[2].Attrs[0].Value.Int64(); got != 4 {
		t.Errorf("newest i = %d; want 4", got)
	}
}

// TestProfile_SinceFilter verifies SnapshotEventsSince filters out
// events whose last_seen falls outside the window.
func TestProfile_SinceFilter(t *testing.T) {
	p := &Profile{}
	p.Note("old")
	// Forge the timestamp to 1h ago.
	p.mu.Lock()
	p.counters["old"].firstNano.Store(time.Now().Add(-2 * time.Hour).UnixNano())
	p.counters["old"].lastNano.Store(time.Now().Add(-1 * time.Hour).UnixNano())
	p.mu.Unlock()
	p.Note("recent")

	got := p.SnapshotEventsSince(10 * time.Minute)
	if len(got) != 1 || got[0].Kind != "recent" {
		t.Errorf("since=10m filter: got %v; want only 'recent'", got)
	}
	got = p.SnapshotEventsSince(0)
	if len(got) != 2 {
		t.Errorf("since=0 (no filter): got %v; want 2 entries", got)
	}
}

// TestProfile_Observations_SinceFilter verifies the ring filter.
func TestProfile_Observations_SinceFilter(t *testing.T) {
	p := NewProfileWithRing(8)
	p.Observe("k", slog.Int("i", 1))
	// Backdate the first observation.
	p.eventsMu.Lock()
	p.events[0].At = time.Now().Add(-2 * time.Hour)
	p.eventsMu.Unlock()
	p.Observe("k", slog.Int("i", 2))

	got := p.Observations(10 * time.Minute)
	if len(got) != 1 || got[0].Attrs[0].Value.Int64() != 2 {
		t.Errorf("since=10m filter: got %+v", got)
	}
}

// TestProfile_ZeroValueObserveSkipsRing verifies the zero-value
// Profile (no ring) still updates counters from Observe but skips
// ring storage.
func TestProfile_ZeroValueObserveSkipsRing(t *testing.T) {
	p := &Profile{}
	p.Observe("k", slog.Int("i", 1))
	if p.SnapshotEvents()[0].Count != 1 {
		t.Error("counter not bumped on zero-value profile")
	}
	if obs := p.Observations(0); obs != nil {
		t.Errorf("zero-value profile should not record observations: %v", obs)
	}
}

// TestProfile_BackCompat_Snapshot verifies the legacy Snapshot()
// surface still returns the same map[string]int64 shape.
func TestProfile_BackCompat_Snapshot(t *testing.T) {
	p := &Profile{}
	p.Note("k")
	p.Note("k")
	p.Note("j")
	snap := p.Snapshot()
	if snap["k"] != 2 || snap["j"] != 1 {
		t.Errorf("legacy Snapshot = %v", snap)
	}
}
