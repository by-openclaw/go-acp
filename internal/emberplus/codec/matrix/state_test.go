package matrix

import (
	"strings"
	"testing"
	"time"

	"dhs/internal/emberplus/codec/glow"
)

// TestSourceSetEqual covers every branch of the order-independent source-set
// comparison used by ApplyConnectionReport to decide whether a crosspoint
// actually changed (length mismatch, empty, reordered-equal, value mismatch).
func TestSourceSetEqual(t *testing.T) {
	cases := []struct {
		name string
		a, b []int32
		want bool
	}{
		{"both empty", nil, []int32{}, true},
		{"equal same order", []int32{1, 2, 3}, []int32{1, 2, 3}, true},
		{"equal reordered", []int32{1, 2, 3}, []int32{3, 1, 2}, true},
		{"length mismatch", []int32{1, 2}, []int32{1, 2, 3}, false},
		{"same length different element", []int32{1, 2, 3}, []int32{1, 2, 4}, false},
		{"duplicate vs distinct", []int32{1, 1, 2}, []int32{1, 2, 2}, false},
	}
	for _, c := range cases {
		if got := sourceSetEqual(c.a, c.b); got != c.want {
			t.Errorf("%s: sourceSetEqual(%v,%v) = %v, want %v", c.name, c.a, c.b, got, c.want)
		}
	}
}

// TestNewStateFromGlow pins the wholesale build from a decoded Glow Matrix:
// every static Contents field is mirrored and every Connection becomes a
// TargetState attributed to ChangeWalk. Spec p.88-89.
func TestNewStateFromGlow(t *testing.T) {
	before := time.Now()
	m := &glow.Matrix{
		MatrixType:           glow.MatrixTypeNToN,
		AddressingMode:       1,
		TargetCount:          8,
		SourceCount:          16,
		MaxTotalConnects:     32,
		MaxConnectsPerTarget: 4,
		Connections: []glow.Connection{
			{Target: 0, Sources: []int32{1, 2}, Operation: glow.ConnOpAbsolute, Disposition: glow.ConnDispTally},
			{Target: 3, Sources: []int32{5}, Operation: glow.ConnOpConnect, Disposition: glow.ConnDispModified},
		},
	}
	s := NewStateFromGlow(m)
	if s.Type != glow.MatrixTypeNToN || s.AddressingMode != 1 ||
		s.TargetCount != 8 || s.SourceCount != 16 ||
		s.MaxTotalConnects != 32 || s.MaxConnectsPerTarget != 4 {
		t.Fatalf("static fields not mirrored: %+v", s)
	}
	if len(s.Targets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(s.Targets))
	}
	t0 := s.Targets[0]
	if t0 == nil || len(t0.Sources) != 2 || t0.Sources[0] != 1 || t0.Sources[1] != 2 {
		t.Fatalf("target 0 sources wrong: %+v", t0)
	}
	if t0.ChangedBy != ChangeWalk {
		t.Errorf("target 0 ChangedBy = %v, want ChangeWalk", t0.ChangedBy)
	}
	if t0.LastChanged.Before(before) {
		t.Errorf("target 0 LastChanged not set")
	}
	// Mutating the source slice in the decoded matrix must not alias state.
	m.Connections[0].Sources[0] = 99
	if s.Targets[0].Sources[0] != 1 {
		t.Errorf("NewStateFromGlow aliased the wire source slice")
	}
}

// TestSnapshot_LabelsAndGain covers the deep-copy branches for the
// UI-only LabelSources slice and ResolvedGainDb map.
func TestSnapshot_LabelsAndGain(t *testing.T) {
	s := &State{
		Type: glow.MatrixTypeNToN,
		Targets: map[int32]*TargetState{
			1: {
				Target:         1,
				Sources:        []int32{2, 3},
				LabelTarget:    "MV-1",
				LabelSources:   []string{"CAM-2", "CAM-3"},
				ResolvedGainDb: map[int32]float64{2: -3.0, 3: 0.0},
			},
		},
	}
	snap := s.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(snap))
	}
	got := snap[0]
	if len(got.LabelSources) != 2 || got.LabelSources[0] != "CAM-2" {
		t.Fatalf("LabelSources not copied: %+v", got.LabelSources)
	}
	if got.ResolvedGainDb[2] != -3.0 {
		t.Fatalf("ResolvedGainDb not copied: %+v", got.ResolvedGainDb)
	}
	// Deep-copy: mutating the snapshot must not touch source state.
	got.LabelSources[0] = "X"
	got.ResolvedGainDb[2] = 99
	if s.Targets[1].LabelSources[0] != "CAM-2" || s.Targets[1].ResolvedGainDb[2] != -3.0 {
		t.Errorf("Snapshot did not deep-copy label/gain")
	}
}

func TestCanConnect_OneToN(t *testing.T) {
	s := &State{
		Type:        glow.MatrixTypeOneToN,
		TargetCount: 4, SourceCount: 4,
		Targets: map[int32]*TargetState{
			1: {Target: 1, Sources: []int32{2}},
		},
	}
	if err := s.CanConnect(1, []int32{5}, glow.ConnOpAbsolute); err != nil {
		t.Fatalf("oneToN absolute should accept replacing sole source: %v", err)
	}
	if err := s.CanConnect(1, []int32{3, 5}, glow.ConnOpAbsolute); err == nil {
		t.Fatal("oneToN should reject 2 sources on one target")
	}
	if err := s.CanConnect(1, []int32{5}, glow.ConnOpConnect); err == nil {
		t.Fatal("oneToN should reject add that exceeds 1 source (2 total)")
	}
}

// TestCanConnect_OneToOne_SourceStealAccepted pins #465: oneToOne
// source-already-used is NOT rejected by CanConnect. Every shipping
// Ember+ provider (Lawo VSM, EmberPlusView, TinyEmber+) implements
// source-steal — the source is implicitly disconnected from its prior
// target on receive. The consumer accepts the SET and the
// plugin.MatrixConnect layer fires a compliance event.
func TestCanConnect_OneToOne_SourceStealAccepted(t *testing.T) {
	s := &State{
		Type:        glow.MatrixTypeOneToOne,
		TargetCount: 4, SourceCount: 4,
		Targets: map[int32]*TargetState{
			1: {Target: 1, Sources: []int32{3}},
		},
	}
	// Source 3 currently routed to target 1; setting it on target 2
	// would have been rejected pre-#465. Now accepted (provider steals).
	if err := s.CanConnect(2, []int32{3}, glow.ConnOpAbsolute); err != nil {
		t.Errorf("oneToOne source-steal must be accepted (provider handles disconnect): %v", err)
	}
	// Unused source on a fresh target still works.
	if err := s.CanConnect(2, []int32{5}, glow.ConnOpAbsolute); err != nil {
		t.Errorf("oneToOne should accept unused source: %v", err)
	}
	// Cardinality-1 invariant (≤1 source per target) still enforced.
	if err := s.CanConnect(2, []int32{5, 6}, glow.ConnOpAbsolute); err == nil {
		t.Error("oneToOne should still reject 2 sources on a target [spec p.33 cardinality]")
	}
}

// TestDetectOneToOneSourceSteal pins the steal-detection helper used
// by the consumer's MatrixConnect to fire OneToOneSourceStealAccepted.
func TestDetectOneToOneSourceSteal(t *testing.T) {
	s := &State{
		Type:        glow.MatrixTypeOneToOne,
		TargetCount: 4, SourceCount: 4,
		Targets: map[int32]*TargetState{
			1: {Target: 1, Sources: []int32{3}},
			0: {Target: 0, Sources: []int32{0}},
		},
	}
	// Setting target 2 ← source 3 steals source 3 from target 1.
	stolen := s.DetectOneToOneSourceSteal(2, []int32{3})
	if len(stolen) != 1 || stolen[0].FromTarget != 1 || stolen[0].Source != 3 {
		t.Errorf("steal-detect = %+v, want [{FromTarget:1 Source:3}]", stolen)
	}
	// Setting target 5 ← source 99 (neither routed) — no steal.
	if stolen := s.DetectOneToOneSourceSteal(5, []int32{99}); len(stolen) != 0 {
		t.Errorf("steal-detect on unrouted source = %+v, want empty", stolen)
	}
	// Setting target 0 ← source 0 (self, no other target has it) — no steal.
	if stolen := s.DetectOneToOneSourceSteal(0, []int32{0}); len(stolen) != 0 {
		t.Errorf("steal-detect on self-routed source = %+v, want empty", stolen)
	}
	// Non-oneToOne matrix returns nil regardless of state.
	s.Type = glow.MatrixTypeNToN
	if stolen := s.DetectOneToOneSourceSteal(2, []int32{3}); stolen != nil {
		t.Errorf("steal-detect on nToN = %+v, want nil", stolen)
	}
}

func TestCanConnect_NToN_Caps(t *testing.T) {
	s := &State{
		Type:                 glow.MatrixTypeNToN,
		TargetCount:          4,
		SourceCount:          4,
		MaxConnectsPerTarget: 2,
		MaxTotalConnects:     3,
		Targets: map[int32]*TargetState{
			1: {Target: 1, Sources: []int32{1, 2}},
		},
	}
	if err := s.CanConnect(1, []int32{3}, glow.ConnOpConnect); err == nil {
		t.Fatal("nToN should reject exceeding MaxConnectsPerTarget")
	}
	if err := s.CanConnect(2, []int32{1, 2}, glow.ConnOpAbsolute); err == nil {
		t.Fatal("nToN should reject exceeding MaxTotalConnects (would total 4)")
	}
	if err := s.CanConnect(2, []int32{1}, glow.ConnOpAbsolute); err != nil {
		t.Fatalf("nToN should accept within caps: %v", err)
	}
}

// TestCanConnect_NToN_TotalAcrossTargets exercises the MaxTotalConnects
// sum loop over OTHER targets (skips the target under test) and the
// projectSources Disconnect branch.
func TestCanConnect_NToN_TotalAcrossTargets(t *testing.T) {
	s := &State{
		Type:                 glow.MatrixTypeNToN,
		TargetCount:          4,
		SourceCount:          4,
		MaxConnectsPerTarget: 4,
		MaxTotalConnects:     4,
		Targets: map[int32]*TargetState{
			1: {Target: 1, Sources: []int32{1, 2}},
			2: {Target: 2, Sources: []int32{3}},
		},
	}
	// Targets 1+2 already hold 3 connects. Adding 2 to target 3 (absolute)
	// would total 5 > 4 — must be rejected via the cross-target sum loop.
	if err := s.CanConnect(3, []int32{1, 4}, glow.ConnOpAbsolute); err == nil {
		t.Fatal("nToN should reject when cross-target total exceeds MaxTotalConnects")
	}
	// Disconnect on target 1 projects to fewer sources (projectSources
	// Disconnect branch); result stays within caps.
	if err := s.CanConnect(1, []int32{2}, glow.ConnOpDisconnect); err != nil {
		t.Fatalf("nToN disconnect within caps should accept: %v", err)
	}
}

func TestCanConnect_LockedTarget(t *testing.T) {
	s := &State{
		Type:        glow.MatrixTypeOneToN,
		TargetCount: 2, SourceCount: 2,
		Targets: map[int32]*TargetState{
			1: {Target: 1, Disposition: glow.ConnDispLocked},
		},
	}
	err := s.CanConnect(1, []int32{1}, glow.ConnOpAbsolute)
	if err == nil || !strings.Contains(err.Error(), "locked") {
		t.Fatalf("locked target should be rejected; got %v", err)
	}
}

func TestApplyConnection_ConnectAndDisconnect(t *testing.T) {
	s := &State{
		Type:    glow.MatrixTypeNToN,
		Targets: map[int32]*TargetState{},
	}
	s.ApplyConnection(glow.Connection{Target: 1, Sources: []int32{1, 2}, Operation: glow.ConnOpAbsolute}, ChangeWalk)
	s.ApplyConnection(glow.Connection{Target: 1, Sources: []int32{3}, Operation: glow.ConnOpConnect}, ChangeAnnounce)
	s.ApplyConnection(glow.Connection{Target: 1, Sources: []int32{1}, Operation: glow.ConnOpDisconnect}, ChangeAnnounce)

	snap := s.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 target, got %d", len(snap))
	}
	got := snap[0].Sources
	want := map[int32]bool{2: true, 3: true}
	if len(got) != 2 || !want[got[0]] || !want[got[1]] {
		t.Fatalf("expected sources {2,3} in any order, got %v", got)
	}
}
