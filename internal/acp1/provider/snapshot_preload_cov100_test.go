package acp1

import (
	"context"
	"testing"

	"dhs/internal/acp1/codec"
	"dhs/internal/consumer"
	"dhs/internal/devicemodel"
	"dhs/internal/export"
)

// TestSnapshotToEntries_FileEmptyLabelAndUnknownKind covers the file-group
// count arm, the empty-label "#id" identifier fallback, and the default
// acpTypeFromObject (unknown kind → TypeInteger).
func TestSnapshotToEntries_FileEmptyLabelAndUnknownKind(t *testing.T) {
	snap := &export.Snapshot{Slots: []export.SlotDump{{
		Slot: 1,
		Objects: []consumer.Object{
			// File-group object → counts.numFile arm.
			{Slot: 1, Group: "file", ID: 0, Label: "FW", Kind: consumer.KindString, Access: 0x01},
			// Empty-label control object → "#id" identifier.
			{Slot: 1, Group: "control", ID: 2, Label: "", Kind: consumer.KindInt, Access: 0x03},
			// Unknown kind → acpTypeFromObject default (TypeInteger).
			{Slot: 1, Group: "status", ID: 0, Label: "X", Kind: consumer.ValueKind(99), Access: 0x01},
		},
	}}}
	entries, counts, err := snapshotToEntries(1, snap)
	if err != nil {
		t.Fatalf("snapshotToEntries: %v", err)
	}
	if counts.numFile == 0 {
		t.Error("expected numFile counted")
	}
	if len(entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(entries))
	}
}

// TestSnapshotToEntries_NoMatchingSlot covers findSlotDump returning nil
// (multiple dumps, none matching) → snapshotToEntries error.
func TestSnapshotToEntries_NoMatchingSlot(t *testing.T) {
	snap := &export.Snapshot{Slots: []export.SlotDump{{Slot: 5}, {Slot: 6}}}
	if _, _, err := snapshotToEntries(1, snap); err == nil {
		t.Fatal("no matching slot: want error")
	}
}

// TestFindSlotDump_SingleEntryFallback covers the len==1 fallback arm.
func TestFindSlotDump_SingleEntryFallback(t *testing.T) {
	snap := &export.Snapshot{Slots: []export.SlotDump{{Slot: 99}}}
	if sd := findSlotDump(snap, 1); sd == nil {
		t.Fatal("single-entry fallback should match")
	}
}

// TestFormatHintFor_FileAndFrame covers the file and frame hint arms.
func TestFormatHintFor_FileAndFrame(t *testing.T) {
	if h := formatHintFor(consumer.Object{}, codec.TypeFile); h != "file" {
		t.Errorf("file hint = %q", h)
	}
	if h := formatHintFor(consumer.Object{}, codec.TypeFrame); h != "frame" {
		t.Errorf("frame hint = %q", h)
	}
}

// TestPreloadSlot_ErrorPaths covers PreloadSlot's no-resolver, bad-path,
// resolve-error, no-slot-data, and ReplaceSlot-error arms.
func TestPreloadSlot_ErrorPaths(t *testing.T) {
	// No resolver.
	s0 := newTestServer(t)
	if err := s0.PreloadSlot(1, "axon/synapse/RRS18-1601/acp1"); err == nil {
		t.Error("no resolver: want error")
	}

	// Bad card path.
	s1 := newTestServer(t)
	s1.SetDMLibrary(&fakeDMResolver{})
	if err := s1.PreloadSlot(1, "bogus"); err == nil {
		t.Error("bad path: want error")
	}

	// Resolve error.
	s2 := newTestServer(t)
	s2.SetDMLibrary(&fakeDMResolver{resolveErr: devicemodel.ErrNotFound})
	if err := s2.PreloadSlot(1, "axon/synapse/RRS18-1601/acp1"); err == nil {
		t.Error("resolve error: want error")
	}

	// No slot data: schema with slots but none for the target and >1 entry.
	s3 := newTestServer(t)
	s3.SetDMLibrary(&fakeDMResolver{schemas: map[string]*devicemodel.Schema{
		"RRS18-1601": {Slots: map[int]*export.Snapshot{
			7: {Slots: []export.SlotDump{{Slot: 7}}},
			8: {Slots: []export.SlotDump{{Slot: 8}}},
		}},
	}})
	if err := s3.PreloadSlot(1, "axon/synapse/RRS18-1601/acp1"); err == nil {
		t.Error("no slot data: want error")
	}

	// ReplaceSlot error: pickSnapshot returns a snapshot (keyed at slot 1)
	// whose SlotDumps don't include slot 1 and number >1 → findSlotDump nil.
	s4 := newTestServer(t)
	s4.SetDMLibrary(&fakeDMResolver{schemas: map[string]*devicemodel.Schema{
		"RRS18-1601": {Slots: map[int]*export.Snapshot{
			1: {Slots: []export.SlotDump{{Slot: 5}, {Slot: 6}}},
		}},
	}})
	if err := s4.PreloadSlot(1, "axon/synapse/RRS18-1601/acp1"); err == nil {
		t.Error("ReplaceSlot error: want error")
	}
}

// TestSlotLoad_NoSlotDataAndReplaceError covers SlotLoad's no-slot-data and
// ReplaceSlot-error arms (the resolver/path arms are covered elsewhere).
func TestSlotLoad_NoSlotDataAndReplaceError(t *testing.T) {
	s := newTestServer(t)
	s.SetDMLibrary(&fakeDMResolver{schemas: map[string]*devicemodel.Schema{
		"RRS18-1601": {Slots: map[int]*export.Snapshot{
			7: {Slots: []export.SlotDump{{Slot: 7}}},
			8: {Slots: []export.SlotDump{{Slot: 8}}},
		}},
	}})
	if err := s.SlotLoad(context.Background(), 1, "axon/synapse/RRS18-1601/acp1"); err == nil {
		t.Error("no slot data: want error")
	}

	s2 := newTestServer(t)
	s2.SetDMLibrary(&fakeDMResolver{schemas: map[string]*devicemodel.Schema{
		"RRS18-1601": {Slots: map[int]*export.Snapshot{
			1: {Slots: []export.SlotDump{{Slot: 5}, {Slot: 6}}},
		}},
	}})
	if err := s2.SlotLoad(context.Background(), 1, "axon/synapse/RRS18-1601/acp1"); err == nil {
		t.Error("ReplaceSlot error: want error")
	}
}
