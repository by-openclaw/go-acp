package matrix

import (
	"strings"
	"testing"

	"dhs/internal/emberplus/codec/glow"
)

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
