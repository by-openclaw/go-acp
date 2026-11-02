package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"dhs/internal/cerebrum-nb/codec"
)

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
