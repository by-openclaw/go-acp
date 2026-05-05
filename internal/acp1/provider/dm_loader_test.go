package acp1

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"dhs/internal/acp1/codec"
	"dhs/internal/dmlib"
	"dhs/internal/export"
	"dhs/internal/protocol"
)

func TestParseCardPath(t *testing.T) {
	cases := []struct {
		in   string
		want dmlib.Fingerprint
		err  bool
	}{
		{
			in: "axon/synapse/RRS18-1601/acp1",
			want: dmlib.Fingerprint{
				Vendor: "axon", Product: "synapse",
				Model: "RRS18", SwRev: "1601", Proto: "acp1",
			},
		},
		{
			// Hyphenated model: split on the LAST '-'.
			in: "lawo/vsm/GIO-12-2000/acp2",
			want: dmlib.Fingerprint{
				Vendor: "lawo", Product: "vsm",
				Model: "GIO-12", SwRev: "2000", Proto: "acp2",
			},
		},
		{in: "too/few/parts", err: true},
		{in: "too/many/parts/here/now", err: true},
		{in: "axon//RRS18-1601/acp1", err: true},
		{in: "axon/synapse/RRS18/acp1", err: true},   // no rev separator
		{in: "axon/synapse/-1601/acp1", err: true},   // empty model
		{in: "axon/synapse/RRS18-/acp1", err: true},  // empty rev
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := parseCardPath(tc.in)
			if tc.err {
				if err == nil {
					t.Fatalf("expected error for %q", tc.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

// fakeDMResolver satisfies dmlib.Resolver with canned answers. When
// schemas is non-nil the resolver returns the schema keyed by the
// fingerprint's "Model-SwRev" string, so a single test can swap
// between models / firmware revs by editing the map in place.
type fakeDMResolver struct {
	resolveErr error
	calledFP   dmlib.Fingerprint
	schemas    map[string]*dmlib.Schema
}

func (r *fakeDMResolver) Resolve(fp dmlib.Fingerprint) (*dmlib.Schema, error) {
	r.calledFP = fp
	if r.resolveErr != nil {
		return nil, r.resolveErr
	}
	if r.schemas != nil {
		key := fp.Model + "-" + fp.SwRev
		if s, ok := r.schemas[key]; ok {
			return s, nil
		}
		return nil, dmlib.ErrNotFound
	}
	return &dmlib.Schema{
		Fingerprint: fp,
		Slots: map[int]*export.Snapshot{
			1: {Slots: []export.SlotDump{{Slot: 1, Objects: []protocol.Object{}}}},
		},
	}, nil
}
func (r *fakeDMResolver) LookupAlternate(fp dmlib.Fingerprint) ([]dmlib.Fingerprint, error) {
	return nil, nil
}
func (r *fakeDMResolver) Persist(s *dmlib.Schema) error      { return nil }
func (r *fakeDMResolver) Diff(p, c *dmlib.Schema) dmlib.Diff { return dmlib.Diff{} }

func TestSlotLoad_NoResolverConfigured(t *testing.T) {
	s := newTestServer(t)
	err := s.SlotLoad(context.Background(), 1, "axon/synapse/RRS18-1601/acp1")
	if !errors.Is(err, ErrNoDMLibrary) {
		t.Fatalf("err = %v, want ErrNoDMLibrary", err)
	}
}

func TestSlotLoad_BadCardPath(t *testing.T) {
	s := newTestServer(t)
	s.SetDMLibrary(&fakeDMResolver{})
	err := s.SlotLoad(context.Background(), 1, "totally bogus")
	if err == nil {
		t.Fatal("expected error for bad path")
	}
}

func TestSlotLoad_ResolverMiss(t *testing.T) {
	s := newTestServer(t)
	s.SetDMLibrary(&fakeDMResolver{resolveErr: dmlib.ErrNotFound})
	err := s.SlotLoad(context.Background(), 1, "axon/synapse/RRS18-1601/acp1")
	if !errors.Is(err, dmlib.ErrNotFound) {
		t.Fatalf("err = %v, want dmlib.ErrNotFound", err)
	}
}

func TestSlotLoad_DrivesCascade(t *testing.T) {
	s := newTestServer(t)
	s.SetInsertTiming(InsertTimingFast)
	s.SetDMLibrary(&fakeDMResolver{})
	if err := s.setSlotStatus(1, 0); err != nil {
		t.Fatalf("reset slot: %v", err)
	}

	if err := s.SlotLoad(context.Background(), 1, "axon/synapse/RRS18-1601/acp1"); err != nil {
		t.Fatalf("SlotLoad: %v", err)
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if readSlotStatus(t, s, 1) == 2 /* present */ {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("cascade did not reach present after SlotLoad: state = %d", readSlotStatus(t, s, 1))
}

func TestSlotUnload_DrivesExtract(t *testing.T) {
	s := newTestServer(t)
	// fixture starts slot 1 at present.
	s.SlotUnload(1)
	if got := readSlotStatus(t, s, 1); got != 0 {
		t.Fatalf("after SlotUnload: state = %d, want no_card (0)", got)
	}
}

func TestSlotLoad_PassesFingerprintToResolver(t *testing.T) {
	s := newTestServer(t)
	r := &fakeDMResolver{}
	s.SetDMLibrary(r)

	const path = "axon/synapse/RRS18-1601/acp1"
	if err := s.SlotLoad(context.Background(), 1, path); err != nil {
		t.Fatalf("SlotLoad: %v", err)
	}
	want := dmlib.Fingerprint{
		Vendor: "axon", Product: "synapse",
		Model: "RRS18", SwRev: "1601", Proto: "acp1",
	}
	if r.calledFP != want {
		t.Fatalf("resolver called with %+v, want %+v", r.calledFP, want)
	}
}

// schemaWithIdentity builds a minimal *dmlib.Schema whose only object
// is identity[0]=Card-Label. Just enough to verify that ReplaceSlot
// truly swaps the served identity.
func schemaWithIdentity(slot int, model, swRev string) *dmlib.Schema {
	return &dmlib.Schema{
		Fingerprint: dmlib.Fingerprint{Model: model, SwRev: swRev, Proto: "acp1"},
		Slots: map[int]*export.Snapshot{
			slot: {
				Slots: []export.SlotDump{
					{
						Slot: slot,
						Objects: []protocol.Object{
							{
								Slot:   slot,
								Group:  "identity",
								ID:     0,
								Label:  "Card name",
								Kind:   protocol.KindString,
								Access: 0x01, // read
								Value:  protocol.Value{Kind: protocol.KindString, Str: model},
							},
							{
								Slot:   slot,
								Group:  "identity",
								ID:     3,
								Label:  "Sw revision",
								Kind:   protocol.KindString,
								Access: 0x01,
								Value:  protocol.Value{Kind: protocol.KindString, Str: swRev},
							},
						},
					},
				},
			},
		},
	}
}

// readEntry helper: snapshot a single (slot, group, id) under read-lock.
func readEntry(t *testing.T, s *server, slot uint8, grp codec.ObjGroup, id uint8) *entry {
	t.Helper()
	s.tree.mu.RLock()
	defer s.tree.mu.RUnlock()
	return s.tree.entries[objectKey{slot: slot, group: grp, id: id}]
}

// TestSlotLoad_ReplacesIdentity is the central end-to-end check: after
// SlotLoad the served identity reflects the loaded card, not the
// tree.json starter.
func TestSlotLoad_ReplacesIdentity(t *testing.T) {
	s := newTestServer(t)
	s.SetInsertTiming(InsertTimingFast)

	r := &fakeDMResolver{
		schemas: map[string]*dmlib.Schema{
			"RRS18-1601": schemaWithIdentity(1, "RRS18", "1601"),
		},
	}
	s.SetDMLibrary(r)

	if err := s.SlotLoad(context.Background(), 1, "axon/synapse/RRS18-1601/acp1"); err != nil {
		t.Fatalf("SlotLoad: %v", err)
	}

	e := readEntry(t, s, 1, codec.GroupIdentity, 0)
	if e == nil {
		t.Fatal("identity[0] entry missing after SlotLoad")
	}
	if got, _ := e.param.Value.(string); got != "RRS18" {
		t.Fatalf("identity[0] value = %q, want RRS18", got)
	}
}

// TestSlotLoad_TwoLoadsSameSlot_SecondWins models a card-swap on the
// same slot: the second load must overwrite the first.
func TestSlotLoad_TwoLoadsSameSlot_SecondWins(t *testing.T) {
	s := newTestServer(t)
	s.SetInsertTiming(InsertTimingFast)

	r := &fakeDMResolver{
		schemas: map[string]*dmlib.Schema{
			"RRS18-1601":  schemaWithIdentity(1, "RRS18", "1601"),
			"2GS110-2728": schemaWithIdentity(1, "2GS110", "2728"),
		},
	}
	s.SetDMLibrary(r)

	ctx := context.Background()
	if err := s.SlotLoad(ctx, 1, "axon/synapse/RRS18-1601/acp1"); err != nil {
		t.Fatalf("first SlotLoad: %v", err)
	}
	if err := s.SlotLoad(ctx, 1, "axon/synapse/2GS110-2728/acp1"); err != nil {
		t.Fatalf("second SlotLoad: %v", err)
	}

	e := readEntry(t, s, 1, codec.GroupIdentity, 0)
	if e == nil {
		t.Fatal("identity[0] missing after second load")
	}
	if got, _ := e.param.Value.(string); got != "2GS110" {
		t.Fatalf("identity[0] = %q, want 2GS110 (second load should win)", got)
	}
	swrev := readEntry(t, s, 1, codec.GroupIdentity, 3)
	if swrev == nil {
		t.Fatal("identity[3] missing after second load")
	}
	if got, _ := swrev.param.Value.(string); got != "2728" {
		t.Fatalf("identity[3] = %q, want 2728", got)
	}
}

// TestSlotLoad_FromPresentState_Succeeds confirms that loading a card
// onto a slot already at present works without preconditions. Real
// frames let an operator hot-replace without first issuing an extract.
func TestSlotLoad_FromPresentState_Succeeds(t *testing.T) {
	s := newTestServer(t)
	s.SetInsertTiming(InsertTimingFast)
	// fixture starts slot 1 at present.
	if got := readSlotStatus(t, s, 1); got != 2 {
		t.Fatalf("setup: slot 1 status = %d, want present (2)", got)
	}
	r := &fakeDMResolver{
		schemas: map[string]*dmlib.Schema{
			"RRS18-1601": schemaWithIdentity(1, "RRS18", "1601"),
		},
	}
	s.SetDMLibrary(r)

	if err := s.SlotLoad(context.Background(), 1, "axon/synapse/RRS18-1601/acp1"); err != nil {
		t.Fatalf("SlotLoad on present slot: %v", err)
	}

	e := readEntry(t, s, 1, codec.GroupIdentity, 0)
	if e == nil {
		t.Fatal("identity[0] missing after load on present slot")
	}
}

// TestSlotUnload_FromAnyState_Idempotent calls unload twice in a row.
// The second call must succeed even though the slot is already empty.
func TestSlotUnload_FromAnyState_Idempotent(t *testing.T) {
	s := newTestServer(t)
	s.SetInsertTiming(InsertTimingFast)

	s.SlotUnload(1)
	if got := readSlotStatus(t, s, 1); got != 0 {
		t.Fatalf("after first unload: state = %d, want no_card", got)
	}
	// Second call on already-empty slot — must not panic or error.
	s.SlotUnload(1)
	if got := readSlotStatus(t, s, 1); got != 0 {
		t.Fatalf("after second unload: state = %d, want no_card", got)
	}
}

// TestSlotUnload_RemovesEntries verifies tree entries for the slot
// are gone after unload (so subsequent identity probes return
// "object group does not exist").
func TestSlotUnload_RemovesEntries(t *testing.T) {
	s := newTestServer(t)
	s.SetInsertTiming(InsertTimingFast)

	r := &fakeDMResolver{
		schemas: map[string]*dmlib.Schema{
			"RRS18-1601": schemaWithIdentity(1, "RRS18", "1601"),
		},
	}
	s.SetDMLibrary(r)

	if err := s.SlotLoad(context.Background(), 1, "axon/synapse/RRS18-1601/acp1"); err != nil {
		t.Fatalf("SlotLoad: %v", err)
	}
	if e := readEntry(t, s, 1, codec.GroupIdentity, 0); e == nil {
		t.Fatal("setup: identity[0] should exist after load")
	}

	s.SlotUnload(1)

	if e := readEntry(t, s, 1, codec.GroupIdentity, 0); e != nil {
		t.Fatal("identity[0] should be gone after unload")
	}
	if e := readEntry(t, s, 1, codec.GroupIdentity, 3); e != nil {
		t.Fatal("identity[3] should be gone after unload")
	}
	s.tree.mu.RLock()
	_, hasCounts := s.tree.slots[1]
	s.tree.mu.RUnlock()
	if hasCounts {
		t.Fatal("slotCounts[1] should be removed after unload")
	}
}

// TestTree_ReplaceSlot_RejectsSlotZero defends the rack-controller
// frame-status from a misrouted slot.load.
func TestTree_ReplaceSlot_RejectsSlotZero(t *testing.T) {
	s := newTestServer(t)
	snap := schemaWithIdentity(0, "BOGUS", "0").Slots[0]
	if err := s.tree.ReplaceSlot(0, snap); err == nil {
		t.Fatal("ReplaceSlot(0, ...) should reject slot 0")
	}
}

// TestTree_ClearSlot_RejectsSlotZero defends slot 0 from a wayward
// SlotUnload.
func TestTree_ClearSlot_RejectsSlotZero(t *testing.T) {
	s := newTestServer(t)
	if err := s.tree.ClearSlot(0); err == nil {
		t.Fatal("ClearSlot(0) should reject slot 0")
	}
}

// TestTree_ReplaceSlot_DropsExistingEntries confirms the contract:
// every prior entry for the slot is gone before new entries land.
func TestTree_ReplaceSlot_DropsExistingEntries(t *testing.T) {
	s := newTestServer(t)
	// fixture's slot 1 carries identity[0]=GIO-12 + control[0]=Level.
	if e := readEntry(t, s, 1, codec.GroupControl, 0); e == nil {
		t.Fatal("setup: control[0] (Level) should exist on fixture slot 1")
	}

	snap := schemaWithIdentity(1, "REPLACED", "9999").Slots[1]
	if err := s.tree.ReplaceSlot(1, snap); err != nil {
		t.Fatalf("ReplaceSlot: %v", err)
	}
	// New identity is in.
	if e := readEntry(t, s, 1, codec.GroupIdentity, 0); e == nil {
		t.Fatal("identity[0] should exist after replace")
	}
	// Old control entry from the fixture is gone.
	if e := readEntry(t, s, 1, codec.GroupControl, 0); e != nil {
		t.Fatal("control[0] from fixture should be wiped after replace")
	}
}
