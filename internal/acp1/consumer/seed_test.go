package acp1

import (
	"errors"
	"log/slog"
	"testing"
	"time"

	"dhs/internal/acp1/codec"
	"dhs/internal/export"
	"dhs/internal/protocol"
)

func makeSeedSnapshot(slot int, objs []protocol.Object) *export.Snapshot {
	return &export.Snapshot{
		Device: export.DeviceInfo{IP: "10.6.239.113", Protocol: "acp1"},
		Slots: []export.SlotDump{{
			Slot:     slot,
			Objects:  objs,
			WalkedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		}},
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

func TestSeedFromDM_PopulatesTreeAndLabels(t *testing.T) {
	p := &Plugin{
		logger: slog.Default(),
		trees:  newSlotTreeCache(8, time.Hour),
	}

	snap := makeSeedSnapshot(1, []protocol.Object{
		{Slot: 1, Group: "identity", ID: 0, Label: "Card Name", Kind: protocol.KindString},
		{Slot: 1, Group: "control", ID: 5, Label: "Gain", Kind: protocol.KindFloat, Unit: "dB"},
		{Slot: 1, Group: "status", ID: 0, Label: "Pct", Kind: protocol.KindUint, Unit: "%"},
	})

	if err := p.SeedFromDM(1, snap); err != nil {
		t.Fatalf("SeedFromDM: %v", err)
	}

	tree, ok := p.trees.Get(1)
	if !ok {
		t.Fatal("slot 1 missing after seed")
	}
	if len(tree.Objects) != 3 {
		t.Fatalf("Objects = %d, want 3", len(tree.Objects))
	}
	if len(tree.ACPTypes) != 3 {
		t.Fatalf("ACPTypes = %d, want 3", len(tree.ACPTypes))
	}
	if tree.ACPTypes[0] != codec.TypeString {
		t.Fatalf("identity ACPType = %d, want String(5)", tree.ACPTypes[0])
	}
	if tree.ACPTypes[1] != codec.TypeFloat {
		t.Fatalf("control ACPType = %d, want Float(3)", tree.ACPTypes[1])
	}
	if tree.ACPTypes[2] != codec.TypeByte {
		t.Fatalf("status ACPType = %d, want Byte(10)", tree.ACPTypes[2])
	}
	if idx := tree.Lookup("control", "Gain"); idx != 1 {
		t.Fatalf("Lookup control/Gain = %d, want 1", idx)
	}
	if idx := tree.Lookup("status", "Pct"); idx != 2 {
		t.Fatalf("Lookup status/Pct = %d, want 2", idx)
	}
}

func TestSeedFromDM_StripsValues(t *testing.T) {
	// Schemas are value-less: SeedFromDM must drop any values the snapshot
	// carries (e.g. accidentally persisted from an export run).
	p := &Plugin{
		logger: slog.Default(),
		trees:  newSlotTreeCache(8, time.Hour),
	}

	snap := makeSeedSnapshot(1, []protocol.Object{
		{
			Slot: 1, Group: "control", ID: 5, Label: "Gain",
			Kind:  protocol.KindFloat,
			Value: protocol.Value{Kind: protocol.KindFloat, Float: -6.0},
		},
	})
	if err := p.SeedFromDM(1, snap); err != nil {
		t.Fatalf("SeedFromDM: %v", err)
	}
	tree, _ := p.trees.Get(1)
	if !tree.Objects[0].Value.IsZero() {
		t.Fatalf("Value not stripped: %+v", tree.Objects[0].Value)
	}
}

func TestSeedFromDM_MetaACPTypeOverridesKindMapping(t *testing.T) {
	// Walks may persist Meta["acp1_type"] to disambiguate Integer vs
	// Long (both widen to KindInt). SeedFromDM must honour the meta
	// hint when present.
	p := &Plugin{
		logger: slog.Default(),
		trees:  newSlotTreeCache(8, time.Hour),
	}

	snap := makeSeedSnapshot(1, []protocol.Object{
		{Slot: 1, Group: "control", ID: 5, Label: "Counter",
			Kind: protocol.KindInt,
			Meta: map[string]any{"acp1_type": float64(codec.TypeLong)},
		},
	})
	if err := p.SeedFromDM(1, snap); err != nil {
		t.Fatalf("SeedFromDM: %v", err)
	}
	tree, _ := p.trees.Get(1)
	if tree.ACPTypes[0] != codec.TypeLong {
		t.Fatalf("ACPType = %d, want Long(9)", tree.ACPTypes[0])
	}
}

func TestSeedFromDM_NotConnected(t *testing.T) {
	p := &Plugin{logger: slog.Default()} // p.trees is nil
	snap := makeSeedSnapshot(1, nil)
	err := p.SeedFromDM(1, snap)
	if !errors.Is(err, protocol.ErrNotConnected) {
		t.Fatalf("err = %v, want ErrNotConnected", err)
	}
}

func TestSeedFromDM_NilSnapshot(t *testing.T) {
	p := &Plugin{
		logger: slog.Default(),
		trees:  newSlotTreeCache(8, time.Hour),
	}
	if err := p.SeedFromDM(1, nil); err == nil {
		t.Fatal("expected error for nil snapshot")
	}
}

func TestSeedFromDM_SlotMismatch_FallbackToSingleEntry(t *testing.T) {
	// Single-slot snapshot with Slot=0 (legacy export) should still
	// seed when the caller asks for slot 1.
	p := &Plugin{
		logger: slog.Default(),
		trees:  newSlotTreeCache(8, time.Hour),
	}
	snap := makeSeedSnapshot(0, []protocol.Object{
		{Slot: 0, Group: "control", ID: 5, Label: "Gain", Kind: protocol.KindFloat},
	})
	if err := p.SeedFromDM(1, snap); err != nil {
		t.Fatalf("SeedFromDM: %v", err)
	}
	tree, ok := p.trees.Get(1)
	if !ok || len(tree.Objects) != 1 {
		t.Fatalf("seed via single-entry fallback failed: tree=%+v", tree)
	}
}

func TestSeedFromDM_MissingSlot_MultiSlotSnapshot(t *testing.T) {
	// Multi-slot snapshot, requested slot absent: error, not silent.
	p := &Plugin{
		logger: slog.Default(),
		trees:  newSlotTreeCache(8, time.Hour),
	}
	snap := &export.Snapshot{
		Device: export.DeviceInfo{IP: "x", Protocol: "acp1"},
		Slots: []export.SlotDump{
			{Slot: 1, Objects: []protocol.Object{{Slot: 1, ID: 1}}},
			{Slot: 2, Objects: []protocol.Object{{Slot: 2, ID: 1}}},
		},
		CreatedAt: time.Now(),
	}
	if err := p.SeedFromDM(99, snap); err == nil {
		t.Fatal("expected error for missing slot")
	}
}
