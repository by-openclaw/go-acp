// Selective-import filter tests (issue #45).
//
// Verifies ImportFilter.Matches + ApplyFilter against the three
// sample snapshots (ACP1, ACP2, Ember+) defined in csv_lossless_test.go
// — covers every protocol's addressing model:
//   - ACP1: per-group unique ID, labels unique within group
//   - ACP2: globally-unique u32 ID, labels collide across sub-nodes
//   - Ember+: OID-based, labels collide across channels
//
// No device required; pure in-memory snapshot manipulation.
package export_test

import (
	"testing"

	"acp/internal/export"
	"acp/internal/protocol"
)

// TestFilter_EmptyPassesEverything — nil filter and zero-value filter
// both leave the snapshot untouched and report zero removed. This is
// today's default behaviour (no flags = apply all writable).
func TestFilter_EmptyPassesEverything(t *testing.T) {
	cases := []struct {
		name   string
		filter *export.ImportFilter
	}{
		{"nil", nil},
		{"zero-value", &export.ImportFilter{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			snap := acp2Snapshot()
			before := countObjects(snap)
			removed := export.ApplyFilter(snap, c.filter)
			after := countObjects(snap)
			if removed != 0 {
				t.Errorf("removed got %d, want 0", removed)
			}
			if before != after {
				t.Errorf("object count changed: %d → %d", before, after)
			}
		})
	}
}

// TestFilter_ByID — filtering by ID narrows to exactly the listed
// objects across all 3 protocols. Verifies the ACP2 duplicate-label
// case specifically — PSU.1/Present (id=60001) and PSU.2/Present
// (id=60002) both have label "Present" but are addressable by ID.
func TestFilter_ByID(t *testing.T) {
	snap := acp2Snapshot()
	filter := &export.ImportFilter{IDs: []int{60001}}
	removed := export.ApplyFilter(snap, filter)

	// Original ACP2 snapshot: 4 objects. Keep 1, remove 3.
	if removed != 3 {
		t.Errorf("removed got %d, want 3", removed)
	}
	if n := countObjects(snap); n != 1 {
		t.Fatalf("objects after filter got %d, want 1", n)
	}
	got := snap.Slots[0].Objects[0]
	if got.ID != 60001 || got.Label != "Present" {
		t.Errorf("surviving object got (id=%d label=%q), want (60001, \"Present\")",
			got.ID, got.Label)
	}
}

// TestFilter_ByPath — filtering by dotted path works when labels
// collide. Ember+ test snapshot has two "gain" objects under
// different channels; path selects one unambiguously.
func TestFilter_ByPath(t *testing.T) {
	snap := emberplusSnapshot()
	filter := &export.ImportFilter{Paths: []string{"router.inputs.ch2.gain"}}
	removed := export.ApplyFilter(snap, filter)

	if removed != 2 {
		t.Errorf("removed got %d, want 2", removed)
	}
	if n := countObjects(snap); n != 1 {
		t.Fatalf("objects after filter got %d, want 1", n)
	}
	got := snap.Slots[0].Objects[0]
	if got.OID != "1.2.2.3" {
		t.Errorf("surviving object got OID %q, want \"1.2.2.3\"", got.OID)
	}
}

// TestFilter_ByMultipleIDs — two IDs via repeated --id flags.
func TestFilter_ByMultipleIDs(t *testing.T) {
	snap := acp2Snapshot()
	filter := &export.ImportFilter{IDs: []int{60001, 60002}}
	removed := export.ApplyFilter(snap, filter)

	if removed != 2 {
		t.Errorf("removed got %d, want 2", removed)
	}
	if n := countObjects(snap); n != 2 {
		t.Fatalf("objects after filter got %d, want 2", n)
	}
}

// TestFilter_ByMultiplePaths — two paths via repeated --path flags.
func TestFilter_ByMultiplePaths(t *testing.T) {
	snap := emberplusSnapshot()
	filter := &export.ImportFilter{
		Paths: []string{"router.inputs.ch1.gain", "router.inputs.ch2.gain"},
	}
	removed := export.ApplyFilter(snap, filter)

	if removed != 1 {
		t.Errorf("removed got %d, want 1", removed)
	}
	if n := countObjects(snap); n != 2 {
		t.Fatalf("objects after filter got %d, want 2", n)
	}
}

// TestFilter_NoMatches — filter targets IDs/paths not present; every
// object is removed. Protects against a silent-match bug.
func TestFilter_NoMatches(t *testing.T) {
	snap := acp1Snapshot()
	before := countObjects(snap)
	filter := &export.ImportFilter{IDs: []int{99999}}
	removed := export.ApplyFilter(snap, filter)

	if removed != before {
		t.Errorf("removed got %d, want %d (all)", removed, before)
	}
	if n := countObjects(snap); n != 0 {
		t.Errorf("objects after filter got %d, want 0", n)
	}
}

// TestFilter_MatchesHelper — verifies the Matches predicate directly,
// without ApplyFilter. Useful for future callers (REST API) that want
// to evaluate filter membership per object.
func TestFilter_MatchesHelper(t *testing.T) {
	obj := protocol.Object{
		ID:    42,
		Label: "Gain",
		Path:  []string{"BOARD", "Gain"},
		OID:   "1.1.42",
	}
	cases := []struct {
		name   string
		filter *export.ImportFilter
		want   bool
	}{
		{"empty filter matches", &export.ImportFilter{}, true},
		{"id match", &export.ImportFilter{IDs: []int{42}}, true},
		{"id mismatch", &export.ImportFilter{IDs: []int{99}}, false},
		{"path match", &export.ImportFilter{Paths: []string{"BOARD.Gain"}}, true},
		{"path mismatch", &export.ImportFilter{Paths: []string{"BOARD.Mute"}}, false},
		{"id set but path set too — both checked", &export.ImportFilter{
			IDs: []int{99}, Paths: []string{"BOARD.Gain"},
		}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := c.filter.Matches(obj)
			if got != c.want {
				t.Errorf("Matches got %v, want %v", got, c.want)
			}
		})
	}
}
