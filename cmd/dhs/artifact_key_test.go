package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dhs/internal/datastore"
	"dhs/internal/manifest"
)

// One device, one folder: the key is its IP however it was addressed
// (ADR-0028 §2); the hostname is metadata.
func TestDeviceKey(t *testing.T) {
	lookup := func(_ context.Context, h string) ([]string, error) {
		switch h {
		case "matrix-a.lab":
			return []string{"fe80::1", "10.6.250.20"}, nil
		case "v6only.lab":
			return []string{"fd00::7"}, nil
		}
		return nil, errors.New("no such host")
	}
	cases := []struct{ in, key, fqdn string }{
		{"10.6.250.20", "10.6.250.20", ""},
		{"10.6.250.20:2072", "10.6.250.20", ""},
		{"0.0.0.0", "0.0.0.0", ""},
		{"[fd00::7]:9000", "fd00::7", ""},
		{"matrix-a.lab", "10.6.250.20", "matrix-a.lab"},
		{"matrix-a.lab:8000", "10.6.250.20", "matrix-a.lab"},
		{"v6only.lab", "fd00::7", "v6only.lab"},
		{"gone.lab", "gone.lab", "gone.lab"},
	}
	for _, c := range cases {
		if got := deviceKeyWith(lookup, c.in); got != c.key {
			t.Errorf("deviceKey(%q) = %q, want %q", c.in, got, c.key)
		}
		if got := fqdnOf(c.in); got != c.fqdn {
			t.Errorf("fqdnOf(%q) = %q, want %q", c.in, got, c.fqdn)
		}
	}
}

// The matrices of one SW-P-08 router never share a folder: matrix 0 in
// the device folder, matrix N in matrix-N/.
func TestProbelMatrixDir(t *testing.T) {
	base := filepath.Join("snapshots", "probel-sw08p", "10.6.250.40")
	for m, want := range map[int]string{0: base, 1: filepath.Join(base, "matrix-1"), 255: filepath.Join(base, "matrix-255")} {
		if got := probelMatrixDir("10.6.250.40:8910", m); !strings.HasSuffix(got, want) {
			t.Errorf("matrix %d → %q, want suffix %q", m, got, want)
		}
	}
}

// A walk's manifest lands beside dm/ in the .cache the tree store
// uses, never in the current directory (ADR-0028 §1).
func TestSlotManifestBesideDM(t *testing.T) {
	prev := treeStore
	treeStore = datastore.NewTreeStore(t.TempDir())
	t.Cleanup(func() { treeStore = prev })
	wd, _ := os.Getwd()
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	writeSlotManifest("", "acp1", "10.6.250.30:2071", 2071, []slotBinding{{Slot: 1, Identity: "CONVERT@6.7.4"}}, nil)
	p := manifest.PathFor(treeStore.BaseDir(), &manifest.Manifest{Device: manifest.Device{Protocol: "acp1", IP: "10.6.250.30"}})
	mf, err := manifest.Load(p)
	if err != nil {
		t.Fatalf("manifest not at %s: %v", p, err)
	}
	if mf.Device.IP != "10.6.250.30" || mf.Device.FQDN != "" {
		t.Errorf("device ip %q fqdn %q", mf.Device.IP, mf.Device.FQDN)
	}
	if _, err := os.Stat(".cache"); err == nil {
		t.Error("a .cache was created in the current directory")
	}
}

// extract-all writes the sub-devices of one device one by one: the
// manifest keeps every one of them, and a re-extract of one replaces
// only its own binding.
func TestCerebrumManifestKeepsEverySubDevice(t *testing.T) {
	prev := treeStore
	treeStore = datastore.NewTreeStore(t.TempDir())
	t.Cleanup(func() { treeStore = prev })

	for _, w := range []struct{ id, sub string }{{"Cerebrum@2.9.3", "0"}, {"Cerebrum-RM@2.9.3", "3"}, {"Cerebrum-S1@2.9.3", "1"}, {"Cerebrum-RM@2.9.4", "3"}} {
		if _, err := writeCerebrumManifest(w.id, "Cerebrum", "10.6.250.5", "10.6.250.5", 40009, w.sub); err != nil {
			t.Fatal(err)
		}
	}
	mf, err := manifest.Load(manifest.PathFor(treeStore.BaseDir(), &manifest.Manifest{Device: manifest.Device{Protocol: "cerebrum-nb", IP: "10.6.250.5"}}))
	if err != nil {
		t.Fatal(err)
	}
	if len(mf.Frames) != 1 {
		t.Fatalf("frames = %d, want 1", len(mf.Frames))
	}
	got := map[string]string{}
	for _, sl := range mf.Frames[0].Slots {
		got[sl.Addr["sub_device"].(string)] = sl.DM
	}
	want := map[string]string{"0": "Cerebrum@2.9.3.json", "1": "Cerebrum-S1@2.9.3.json", "3": "Cerebrum-RM@2.9.4.json"}
	if len(got) != len(want) {
		t.Fatalf("slots %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("sub-device %s → %q, want %q", k, got[k], v)
		}
	}
}

// One box, three controllers: the second is recognised by its serial,
// the third is named with --primary; every IP then keys to the one
// folder and manifest, which lists all three.
func TestOneDeviceManyControllers(t *testing.T) {
	prev := treeStore
	treeStore = datastore.NewTreeStore(t.TempDir())
	t.Cleanup(func() { treeStore = prev })
	t.Setenv(primaryEnv, "")
	unit := map[string]string{"serial": "NRN-0042"}
	b := []slotBinding{{Slot: 0, Identity: "CONVERT@6.7.4"}}

	writeSlotManifest("", "acp2", "10.6.255.102", 2072, b, unit)
	writeSlotManifest("", "acp2", "10.6.255.122", 2072, b, unit) // same serial → joins
	if _, err := stripPrimaryFlag([]string{"--primary", "10.6.255.102"}); err != nil {
		t.Fatal(err)
	}
	writeSlotManifest("", "acp2", "10.6.255.142", 2072, b, nil) // named
	_ = os.Unsetenv(primaryEnv)

	files, _ := filepath.Glob(filepath.Join(treeStore.BaseDir(), "manifest", "acp2", "*.json"))
	if len(files) != 1 {
		t.Fatalf("manifests %v, want one", files)
	}
	mf, err := manifest.Load(files[0])
	if err != nil {
		t.Fatal(err)
	}
	var ips []string
	for _, e := range mf.Device.Endpoints {
		ips = append(ips, e.IP)
	}
	if mf.Device.IP != "10.6.255.102" || strings.Join(ips, ",") != "10.6.255.102,10.6.255.122,10.6.255.142" {
		t.Fatalf("device %s endpoints %v", mf.Device.IP, ips)
	}
	for _, ip := range []string{"10.6.255.102", "10.6.255.122:2072", "10.6.255.142"} {
		if got := deviceKeyFor("acp2", ip); got != "10.6.255.102" {
			t.Errorf("deviceKeyFor(%s) = %s", ip, got)
		}
	}
	if got := deviceKeyFor("acp1", "10.6.255.122"); got != "10.6.255.122" {
		t.Errorf("another protocol re-keyed: %s", got)
	}

	// UDP (acp1): one controller per device, a second is never joined.
	writeSlotManifest("", "acp1", "10.6.250.30", 2071, b, unit)
	writeSlotManifest("", "acp1", "10.6.250.31", 2071, b, unit)
	udp, _ := filepath.Glob(filepath.Join(treeStore.BaseDir(), "manifest", "acp1", "*.json"))
	if len(udp) != 2 {
		t.Errorf("acp1 manifests %v, want one per controller", udp)
	}
}

func TestPrimaryFlag(t *testing.T) {
	t.Setenv(primaryEnv, "10.9.9.9") // inherited: cleared, never used
	rest, err := stripPrimaryFlag([]string{"consumer", "acp2", "walk", "10.0.0.2"})
	if err != nil || len(rest) != 4 || os.Getenv(primaryEnv) != "" {
		t.Fatalf("rest %v err %v env %q", rest, err, os.Getenv(primaryEnv))
	}
	for _, args := range [][]string{{"--primary=10.0.0.1:2072", "x"}, {"-primary", "10.0.0.1", "x"}, {"-primary=10.0.0.1", "x"}} {
		rest, err := stripPrimaryFlag(args)
		if err != nil || len(rest) != 1 || os.Getenv(primaryEnv) != "10.0.0.1" {
			t.Errorf("%v → %v %v %q", args, rest, err, os.Getenv(primaryEnv))
		}
		if got := deviceKeyFor("acp2", "10.0.0.2"); got != "10.0.0.1" {
			t.Errorf("deviceKeyFor with --primary = %s", got)
		}
	}
	for _, args := range [][]string{{"--primary"}, {"--primary", "frame-a"}} {
		if _, err := stripPrimaryFlag(args); err == nil {
			t.Errorf("%v accepted", args)
		}
	}
	_ = os.Unsetenv(primaryEnv)
}
