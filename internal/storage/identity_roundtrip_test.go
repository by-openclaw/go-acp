package storage

import (
	"testing"
	"time"

	"dhs/internal/export"
	"dhs/internal/protocol"
)

// TestSaveLoadByIdentity_PreservesMetaAndContent pins the regression
// surfaced live on 2026-05-09: SaveByIdentity used to route through
// export.WriteJSON (hierarchical), and the matching reader's leaf
// decode struct in flattenJSONTree had no Meta field. So the disk
// round-trip silently dropped Object.Meta — the exact map the ACP2
// SeedTreeFromCachedObjects path needs (acp2.objType / numType /
// optionsMap) to rebuild ObjTypes/NumTypes parallel arrays. Symptom:
// watcher decoded enum labels and units but rendered numeric values
// as `raw(N)` because seed couldn't recover the type info.
//
// Fix: SaveByIdentity uses stdlib json.Encoder direct on *Snapshot;
// LoadByIdentity uses stdlib json.Decoder direct into *Snapshot. The
// snapshot is byte-faithful across separate dhs invocations.
//
// The test uses a NEW TreeStore for the load step so we exercise the
// real cross-process path (open file, decode flat) — same shape the
// user hits when running `walk --slot 0` then `walk --slot 1` in two
// shell invocations.
func TestSaveLoadByIdentity_PreservesMetaAndContent(t *testing.T) {
	dir := t.TempDir()
	saver := NewTreeStore(dir)

	objs := []protocol.Object{
		{
			Slot:  1,
			ID:    70232,
			Label: "Backup Input",
			Path:  []string{"BOARD", "Stream"},
			Kind:  protocol.KindEnum,
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
	snap := &export.Snapshot{
		Device:    export.DeviceInfo{IP: "10.41.40.4", Protocol: "acp2"},
		Generator: "test",
		CreatedAt: time.Now().UTC(),
		Slots: []export.SlotDump{{
			Slot:     1,
			WalkedAt: time.Now().UTC(),
			Objects:  objs,
		}},
	}

	if err := saver.SaveByIdentity("SHPRM1@0.7", snap); err != nil {
		t.Fatalf("SaveByIdentity: %v", err)
	}

	loader := NewTreeStore(dir)
	got, err := loader.LoadByIdentity("SHPRM1@0.7")
	if err != nil || got == nil {
		t.Fatalf("LoadByIdentity: snap=%v err=%v", got, err)
	}

	if len(got.Slots) != 1 || len(got.Slots[0].Objects) != 1 {
		t.Fatalf("snap shape: slots=%d objs=%d, want 1/1",
			len(got.Slots),
			func() int {
				if len(got.Slots) == 0 {
					return -1
				}
				return len(got.Slots[0].Objects)
			}())
	}
	rt := got.Slots[0].Objects[0]
	if rt.ID != 70232 || rt.Label != "Backup Input" || rt.Kind != protocol.KindEnum {
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

// TestSaveLoadByIdentity_MultiInvocationMerge reproduces the exact
// user flow: walk slot 0 then walk slot 1 in two separate dhs
// invocations. Each invocation re-creates the TreeStore. After both,
// the file must contain BOTH slots with their original object content
// — not slot 1 alone (the bug) and not slot 0 alone.
func TestSaveLoadByIdentity_MultiInvocationMerge(t *testing.T) {
	dir := t.TempDir()
	identity := "SHPRM1@0.7"

	{
		store := NewTreeStore(dir)
		existing, _ := store.LoadByIdentity(identity)
		if existing != nil {
			t.Fatalf("invocation 1 expected miss, got %v", existing)
		}
		snap := &export.Snapshot{
			Device:    export.DeviceInfo{IP: "10.41.40.4", Protocol: "acp2"},
			Generator: "test",
			CreatedAt: time.Now().UTC(),
			Slots: []export.SlotDump{{
				Slot:     0,
				WalkedAt: time.Now().UTC(),
				Objects: []protocol.Object{{
					Slot: 0, ID: 1, Label: "BOARD",
					Path: []string{"BOARD"},
					Kind: protocol.KindString,
					Meta: map[string]any{"acp2.objType": uint8(0)},
				}},
			}},
		}
		if err := store.SaveByIdentity(identity, snap); err != nil {
			t.Fatalf("invocation 1 save: %v", err)
		}
	}

	{
		store := NewTreeStore(dir)
		existing, err := store.LoadByIdentity(identity)
		if err != nil || existing == nil {
			t.Fatalf("invocation 2 expected hit, got snap=%v err=%v", existing, err)
		}
		if len(existing.Slots) != 1 || existing.Slots[0].Slot != 0 {
			t.Fatalf("invocation 2 sees wrong slots: %+v", existing.Slots)
		}
		if len(existing.Slots[0].Objects) != 1 || existing.Slots[0].Objects[0].Label != "BOARD" {
			t.Fatalf("invocation 2 lost slot 0 content: %+v", existing.Slots[0].Objects)
		}
		existing.Slots = append(existing.Slots, export.SlotDump{
			Slot:     1,
			WalkedAt: time.Now().UTC(),
			Objects: []protocol.Object{{
				Slot: 1, ID: 70232, Label: "Backup Input",
				Path: []string{"BOARD", "Stream"},
				Kind: protocol.KindEnum,
				Unit: "dBFS",
				Meta: map[string]any{"acp2.objType": uint8(2)},
			}},
		})
		if err := store.SaveByIdentity(identity, existing); err != nil {
			t.Fatalf("invocation 2 save: %v", err)
		}
	}

	store := NewTreeStore(dir)
	final, err := store.LoadByIdentity(identity)
	if err != nil || final == nil {
		t.Fatalf("final load: %v", err)
	}
	if len(final.Slots) != 2 {
		t.Fatalf("final file should hold 2 slots, got %d (slot 1 overwrote slot 0?)", len(final.Slots))
	}
	gotSlots := map[int]string{}
	gotUnits := map[int]string{}
	for _, sd := range final.Slots {
		if len(sd.Objects) == 0 {
			t.Errorf("slot %d has zero objects after merge", sd.Slot)
			continue
		}
		gotSlots[sd.Slot] = sd.Objects[0].Label
		gotUnits[sd.Slot] = sd.Objects[0].Unit
	}
	if gotSlots[0] != "BOARD" {
		t.Errorf("slot 0 lost original content: got %q want %q", gotSlots[0], "BOARD")
	}
	if gotSlots[1] != "Backup Input" {
		t.Errorf("slot 1 missing: got %q want %q", gotSlots[1], "Backup Input")
	}
	if gotUnits[1] != "dBFS" {
		t.Errorf("slot 1 Unit lost on merge: got %q want %q", gotUnits[1], "dBFS")
	}
}
