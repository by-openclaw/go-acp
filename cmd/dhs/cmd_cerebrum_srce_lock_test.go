package main

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"dhs/internal/cerebrum-nb/codec"
)

func TestCerebrumSrceLockCSVRoundTrip(t *testing.T) {
	csv := formatCerebrumSrceLockCSV([]cerebrumLockSpec{
		{Dest: "16", Level: "2", State: "LOCKED", LockedBy: "Admin"},
		{Dest: "16", Level: "1", State: "locked", LockedBy: "Admin"},
		{Dest: "9", Level: "1", State: "RELEASED"},
	})
	if want := "srce,state,levels,locked_by\n16,LOCKED,1;2,Admin\n"; csv != want {
		t.Fatalf("csv = %q, want %q", csv, want)
	}
	rows, err := parseCerebrumSrceLockCSV([]byte(csv), "t")
	if err != nil {
		t.Fatal(err)
	}
	want := []cerebrumLockRow{{Dest: "16", State: "LOCKED", Levels: []string{"1", "2"}, LockedBy: "Admin"}}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("got %+v", rows)
	}
	if _, err := parseCerebrumLockCSV([]byte(csv), "t"); err == nil || !strings.Contains(err.Error(), `"dest"`) {
		t.Errorf("a source file read as a destination file: %v", err)
	}
	if _, err := parseCerebrumSrceLockCSV([]byte("srce,state,levels\n,LOCKED,1\n"), "t"); err == nil || !strings.Contains(err.Error(), "empty srce") {
		t.Errorf("empty id: %v", err)
	}
}

func TestCerebrumLockMismatch(t *testing.T) {
	locks := []cerebrumLockSpec{{Dest: "7", Level: "1", State: "locked"}, {Dest: "7", Level: "2", State: "RELEASED"}, {Dest: "8", Level: "3", State: "LOCKED"}}
	for _, tc := range []struct {
		levels       []string
		desired      string
		level, state string
		bad          bool
	}{
		{[]string{"1"}, "LOCKED", "", "", false},
		{[]string{"1", "2"}, "LOCKED", "2", "RELEASED", true},
		{[]string{"3"}, "LOCKED", "3", "RELEASED", true}, // another id's cell, absent here
		{[]string{"3"}, "RELEASED", "", "", false},
	} {
		level, state, bad := cerebrumLockMismatch(locks, "7", tc.levels, tc.desired)
		if level != tc.level || state != tc.state || bad != tc.bad {
			t.Errorf("%v %s: got %q %q %v", tc.levels, tc.desired, level, state, bad)
		}
	}
}

func TestCerebrumListenSrceLockItem(t *testing.T) {
	for _, tc := range []struct{ in, want string }{{"", "*"}, {"  ", "*"}, {"2", "2"}} {
		got := cerebrumListenSrceLockItem("0.0.0.0", tc.in)
		if got.Type != "SRCE_LOCK" || got.SrceID != "*" || got.LevelID != tc.want || got.DeviceType != codec.DeviceType("ROUTER") {
			t.Errorf("level %q: %+v", tc.in, got)
		}
	}
}

func TestBuildDeviceConfigProtocolFlags(t *testing.T) {
	dc := &codec.DeviceConfiguration{}
	if _, err := buildDeviceConfigBody(dc, "generic", deviceConfigBodyFlags{virtualise: "1", secondaryIP: "10.0.0.2", protoSpecific: "<A/>"}); err != nil {
		t.Fatal(err)
	}
	g := dc.Generic
	if g.Protocol == nil || g.Protocol.Virtualise != "1" || g.Protocol.SecondaryIP != "10.0.0.2" {
		t.Errorf("protocol = %+v", g.Protocol)
	}
	if g.ProtocolSpecific == nil || *g.ProtocolSpecific != "<A/>" {
		t.Errorf("protocol specific = %v", g.ProtocolSpecific)
	}
}

func TestCerebrumRMAddFlagsRefused(t *testing.T) {
	for name, args := range map[string][]string{
		"check without total":  {"--kind", "source", "--check", "h"},
		"total and federation": {"--kind", "source", "--total", "5", "--federation", "h"},
		"negative total":       {"--kind", "source", "--total", "-1", "h"},
	} {
		if err := cerebrumRMAdd(context.Background(), args); err == nil || !strings.Contains(err.Error(), "--total") {
			t.Errorf("%s: %v", name, err)
		}
	}
}
