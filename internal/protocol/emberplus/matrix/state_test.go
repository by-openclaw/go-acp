package matrix

import (
	"strings"
	"testing"

	"acp/internal/protocol/emberplus/glow"
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

func TestCanConnect_OneToOne_SourceExclusive(t *testing.T) {
	s := &State{
		Type:        glow.MatrixTypeOneToOne,
		TargetCount: 4, SourceCount: 4,
		Targets: map[int32]*TargetState{
			1: {Target: 1, Sources: []int32{3}},
		},
	}
	if err := s.CanConnect(2, []int32{3}, glow.ConnOpAbsolute); err == nil {
		t.Fatal("oneToOne should reject source 3 on target 2 when target 1 already uses it")
	}
	if err := s.CanConnect(2, []int32{5}, glow.ConnOpAbsolute); err != nil {
		t.Fatalf("oneToOne should accept unused source: %v", err)
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
