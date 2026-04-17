package storage

import (
	"os"
	"path/filepath"
	"testing"

	"acp/internal/protocol"
)

func TestTreeStore_SaveLoad(t *testing.T) {
	dir := t.TempDir()
	store := NewTreeStore(dir)

	objs := []protocol.Object{
		{
			Slot: 0, Group: "identity", Path: []string{"identity"},
			ID: 0, Label: "Card name", Kind: protocol.KindString,
			Access: 1, MaxLen: 8,
			Value: protocol.Value{Kind: protocol.KindString, Str: "RRS18"},
		},
		{
			Slot: 0, Group: "control", Path: []string{"control"},
			ID: 4, Label: "Broadcasts", Kind: protocol.KindEnum,
			Access: 3, EnumItems: []string{"Off", "On"},
			Value: protocol.Value{Kind: protocol.KindEnum, Enum: 1, Str: "On"},
		},
	}

	// Save.
	if err := store.Save("10.6.239.113", "acp1", 0, objs); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// File exists.
	path := filepath.Join(dir, "devices", "10.6.239.113", "slot_0.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not created: %v", err)
	}

	// Load.
	snap, err := store.Load("10.6.239.113", 0)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if snap == nil {
		t.Fatal("Load returned nil")
	}
	if len(snap.Slots) != 1 {
		t.Fatalf("slots: got %d, want 1", len(snap.Slots))
	}

	// Verify objects loaded (values should be stripped).
	loaded := snap.Slots[0].Objects
	if len(loaded) != 2 {
		t.Fatalf("objects: got %d, want 2", len(loaded))
	}

	// Find Card name by label (map ordering may differ).
	var found bool
	for _, o := range loaded {
		if o.Label == "Card name" {
			found = true
			if o.ID != 0 {
				t.Errorf("Card name ID: got %d, want 0", o.ID)
			}
			// Value should be zero (stripped on save).
			if o.Value.Kind != protocol.KindUnknown && o.Value.Str != "" {
				t.Errorf("Value should be stripped, got kind=%v str=%q", o.Value.Kind, o.Value.Str)
			}
		}
	}
	if !found {
		t.Error("Card name not found in loaded objects")
	}
}

func TestTreeStore_LoadMissing(t *testing.T) {
	dir := t.TempDir()
	store := NewTreeStore(dir)

	snap, err := store.Load("10.0.0.1", 0)
	if err != nil {
		t.Fatalf("Load missing: %v", err)
	}
	if snap != nil {
		t.Error("expected nil for missing file")
	}
}

func TestTreeStore_Delete(t *testing.T) {
	dir := t.TempDir()
	store := NewTreeStore(dir)

	objs := []protocol.Object{
		{Slot: 0, ID: 1, Label: "Test", Kind: protocol.KindString},
	}
	if err := store.Save("10.0.0.1", "acp1", 0, objs); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if err := store.Delete("10.0.0.1", 0); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	snap, err := store.Load("10.0.0.1", 0)
	if err != nil {
		t.Fatalf("Load after delete: %v", err)
	}
	if snap != nil {
		t.Error("expected nil after delete")
	}
}

func TestFindCardName(t *testing.T) {
	objs := []protocol.Object{
		{Label: "Serial Number", Value: protocol.Value{Kind: protocol.KindString, Str: "001633"}},
		{Label: "Card Name", Value: protocol.Value{Kind: protocol.KindString, Str: "SHPRM1"}},
	}
	if got := FindCardName(objs); got != "SHPRM1" {
		t.Errorf("FindCardName: got %q, want SHPRM1", got)
	}
}

func TestValidate(t *testing.T) {
	if Validate(nil, "SHPRM1") {
		t.Error("nil snapshot should not validate")
	}
}
