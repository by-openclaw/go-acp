package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"dhs/internal/protocol"
	"dhs/internal/storage"
)

// fakeProber satisfies the new per-slot identityProber contract.
// identityBySlot lets each slot return a different card identity
// (different cards in different slots), and identity is the default
// when no per-slot override is set.
type fakeProber struct {
	identity       string
	identityBySlot map[int]string
	err            error
	calls          int
}

func (f *fakeProber) IdentityProbe(_ context.Context, slot int) (string, error) {
	f.calls++
	if id, ok := f.identityBySlot[slot]; ok {
		return id, f.err
	}
	return f.identity, f.err
}

// withTempStore swaps the package-level treeStore for one rooted at a
// fresh tempdir, and restores the original on cleanup.
func withTempStore(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	prev := treeStore
	treeStore = storage.NewTreeStore(root)
	t.Cleanup(func() { treeStore = prev })
	return root
}

func makeObjs() []protocol.Object {
	return []protocol.Object{
		{Slot: 1, ID: 70232, Label: "Backup Input"},
	}
}

// TestSaveSlotCache_ACP2_WritesIdentityKeyedOnly: ACP2 cache lives at
// .cache/dm/<CardName>@<HwVer>.json. No IP-keyed file. Identity probe
// is called with the watched slot.
func TestSaveSlotCache_ACP2_WritesIdentityKeyedOnly(t *testing.T) {
	root := withTempStore(t)
	prober := &fakeProber{identity: "SHPRM1@0.7"}

	saveSlotCache(context.Background(), prober, "10.100.0.103", "acp2", 1, makeObjs())

	dmPath := filepath.Join(root, "dm", "acp2", "SHPRM1@0.7.json")
	if _, err := os.Stat(dmPath); err != nil {
		t.Fatalf("identity-keyed file missing: %v", err)
	}
	ipPath := filepath.Join(root, "devices", "10.100.0.103", "slot_1.json")
	if _, err := os.Stat(ipPath); !os.IsNotExist(err) {
		t.Errorf("IP-keyed file MUST NOT exist for acp2, got err=%v", err)
	}
	if prober.calls != 1 {
		t.Errorf("identity probe call count: got %d want 1", prober.calls)
	}
}

// TestSaveSlotCache_NilProber_FallsBackToIPKeyed: when the plugin
// does NOT satisfy identityProber (nil), the cache routing falls
// through to the legacy IP-keyed path regardless of the protocol
// name. This is how Ember+ keeps working today and how any future
// protocol joins gracefully — implement IdentityProbe to opt in
// to per-card MasterView, otherwise IP-keyed by default.
func TestSaveSlotCache_NilProber_FallsBackToIPKeyed(t *testing.T) {
	root := withTempStore(t)

	saveSlotCache(context.Background(), nil, "10.100.0.103", "emberplus", 1, makeObjs())

	if _, err := os.Stat(filepath.Join(root, "dm")); !os.IsNotExist(err) {
		t.Errorf("dm dir MUST NOT exist when prober is nil, err=%v", err)
	}
	ipPath := filepath.Join(root, "devices", "10.100.0.103", "slot_1.json")
	if _, err := os.Stat(ipPath); err != nil {
		t.Errorf("IP-keyed file expected as fallback, got err=%v", err)
	}
}

// TestSaveSlotCache_ACP2_EmptyIdentity_NoFile: probe returns "" → no
// file written (no safe filename).
func TestSaveSlotCache_ACP2_EmptyIdentity_NoFile(t *testing.T) {
	root := withTempStore(t)
	prober := &fakeProber{identity: ""}

	saveSlotCache(context.Background(), prober, "10.100.0.103", "acp2", 1, makeObjs())

	if _, err := os.Stat(filepath.Join(root, "dm")); !os.IsNotExist(err) {
		t.Errorf("dm dir MUST NOT be created on empty identity, err=%v", err)
	}
}

// TestSaveSlotCache_NonACP2_WritesIPKeyed: ACP1 / Ember+ keep
// .cache/devices/<ip>/slot_<n>.json (no identity probe used).
func TestSaveSlotCache_NonACP2_WritesIPKeyed(t *testing.T) {
	root := withTempStore(t)

	saveSlotCache(context.Background(), nil, "10.6.239.113", "acp1", 0, makeObjs())

	ipPath := filepath.Join(root, "devices", "10.6.239.113", "slot_0.json")
	if _, err := os.Stat(ipPath); err != nil {
		t.Fatalf("IP-keyed file missing for acp1: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "dm")); !os.IsNotExist(err) {
		t.Errorf("dm dir MUST NOT exist for non-acp2 protocols, err=%v", err)
	}
}

// TestSaveSlotCache_ACP2_DifferentCardsTwoFiles pins the per-card DM
// contract: a frame with two slots holding DIFFERENT cards produces
// TWO DM files, one per card identity. The legacy multi-slot merge
// (one file with both slots inside) is gone.
func TestSaveSlotCache_ACP2_DifferentCardsTwoFiles(t *testing.T) {
	root := withTempStore(t)
	prober := &fakeProber{identityBySlot: map[int]string{
		0: "SHPRM1@0.7", // controller card
		1: "SHPIO@0.7",  // I/O card
	}}
	ctx := context.Background()

	slot0 := []protocol.Object{{Slot: 0, ID: 1, Label: "BOARD"}}
	slot1 := []protocol.Object{{Slot: 1, ID: 70232, Label: "Backup Input"}}

	saveSlotCache(ctx, prober, "10.41.40.4", "acp2", 0, slot0)
	saveSlotCache(ctx, prober, "10.41.40.4", "acp2", 1, slot1)

	for _, id := range []string{"SHPRM1@0.7", "SHPIO@0.7"} {
		snap, err := treeStore.LoadByIdentity("acp2", id)
		if err != nil || snap == nil {
			t.Fatalf("LoadByIdentity(%q): snap=%v err=%v", id, snap, err)
		}
		if len(snap.Slots) != 1 {
			t.Errorf("DM file %q must hold ONE slot dump (one card schema); got %d slots",
				id, len(snap.Slots))
		}
	}

	// Sanity: no IP-keyed leakage.
	if _, err := os.Stat(filepath.Join(root, "devices")); !os.IsNotExist(err) {
		t.Errorf("devices/ dir MUST NOT exist for acp2, err=%v", err)
	}
}

// TestSaveSlotCache_ACP2_SameCardTwoSlots_OneFile pins the dedup
// behaviour: two slots holding the SAME card → one DM file. The
// second save overwrites the first with identical data; loading from
// either slot picks up the same schema.
func TestSaveSlotCache_ACP2_SameCardTwoSlots_OneFile(t *testing.T) {
	root := withTempStore(t)
	prober := &fakeProber{identity: "SHPIO@0.7"} // every slot returns this card
	ctx := context.Background()

	slot0Objs := []protocol.Object{{Slot: 0, ID: 70232, Label: "Backup Input"}}
	slot1Objs := []protocol.Object{{Slot: 1, ID: 70232, Label: "Backup Input"}}

	saveSlotCache(ctx, prober, "10.41.40.4", "acp2", 0, slot0Objs)
	saveSlotCache(ctx, prober, "10.41.40.4", "acp2", 1, slot1Objs)

	files, err := os.ReadDir(filepath.Join(root, "dm", "acp2"))
	if err != nil {
		t.Fatalf("read dm/acp2 dir: %v", err)
	}
	if len(files) != 1 {
		names := make([]string, 0, len(files))
		for _, f := range files {
			names = append(names, f.Name())
		}
		t.Errorf("same-card-two-slots must produce 1 DM file, got %d: %v", len(files), names)
	}
}
