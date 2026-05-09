package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"dhs/internal/protocol"
	"dhs/internal/storage"
)

// fakeProber satisfies identityProber for the ACP2 routing tests.
type fakeProber struct {
	identity string
	err      error
	calls    int
}

func (f *fakeProber) IdentityProbe(_ context.Context) (string, error) {
	f.calls++
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

// TestSaveSlotCache_ACP2_WritesIdentityKeyedOnly pins the #355 contract:
// the ACP2 cache lives in .cache/dm/<identity>.json (DHS 2016 MasterView
// model). The legacy IP-keyed file at .cache/devices/<ip>/slot_<n>.json
// must NOT be created for ACP2 — even though TreeStore.Save still
// supports it for ACP1 / Ember+.
func TestSaveSlotCache_ACP2_WritesIdentityKeyedOnly(t *testing.T) {
	root := withTempStore(t)
	prober := &fakeProber{identity: "SHPRM1@0.7"}

	saveSlotCache(context.Background(), prober, "10.100.0.103", "acp2", 1, makeObjs())

	dmPath := filepath.Join(root, "dm", "SHPRM1@0.7.json")
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

// TestSaveSlotCache_ACP2_NilProber_NoFile guards against silent fall-
// through if a future plugin variant forgets to implement IdentityProbe.
// For ACP2 with a nil prober, no file should be created (warn-only).
func TestSaveSlotCache_ACP2_NilProber_NoFile(t *testing.T) {
	root := withTempStore(t)

	saveSlotCache(context.Background(), nil, "10.100.0.103", "acp2", 1, makeObjs())

	dmDir := filepath.Join(root, "dm")
	if _, err := os.Stat(dmDir); !os.IsNotExist(err) {
		t.Errorf("dm dir MUST NOT exist when prober is nil, err=%v", err)
	}
	ipPath := filepath.Join(root, "devices", "10.100.0.103", "slot_1.json")
	if _, err := os.Stat(ipPath); !os.IsNotExist(err) {
		t.Errorf("IP-keyed file MUST NOT exist for acp2 fallback, err=%v", err)
	}
}

// TestSaveSlotCache_ACP2_EmptyIdentity_NoFile covers the case where the
// probe runs but returns "" (e.g. device offline / Card Name label
// missing). We refuse to write under an empty identity — there is no
// safe filename for it.
func TestSaveSlotCache_ACP2_EmptyIdentity_NoFile(t *testing.T) {
	root := withTempStore(t)
	prober := &fakeProber{identity: ""}

	saveSlotCache(context.Background(), prober, "10.100.0.103", "acp2", 1, makeObjs())

	if _, err := os.Stat(filepath.Join(root, "dm")); !os.IsNotExist(err) {
		t.Errorf("dm dir MUST NOT be created on empty identity, err=%v", err)
	}
}

// TestSaveSlotCache_NonACP2_WritesIPKeyed verifies the ACP1 / Ember+
// path is preserved: no identity probe used, IP-keyed file written.
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

// TestSaveSlotCache_ACP2_MultiSlotMerge pins the multi-slot merge
// contract: walking slot 0 then slot 1 of the same device produces a
// SINGLE identity-keyed file containing both slots, not two separate
// files. This is how a frame with N populated slots accumulates into
// one MasterView.
func TestSaveSlotCache_ACP2_MultiSlotMerge(t *testing.T) {
	root := withTempStore(t)
	prober := &fakeProber{identity: "SHPRM1@0.7"}
	ctx := context.Background()

	slot0 := []protocol.Object{{Slot: 0, ID: 1, Label: "BOARD"}}
	slot1 := []protocol.Object{{Slot: 1, ID: 70232, Label: "Backup Input"}}

	saveSlotCache(ctx, prober, "10.41.40.4", "acp2", 0, slot0)
	saveSlotCache(ctx, prober, "10.41.40.4", "acp2", 1, slot1)

	snap, err := treeStore.LoadByIdentity("SHPRM1@0.7")
	if err != nil || snap == nil {
		t.Fatalf("LoadByIdentity: snap=%v err=%v", snap, err)
	}
	if len(snap.Slots) != 2 {
		t.Fatalf("merged file should hold 2 slots, got %d", len(snap.Slots))
	}
	gotSlots := map[int]bool{}
	for _, sd := range snap.Slots {
		gotSlots[sd.Slot] = true
	}
	if !gotSlots[0] || !gotSlots[1] {
		t.Errorf("merged file missing slot(s): %v", gotSlots)
	}

	// Sanity: no per-slot files appeared on the IP-keyed path.
	if _, err := os.Stat(filepath.Join(root, "devices")); !os.IsNotExist(err) {
		t.Errorf("devices/ dir MUST NOT exist for acp2, err=%v", err)
	}
}
