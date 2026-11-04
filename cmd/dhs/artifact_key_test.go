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
