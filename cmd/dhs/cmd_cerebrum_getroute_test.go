package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"dhs/internal/cerebrum-nb/codec"
)

// A device's address is set by hand: --roots holds across instances by name.
func TestCerebrumSeedRoots(t *testing.T) {
	seeds := map[string][]string{
		"NMOS/0":     {"Nodes", "Actions", "SubDevices"},
		"NMOS/*":     {"Devices"},
		"10.6.0.9/1": {"BOARD"},
		"10.6.0.8/*": {"IDENTITY"},
	}
	for _, tc := range []struct {
		name string
		tgt  cerebrumExtractTarget
		want string
	}{
		{"name and sub", cerebrumExtractTarget{IP: "1.0.0.1", Name: "NMOS ", Sub: "0"}, "Nodes"},
		{"name on another address", cerebrumExtractTarget{IP: "7.7.7.7", Name: "NMOS", Sub: "0"}, "Nodes"},
		{"any other slot", cerebrumExtractTarget{IP: "1.0.0.1", Name: "NMOS", Sub: "4"}, "Devices"},
		{"address and sub", cerebrumExtractTarget{IP: "10.6.0.9", Name: "x", Sub: "1"}, "BOARD"},
		{"address, any sub", cerebrumExtractTarget{IP: "10.6.0.8", Name: "x", Sub: "3"}, "IDENTITY"},
	} {
		got, ok := cerebrumSeedRoots(seeds, tc.tgt)
		if !ok || got[0] != tc.want {
			t.Errorf("%s: got %v %v", tc.name, got, ok)
		}
	}
	if got, ok := cerebrumSeedRoots(seeds, cerebrumExtractTarget{IP: "9.9.9.9", Name: "y", Sub: "0"}); ok {
		t.Errorf("an unknown device must have no seeds, got %v", got)
	}
	if got, ok := cerebrumSeedRoots(map[string][]string{"Admin/0": {}}, cerebrumExtractTarget{IP: "3.0.0.1", Name: "Admin", Sub: "0"}); !ok || len(got) != 0 {
		t.Errorf("an empty line says skip: got %v %v", got, ok)
	}
}

// DETAILS lists slots from 1: sub-device 0 is there only when --roots says so.
func TestCerebrumAddRootTargets(t *testing.T) {
	targets := []cerebrumExtractTarget{
		{IP: "1.0.0.1", Name: "NMOS", Sub: "1"},
		{IP: "1.0.0.1", Name: "NMOS", Sub: "2"},
		{IP: "0.0.0.0", Name: "Cerebrum", Sub: "1"},
		{IP: "10.6.0.1", Name: "Neuron", Sub: "1"},
		{IP: "3.0.0.1", Name: "Admin", Sub: "0"},
	}
	seeds := map[string][]string{"NMOS/0": {"Nodes"}, "0.0.0.0/0": {"License"}, "NMOS/*": {"Devices"}, "Admin/0": {}}
	got := cerebrumAddRootTargets(targets, seeds)
	want := append(append([]cerebrumExtractTarget{}, targets...),
		cerebrumExtractTarget{IP: "1.0.0.1", Name: "NMOS", Sub: "0"},
		cerebrumExtractTarget{IP: "0.0.0.0", Name: "Cerebrum", Sub: "0"})
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v", got)
	}
}

func TestCerebrumFallbackModel(t *testing.T) {
	if got := cerebrumFallbackModel(cerebrumExtractTarget{IP: "1.0.0.1", Name: "NMOS ", Sub: "2"}); got != "NMOS-2" {
		t.Errorf("got %q", got)
	}
	if got := cerebrumFallbackModel(cerebrumExtractTarget{IP: "1.0.0.1", Sub: "2"}); got != "1.0.0.1-2" {
		t.Errorf("got %q", got)
	}
}

func TestCerebrumRouteRowsListEveryLevelInOrder(t *testing.T) {
	st := &cerebrumState{
		Routes: []routeSpec{{Dest: "16", Srce: "2", Level: "10"}, {Dest: "9", Srce: "3", Level: "2"}},
		Src:    []cerebrumMneRow{{ID: "2", Mnemonic: "CONVERT 2"}},
		Dst:    []cerebrumMneRow{{ID: "16", Mnemonic: "PGMv"}},
		Lvl:    []cerebrumMneRow{{ID: "10", Mnemonic: "ANC"}, {ID: "2", Mnemonic: "Audio"}},
	}
	got := cerebrumRouteRows("16", "", st)
	want := []cerebrumRouteRow{
		{Dest: "16", DestName: "PGMv", Level: "2", LevelName: "Audio", Unrouted: true},
		{Dest: "16", DestName: "PGMv", Level: "10", LevelName: "ANC", Srce: "2", SrceName: "CONVERT 2"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
	if one := cerebrumRouteRows("16", "10", st); len(one) != 1 || one[0].Level != "10" {
		t.Fatalf("--level: %+v", one)
	}
}

// A cross-level cell is a route: never "unrouted".
func TestCerebrumRouteRowsShowACrossLevelSource(t *testing.T) {
	st := &cerebrumState{
		Routes: []routeSpec{{Dest: "16", Srce: "2", Level: "1", SrceLevel: "2"}},
		Src:    []cerebrumMneRow{{ID: "2", Mnemonic: "CONVERT 2"}},
		Lvl:    []cerebrumMneRow{{ID: "1", Mnemonic: "Video"}, {ID: "2", Mnemonic: "Audio"}},
	}
	got := cerebrumRouteRows("16", "1", st)
	want := []cerebrumRouteRow{{Dest: "16", Level: "1", LevelName: "Video", Srce: "2", SrceName: "CONVERT 2", SrceLevel: "2", SrceLevelName: "Audio"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestCerebrumRootsOfDMLeaveOutTheACP2Root(t *testing.T) {
	p := filepath.Join(t.TempDir(), "m.json")
	dm := `{"objects":[{"path":["ROOT_NODE_V2"]},{"path":["ROOT_NODE_V2","IDENTITY"]},` +
		`{"path":["ROOT_NODE_V2","IDENTITY","Card Name"]},{"path":["ROOT_NODE_V2","BOARD"]}]}`
	if err := os.WriteFile(p, []byte(dm), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := cerebrumRootsOfDM(p); !reflect.DeepEqual(got, []string{"IDENTITY", "BOARD"}) {
		t.Fatalf("roots = %v", got)
	}
	if got := cerebrumRootsOfDM(p + ".absent"); got != nil {
		t.Fatalf("absent file: %v", got)
	}
}

func TestCerebrumExtractTargets(t *testing.T) {
	if got := cerebrumExtractTargets("10.0.0.1", "n", nil); len(got) != 1 || got[0].Sub != "0" {
		t.Fatalf("no sub-device: %+v", got)
	}
	got := cerebrumExtractTargets("10.0.0.1", "n", []codec.DeviceEntry{{Index: 1}, {Index: 2}})
	if len(got) != 2 || got[1].Sub != "2" {
		t.Fatalf("sub-devices: %+v", got)
	}
}
