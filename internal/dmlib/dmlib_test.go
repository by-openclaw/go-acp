package dmlib

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"dhs/internal/export"
	"dhs/internal/protocol"
)

// fixture builds a tiny on-disk DM library tree under t.TempDir() with two
// products, one of which has two sw_revs across two protocols.
func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	rrs1601 := makeSnapshot("RRS18", []protocol.Object{
		{Slot: 1, Group: "control", ID: 0, Label: "Card Name", Kind: protocol.KindString},
		{Slot: 1, Group: "control", ID: 5, Label: "Gain", Kind: protocol.KindInt, Min: int64(-60), Max: int64(12)},
	})
	rrs1602 := makeSnapshot("RRS18", []protocol.Object{
		{Slot: 1, Group: "control", ID: 0, Label: "Card Name", Kind: protocol.KindString},
		{Slot: 1, Group: "control", ID: 5, Label: "Gain", Kind: protocol.KindInt, Min: int64(-90), Max: int64(12)},
		{Slot: 1, Group: "control", ID: 8, Label: "Mute", Kind: protocol.KindBool},
	})
	rrs1601acp2 := makeSnapshot("RRS18", []protocol.Object{
		{Slot: 1, ID: 1234, Label: "Card Name", Kind: protocol.KindString},
	})

	writeAt(t, root, "axon", "synapse", "RRS18-1601", "acp1", 1, rrs1601)
	writeAt(t, root, "axon", "synapse", "RRS18-1602", "acp1", 1, rrs1602)
	writeAt(t, root, "axon", "synapse", "RRS18-1601", "acp2", 1, rrs1601acp2)

	return root
}

func writeAt(t *testing.T, root, vendor, product, modelRev, proto string, slot int, snap *export.Snapshot) {
	t.Helper()
	dir := filepath.Join(root, vendor, product, modelRev, proto)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	path := filepath.Join(dir, "slot_1.json")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	if err := export.WriteJSON(f, snap); err != nil {
		_ = f.Close()
		t.Fatalf("write %s: %v", path, err)
	}
	_ = f.Close()
	_ = slot
}

func makeSnapshot(cardName string, objs []protocol.Object) *export.Snapshot {
	return &export.Snapshot{
		Device: export.DeviceInfo{
			IP:       "10.6.239.113",
			Port:     2071,
			Protocol: "acp1",
		},
		Generator: "test",
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Slots: []export.SlotDump{{
			Slot:     1,
			Objects:  objs,
			WalkedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		}},
	}
}

func TestResolve_Hit(t *testing.T) {
	r := New(fixture(t))
	s, err := r.Resolve(Fingerprint{
		Vendor: "axon", Product: "synapse",
		Model: "RRS18", SwRev: "1601", Proto: "acp1",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(s.Slots) != 1 {
		t.Fatalf("slots = %d, want 1", len(s.Slots))
	}
	snap, ok := s.Slots[1]
	if !ok {
		t.Fatal("slot 1 missing")
	}
	if len(snap.Slots[0].Objects) != 2 {
		t.Fatalf("object count = %d, want 2", len(snap.Slots[0].Objects))
	}
}

func TestResolve_MissModel(t *testing.T) {
	r := New(fixture(t))
	_, err := r.Resolve(Fingerprint{
		Vendor: "axon", Product: "synapse",
		Model: "DOES_NOT_EXIST", SwRev: "1601", Proto: "acp1",
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestResolve_MissSwRev(t *testing.T) {
	r := New(fixture(t))
	_, err := r.Resolve(Fingerprint{
		Vendor: "axon", Product: "synapse",
		Model: "RRS18", SwRev: "9999", Proto: "acp1",
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestResolve_MissProto(t *testing.T) {
	r := New(fixture(t))
	_, err := r.Resolve(Fingerprint{
		Vendor: "axon", Product: "synapse",
		Model: "RRS18", SwRev: "1601", Proto: "emberplus",
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestResolve_InvalidFingerprint(t *testing.T) {
	r := New(t.TempDir())
	cases := []Fingerprint{
		{}, // all empty
		{Model: "RRS18"},
		{Model: "RRS18", SwRev: "1601"}, // missing Proto
	}
	for _, fp := range cases {
		if _, err := r.Resolve(fp); !errors.Is(err, ErrInvalid) {
			t.Fatalf("Resolve(%+v) err = %v, want ErrInvalid", fp, err)
		}
	}
}

func TestResolve_MultiProtoSameProduct(t *testing.T) {
	r := New(fixture(t))
	s1, err := r.Resolve(Fingerprint{
		Vendor: "axon", Product: "synapse",
		Model: "RRS18", SwRev: "1601", Proto: "acp1",
	})
	if err != nil {
		t.Fatalf("Resolve acp1: %v", err)
	}
	s2, err := r.Resolve(Fingerprint{
		Vendor: "axon", Product: "synapse",
		Model: "RRS18", SwRev: "1601", Proto: "acp2",
	})
	if err != nil {
		t.Fatalf("Resolve acp2: %v", err)
	}
	// Same product, different protocols -> different schemas.
	if len(s1.Slots[1].Slots[0].Objects) == len(s2.Slots[1].Slots[0].Objects) {
		t.Fatal("acp1 and acp2 schemas should differ in object count")
	}
}

func TestLookupAlternate(t *testing.T) {
	r := New(fixture(t))
	alts, err := r.LookupAlternate(Fingerprint{
		Vendor: "axon", Product: "synapse",
		Model: "RRS18", SwRev: "1601", Proto: "acp1",
	})
	if err != nil {
		t.Fatalf("LookupAlternate: %v", err)
	}
	if len(alts) != 1 {
		t.Fatalf("alts = %d, want 1", len(alts))
	}
	if alts[0].SwRev != "1602" {
		t.Fatalf("alt swrev = %q, want 1602", alts[0].SwRev)
	}
}

func TestPersist_Roundtrip(t *testing.T) {
	root := t.TempDir()
	r := New(root)

	original := &Schema{
		Fingerprint: Fingerprint{
			Vendor: "axon", Product: "synapse",
			Model: "RRS18", SwRev: "2000", Proto: "acp1",
		},
		Slots: map[int]*export.Snapshot{
			1: makeSnapshot("RRS18", []protocol.Object{
				{Slot: 1, Group: "control", ID: 0, Label: "Card Name", Kind: protocol.KindString},
			}),
		},
	}
	if err := r.Persist(original); err != nil {
		t.Fatalf("Persist: %v", err)
	}

	got, err := r.Resolve(original.Fingerprint)
	if err != nil {
		t.Fatalf("Resolve roundtrip: %v", err)
	}
	if len(got.Slots) != 1 {
		t.Fatalf("slots = %d, want 1", len(got.Slots))
	}
	if got.Slots[1].Slots[0].Objects[0].Label != "Card Name" {
		t.Fatalf("label = %q, want Card Name", got.Slots[1].Slots[0].Objects[0].Label)
	}
}

func TestPersist_AtomicReplaceExisting(t *testing.T) {
	root := fixture(t)
	r := New(root)

	updated := &Schema{
		Fingerprint: Fingerprint{
			Vendor: "axon", Product: "synapse",
			Model: "RRS18", SwRev: "1601", Proto: "acp1",
		},
		Slots: map[int]*export.Snapshot{
			1: makeSnapshot("RRS18-updated", []protocol.Object{
				{Slot: 1, Group: "control", ID: 0, Label: "Card Name", Kind: protocol.KindString},
				{Slot: 1, Group: "control", ID: 9, Label: "NewControl", Kind: protocol.KindBool},
			}),
		},
	}
	if err := r.Persist(updated); err != nil {
		t.Fatalf("Persist: %v", err)
	}

	got, err := r.Resolve(updated.Fingerprint)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(got.Slots[1].Slots[0].Objects) != 2 {
		t.Fatalf("got %d objects, want 2", len(got.Slots[1].Slots[0].Objects))
	}
}

func TestDiff_AddedRemovedChanged(t *testing.T) {
	prev := &Schema{
		Fingerprint: Fingerprint{Model: "RRS18", SwRev: "1601", Proto: "acp1"},
		Slots: map[int]*export.Snapshot{
			1: makeSnapshot("RRS18", []protocol.Object{
				{Slot: 1, ID: 1, Label: "A", Kind: protocol.KindInt},
				{Slot: 1, ID: 2, Label: "B", Kind: protocol.KindInt},
				{Slot: 1, ID: 3, Label: "C", Kind: protocol.KindInt},
			}),
		},
	}
	cur := &Schema{
		Fingerprint: Fingerprint{Model: "RRS18", SwRev: "1601", Proto: "acp1"},
		Slots: map[int]*export.Snapshot{
			1: makeSnapshot("RRS18", []protocol.Object{
				{Slot: 1, ID: 1, Label: "A", Kind: protocol.KindInt}, // unchanged
				{Slot: 1, ID: 2, Label: "B", Kind: protocol.KindFloat}, // changed kind
				// C removed
				{Slot: 1, ID: 4, Label: "D", Kind: protocol.KindBool}, // added
			}),
		},
	}
	r := New(t.TempDir())
	d := r.Diff(prev, cur)
	if len(d.PerSlot[1].Added) != 1 || d.PerSlot[1].Added[0] != "D" {
		t.Fatalf("Added = %v, want [D]", d.PerSlot[1].Added)
	}
	if len(d.PerSlot[1].Removed) != 1 || d.PerSlot[1].Removed[0] != "C" {
		t.Fatalf("Removed = %v, want [C]", d.PerSlot[1].Removed)
	}
	if len(d.PerSlot[1].Changed) != 1 || d.PerSlot[1].Changed[0] != "B" {
		t.Fatalf("Changed = %v, want [B]", d.PerSlot[1].Changed)
	}
}

func TestDiff_AddedRemovedSlots(t *testing.T) {
	prev := &Schema{
		Fingerprint: Fingerprint{Model: "RRS18", SwRev: "1601", Proto: "acp1"},
		Slots: map[int]*export.Snapshot{
			1: makeSnapshot("X", nil),
			2: makeSnapshot("X", nil),
		},
	}
	cur := &Schema{
		Fingerprint: Fingerprint{Model: "RRS18", SwRev: "1601", Proto: "acp1"},
		Slots: map[int]*export.Snapshot{
			2: makeSnapshot("X", nil),
			3: makeSnapshot("X", nil),
		},
	}
	r := New(t.TempDir())
	d := r.Diff(prev, cur)
	if len(d.AddedSlots) != 1 || d.AddedSlots[0] != 3 {
		t.Fatalf("AddedSlots = %v, want [3]", d.AddedSlots)
	}
	if len(d.RemovedSlots) != 1 || d.RemovedSlots[0] != 1 {
		t.Fatalf("RemovedSlots = %v, want [1]", d.RemovedSlots)
	}
}

func TestPersist_RejectsUnsafeFingerprint(t *testing.T) {
	r := New(t.TempDir())
	s := &Schema{
		Fingerprint: Fingerprint{
			Vendor: "../etc", Product: "synapse",
			Model: "RRS18", SwRev: "1601", Proto: "acp1",
		},
	}
	if err := r.Persist(s); err == nil {
		t.Fatal("Persist accepted path-traversal fingerprint")
	}
}
