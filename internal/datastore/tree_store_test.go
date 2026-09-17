package datastore

import (
	"os"
	"path/filepath"
	"testing"

	"dhs/internal/consumer"
)

func TestTreeStore_SaveLoad(t *testing.T) {
	dir := t.TempDir()
	store := NewTreeStore(dir)

	objs := []consumer.Object{
		{
			Slot: 0, Group: "identity", Path: []string{"identity"},
			ID: 0, Label: "Card name", Kind: consumer.KindString,
			Access: 1, MaxLen: 8,
			Value: consumer.Value{Kind: consumer.KindString, Str: "RRS18"},
		},
		{
			Slot: 0, Group: "control", Path: []string{"control"},
			ID: 4, Label: "Broadcasts", Kind: consumer.KindEnum,
			Access: 3, EnumItems: []string{"Off", "On"},
			Value: consumer.Value{Kind: consumer.KindEnum, Enum: 1, Str: "On"},
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
			if o.Value.Kind != consumer.KindUnknown && o.Value.Str != "" {
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

	objs := []consumer.Object{
		{Slot: 0, ID: 1, Label: "Test", Kind: consumer.KindString},
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
	objs := []consumer.Object{
		{Label: "Serial Number", Value: consumer.Value{Kind: consumer.KindString, Str: "001633"}},
		{Label: "Card Name", Value: consumer.Value{Kind: consumer.KindString, Str: "SHPRM1"}},
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

// TestTreeStore_IdentityPath pins the exported DM-path accessor used
// by the extract verbs for evidence lines (print + fingerprint): it
// must match where WriteDM actually lands the file, and be nil-safe.
func TestTreeStore_IdentityPath(t *testing.T) {
	s := NewTreeStore(t.TempDir())
	if err := s.WriteDM("cerebrum-nb", "SHPRM1@5.3.5", DM{Protocol: "cerebrum-nb"}); err != nil {
		t.Fatalf("WriteDM: %v", err)
	}
	p := s.IdentityPath("cerebrum-nb", "SHPRM1@5.3.5")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("IdentityPath %q does not match WriteDM output: %v", p, err)
	}
	var nilStore *TreeStore
	if got := nilStore.IdentityPath("p", "i"); got != "" {
		t.Fatalf("nil store path = %q, want empty", got)
	}
}
