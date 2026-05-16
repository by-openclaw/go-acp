package datastore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"dhs/internal/export"
	"dhs/internal/consumer"
)

// TestSaveLoadByIdentity_PreservesMetaAndContent pins the per-card DM
// contract (#430): writer round-trips Object fields (Path, Unit, Meta
// with nested maps) without dropping anything. Original 2026-05-09
// regression was that SaveByIdentity used to route through
// export.WriteJSON (hierarchical), and the matching reader's leaf
// decode struct in flattenJSONTree had no Meta field — load → save →
// load silently dropped Object.Meta. Direct json.Encoder on the DM
// struct (#430) preserves it.
func TestSaveLoadByIdentity_PreservesMetaAndContent(t *testing.T) {
	dir := t.TempDir()
	saver := NewTreeStore(dir)

	objs := []consumer.Object{
		{
			// Slot is set on purpose — writer must ZERO it (DM is
			// slot-agnostic). Round-trip should come back with Slot=0.
			Slot:  7,
			ID:    70232,
			Label: "Backup Input",
			Path:  []string{"BOARD", "Stream"},
			Kind:  consumer.KindEnum,
			Unit:  "dBFS",
			Meta: map[string]any{
				"acp2.objType": uint8(2),
				"acp2.numType": uint8(9),
				"acp2.optionsMap": map[string]string{
					"786": "Automatic",
					"791": "Full Speed",
				},
			},
		},
	}

	if err := saver.SaveByIdentity("acp2", "SHPRM1@0.7", objs); err != nil {
		t.Fatalf("SaveByIdentity: %v", err)
	}

	loader := NewTreeStore(dir)
	got, err := loader.LoadByIdentity("acp2", "SHPRM1@0.7")
	if err != nil || got == nil {
		t.Fatalf("LoadByIdentity: snap=%v err=%v", got, err)
	}

	if len(got.Slots) != 1 || len(got.Slots[0].Objects) != 1 {
		t.Fatalf("snap shape: slots=%d, want 1", len(got.Slots))
	}
	rt := got.Slots[0].Objects[0]
	if rt.Slot != 0 {
		t.Errorf("DM is slot-agnostic — writer must zero Slot, got Slot=%d", rt.Slot)
	}
	if rt.ID != 70232 || rt.Label != "Backup Input" || rt.Kind != consumer.KindEnum {
		t.Errorf("base fields lost: id=%d label=%q kind=%v", rt.ID, rt.Label, rt.Kind)
	}
	if rt.Unit != "dBFS" {
		t.Errorf("Unit lost on round-trip: got %q want %q", rt.Unit, "dBFS")
	}
	if len(rt.Path) != 2 || rt.Path[0] != "BOARD" || rt.Path[1] != "Stream" {
		t.Errorf("Path lost: %v", rt.Path)
	}
	if rt.Meta == nil {
		t.Fatalf("Meta dropped on round-trip — DM cache regression")
	}
	if v, ok := rt.Meta["acp2.objType"]; !ok || v == nil {
		t.Errorf("Meta[acp2.objType] missing: meta=%v", rt.Meta)
	}
	if om, ok := rt.Meta["acp2.optionsMap"]; !ok {
		t.Errorf("Meta[acp2.optionsMap] missing")
	} else if m, _ := om.(map[string]any); m == nil || m["786"] != "Automatic" {
		t.Errorf("optionsMap content lost: %v", om)
	}
}

// TestSaveByIdentity_EmitsCleanDMShape asserts the on-disk JSON has
// NONE of the pre-#430 envelope garbage: no `device`, no `slots`, no
// `created_at`, no `generator`, no host instance fields. Only the
// per-card DM contract: model, sw_rev, protocol, objects.
func TestSaveByIdentity_EmitsCleanDMShape(t *testing.T) {
	dir := t.TempDir()
	store := NewTreeStore(dir)

	objs := []consumer.Object{{
		Slot: 0, ID: 0, Group: "identity", Label: "Card name",
		Kind: consumer.KindString,
	}}
	if err := store.SaveByIdentity("acp1", "RRS18@1601", objs); err != nil {
		t.Fatalf("SaveByIdentity: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "dm", "acp1", "RRS18@1601.json"))
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		t.Fatalf("decode: %v", err)
	}

	wantKeys := map[string]bool{"model": true, "sw_rev": true, "protocol": true, "objects": true}
	for k := range probe {
		if !wantKeys[k] {
			t.Errorf("DM has forbidden top-level key %q — only {model, sw_rev, protocol, objects} allowed (#430). Raw: %s", k, raw)
		}
	}
	for want := range wantKeys {
		if _, ok := probe[want]; !ok {
			t.Errorf("DM missing required top-level key %q", want)
		}
	}

	var dm DM
	if err := json.Unmarshal(raw, &dm); err != nil {
		t.Fatalf("DM decode: %v", err)
	}
	if dm.Model != "RRS18" {
		t.Errorf("model: got %q, want %q", dm.Model, "RRS18")
	}
	if dm.SwRev != "1601" {
		t.Errorf("sw_rev: got %q, want %q", dm.SwRev, "1601")
	}
	if dm.Protocol != "acp1" {
		t.Errorf("protocol: got %q, want %q", dm.Protocol, "acp1")
	}
}

// TestIdentityPath_PerProtocolSubfolder pins the #424 path layout
// (per-proto subfolder) still works under the #430 DM-shape change.
func TestIdentityPath_PerProtocolSubfolder(t *testing.T) {
	dir := t.TempDir()
	store := NewTreeStore(dir)
	objs := []consumer.Object{{Slot: 0, ID: 0, Label: "Card name"}}
	if err := store.SaveByIdentity("acp1", "RRS18@1601", objs); err != nil {
		t.Fatalf("SaveByIdentity: %v", err)
	}
	wantPath := filepath.Join(dir, "dm", "acp1", "RRS18@1601.json")
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("expected file at new layout %s: %v", wantPath, err)
	}
	legacyPath := filepath.Join(dir, "dm", "RRS18@1601.json")
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Errorf("legacy path MUST NOT be written: %s err=%v", legacyPath, err)
	}
}

// TestLoadByIdentity_FallbackToLegacyPath verifies pre-#424 path layout
// (flat .cache/dm/<id>.json) still imports.
func TestLoadByIdentity_FallbackToLegacyPath(t *testing.T) {
	dir := t.TempDir()
	legacyDir := filepath.Join(dir, "dm")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	legacyFile := filepath.Join(legacyDir, "CONVERT Hybrid@6.7.4.json")
	// Hand-write a legacy Snapshot-shaped file (pre-#430).
	snap := &export.Snapshot{
		Device: export.DeviceInfo{IP: "10.41.40.195", Protocol: "acp2"},
		Slots: []export.SlotDump{{
			Slot:    1,
			Objects: []consumer.Object{{Slot: 1, ID: 67604, Label: "Destination IP"}},
		}},
	}
	f, _ := os.Create(legacyFile)
	if err := json.NewEncoder(f).Encode(snap); err != nil {
		t.Fatalf("write legacy: %v", err)
	}
	_ = f.Close()

	store := NewTreeStore(dir)
	got, err := store.LoadByIdentity("acp2", "CONVERT Hybrid@6.7.4")
	if err != nil {
		t.Fatalf("LoadByIdentity: %v", err)
	}
	if got == nil {
		t.Fatalf("legacy fallback missed file: %s", legacyFile)
	}
	if len(got.Slots) != 1 || got.Slots[0].Objects[0].Label != "Destination IP" {
		t.Errorf("legacy fallback decoded wrong content: %+v", got.Slots)
	}
}

// TestLoadByIdentity_NewDMShape verifies the reader synthesises a
// Snapshot from a #430-shaped file.
func TestLoadByIdentity_NewDMShape(t *testing.T) {
	dir := t.TempDir()
	store := NewTreeStore(dir)
	objs := []consumer.Object{
		{Slot: 0, ID: 0, Label: "Card name", Kind: consumer.KindString},
		{Slot: 0, ID: 1, Label: "User label", Kind: consumer.KindString},
	}
	if err := store.SaveByIdentity("acp2", "SHPRM1@0.7", objs); err != nil {
		t.Fatalf("SaveByIdentity: %v", err)
	}
	got, err := store.LoadByIdentity("acp2", "SHPRM1@0.7")
	if err != nil || got == nil {
		t.Fatalf("LoadByIdentity: got=%v err=%v", got, err)
	}
	if got.Device.Protocol != "acp2" {
		t.Errorf("synthesised Snapshot protocol: got %q, want %q", got.Device.Protocol, "acp2")
	}
	if got.Device.IP != "" {
		t.Errorf("synthesised Snapshot must have empty IP (slot-agnostic), got %q", got.Device.IP)
	}
	if len(got.Slots) != 1 || len(got.Slots[0].Objects) != 2 {
		t.Fatalf("synthesised shape: slots=%d objs=%d, want 1/2",
			len(got.Slots),
			func() int {
				if len(got.Slots) == 0 {
					return -1
				}
				return len(got.Slots[0].Objects)
			}())
	}
}
