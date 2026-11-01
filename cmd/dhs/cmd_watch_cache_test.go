package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"dhs/internal/consumer"
	"dhs/internal/datastore"
	"dhs/internal/manifest"
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
	treeStore = datastore.NewTreeStore(root)
	t.Cleanup(func() { treeStore = prev })
	return root
}

func makeObjs() []consumer.Object {
	return []consumer.Object{
		{Slot: 1, ID: 70232, Label: "Backup Input"},
	}
}

// TestSaveSlotCache_ACP2_WritesIdentityKeyedOnly: ACP2 cache lives at
// .cache/dm/<CardName>@<HwVer>.json. No IP-keyed file. Identity probe
// is called with the watched slot.
func TestSaveSlotCache_ACP2_WritesIdentityKeyedOnly(t *testing.T) {
	root := withTempStore(t)
	prober := &fakeProber{identity: "SHPRM1@0.7"}

	saveSlotCache(context.Background(), prober, "10.100.0.103", "acp2", 1, makeObjs(), nil)

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

	saveSlotCache(context.Background(), nil, "10.100.0.103", "emberplus", 1, makeObjs(), nil)

	if _, err := os.Stat(filepath.Join(root, "dm")); !os.IsNotExist(err) {
		t.Errorf("dm dir MUST NOT exist when prober is nil, err=%v", err)
	}
	ipPath := filepath.Join(root, "devices", "10.100.0.103", "slot_1.json")
	if _, err := os.Stat(ipPath); err != nil {
		t.Errorf("IP-keyed file expected as fallback, got err=%v", err)
	}
}

// TestSaveSlotCache_EmptyIdentity_WritesFallback: probe returns "" →
// DM still lands under "unknown@<host>.json" so downstream tooling
// keeps working and the device is not silently dropped. The user
// renames once real identity is determined.
func TestSaveSlotCache_EmptyIdentity_WritesFallback(t *testing.T) {
	root := withTempStore(t)
	prober := &fakeProber{identity: ""}

	got := saveSlotCache(context.Background(), prober, "10.100.0.103", "acp2", 1, makeObjs(), nil)

	if want := "unknown@10.100.0.103"; got != want {
		t.Errorf("identity = %q, want %q", got, want)
	}
	dmPath := filepath.Join(root, "dm", "acp2", "unknown@10.100.0.103.json")
	if _, err := os.Stat(dmPath); err != nil {
		t.Errorf("fallback DM missing at %s: %v", dmPath, err)
	}
}

// TestSaveSlotCache_ProberError_WritesFallback: probe returns an error
// → same fallback rule applies. The DM lands; a warning is logged.
func TestSaveSlotCache_ProberError_WritesFallback(t *testing.T) {
	root := withTempStore(t)
	prober := &fakeProber{err: errors.New("synthetic probe error")}

	got := saveSlotCache(context.Background(), prober, "192.168.1.50", "emberplus", 0, makeObjs(), nil)

	if want := "unknown@192.168.1.50"; got != want {
		t.Errorf("identity = %q, want %q", got, want)
	}
	dmPath := filepath.Join(root, "dm", "emberplus", "unknown@192.168.1.50.json")
	if _, err := os.Stat(dmPath); err != nil {
		t.Errorf("fallback DM missing at %s: %v", dmPath, err)
	}
}

// TestSaveSlotCache_NonACP2_WritesIPKeyed: ACP1 / Ember+ keep
// .cache/devices/<ip>/slot_<n>.json (no identity probe used).
func TestSaveSlotCache_NonACP2_WritesIPKeyed(t *testing.T) {
	root := withTempStore(t)

	saveSlotCache(context.Background(), nil, "10.6.239.113", "acp1", 0, makeObjs(), nil)

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

	slot0 := []consumer.Object{{Slot: 0, ID: 1, Label: "BOARD"}}
	slot1 := []consumer.Object{{Slot: 1, ID: 70232, Label: "Backup Input"}}

	saveSlotCache(ctx, prober, "10.41.40.4", "acp2", 0, slot0, nil)
	saveSlotCache(ctx, prober, "10.41.40.4", "acp2", 1, slot1, nil)

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

	slot0Objs := []consumer.Object{{Slot: 0, ID: 70232, Label: "Backup Input"}}
	slot1Objs := []consumer.Object{{Slot: 1, ID: 70232, Label: "Backup Input"}}

	saveSlotCache(ctx, prober, "10.41.40.4", "acp2", 0, slot0Objs, nil)
	saveSlotCache(ctx, prober, "10.41.40.4", "acp2", 1, slot1Objs, nil)

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

type describedProber struct{ *fakeProber }

func (describedProber) DeviceIdentity(context.Context) map[string]string {
	return map[string]string{"constructor": "Acme"}
}

func (describedProber) UnitIdentity(context.Context) map[string]string {
	return map[string]string{"serial": "S1"}
}

// A plugin with more to say than Model@SwRev has it kept in the DM.
func TestSaveSlotCache_DMCarriesTheIdentity(t *testing.T) {
	withTempStore(t)
	prober := describedProber{&fakeProber{identity: "Box@1.0"}}
	if got := saveSlotCache(context.Background(), prober, "10.0.0.1", "snmp", 0, makeObjs(), nil); got != "Box@1.0" {
		t.Fatalf("identity %q", got)
	}
	raw, err := os.ReadFile(treeStore.IdentityPath("snmp", "Box@1.0"))
	if err != nil {
		t.Fatal(err)
	}
	var dm datastore.DM
	if err := json.Unmarshal(raw, &dm); err != nil {
		t.Fatal(err)
	}
	if dm.Identity["constructor"] != "Acme" || dm.Identity["serial"] != "" || dm.Model != "Box" || len(dm.Objects) == 0 {
		t.Errorf("%+v", dm)
	}
}

// What is true of one box goes in its manifest, keyed by its IP.
func TestUnitFactsGoInTheManifest(t *testing.T) {
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })

	unit := unitIdentity(context.Background(), describedProber{&fakeProber{}})
	writeSlotManifest("", "snmp", "10.0.0.1", 161, []slotBinding{{Slot: 0, Identity: "Box@1.0"}}, unit)
	m, err := manifest.Load(filepath.Join(".cache", "manifest", "snmp", "10.0.0.1.json"))
	if err != nil {
		t.Fatal(err)
	}
	if m.Device.Unit["serial"] != "S1" || m.Device.Name != "Box" || m.Device.Endpoints[0].Transport != "udp" {
		t.Errorf("%+v", m.Device)
	}
	if unitIdentity(context.Background(), &fakeProber{}) != nil {
		t.Error("a plugin with nothing to say says nothing")
	}
}

type portPlugin struct {
	consumer.Protocol
	port int
	err  error
}

func (p portPlugin) GetDeviceInfo(context.Context) (consumer.DeviceInfo, error) {
	return consumer.DeviceInfo{Port: p.port}, p.err
}

// With no --port the port is the plugin's default, not a zero in the file.
func TestConnectedPort(t *testing.T) {
	ctx := context.Background()
	if got := connectedPort(ctx, portPlugin{port: 161}, 0); got != 161 {
		t.Errorf("default = %d", got)
	}
	if got := connectedPort(ctx, portPlugin{port: 161}, 1161); got != 1161 {
		t.Errorf("--port = %d", got)
	}
	if got := connectedPort(ctx, portPlugin{err: errors.New("down")}, 0); got != 0 {
		t.Errorf("no answer = %d", got)
	}
}

// A second walk of a Model@SwRev keeps the DM, and still binds the device to it.
func TestSaveSlotCache_ASecondWalkKeepsTheDM(t *testing.T) {
	withTempStore(t)
	prober := &fakeProber{identity: "Box@1.0"}
	first := []consumer.Object{{Label: "first"}}
	second := []consumer.Object{{Label: "second"}}
	if got := saveSlotCache(context.Background(), prober, "10.0.0.1", "snmp", 0, first, nil); got != "Box@1.0" {
		t.Fatal(got)
	}
	if got := saveSlotCache(context.Background(), prober, "10.0.0.2", "snmp", 0, second, nil); got != "Box@1.0" {
		t.Fatalf("the second unit lost its binding: %q", got)
	}
	raw, err := os.ReadFile(treeStore.IdentityPath("snmp", "Box@1.0"))
	if err != nil {
		t.Fatal(err)
	}
	var dm datastore.DM
	if err := json.Unmarshal(raw, &dm); err != nil {
		t.Fatal(err)
	}
	if len(dm.Objects) != 1 || dm.Objects[0].Label != "first" {
		t.Errorf("%+v", dm.Objects)
	}
}
