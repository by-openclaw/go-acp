package main

import (
	"testing"

	codec "dhs/internal/cerebrum-nb/codec"
)

// cerebrumListenRoutingItems: the §5.1 wildcards by default, one ROUTE +
// one DEST_LOCK row per --dest otherwise, --level applied to every row.
func TestCerebrumListenRoutingItems(t *testing.T) {
	t.Parallel()
	type row struct{ typ, dest, level string }
	cases := []struct {
		name          string
		router, dests string
		level         string
		want          []row
	}{
		{"wildcards by default", "0.0.0.0", "", "", []row{{"ROUTE", "*", "*"}, {"DEST_LOCK", "*", "*"}}},
		{"one destination", "0.0.0.0", "7", "", []row{{"ROUTE", "7", "*"}, {"DEST_LOCK", "7", "*"}}},
		{"one destination, one level", "0.0.0.0", "7", "1", []row{{"ROUTE", "7", "1"}, {"DEST_LOCK", "7", "1"}}},
		{"two destinations, spaces tolerated", "10.6.255.102", " 3, 4 ", "", []row{{"ROUTE", "3", "*"}, {"DEST_LOCK", "3", "*"}, {"ROUTE", "4", "*"}, {"DEST_LOCK", "4", "*"}}},
		{"level alone narrows the wildcard", "0.0.0.0", "", "2", []row{{"ROUTE", "*", "2"}, {"DEST_LOCK", "*", "2"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := cerebrumListenRoutingItems(tc.router, tc.dests, tc.level)
			if len(got) != len(tc.want) {
				t.Fatalf("%d items, want %d: %+v", len(got), len(tc.want), got)
			}
			for i, rc := range got {
				if rc.Type != tc.want[i].typ || rc.DestID != tc.want[i].dest || rc.LevelID != tc.want[i].level {
					t.Errorf("item %d = %s dest=%s level=%s, want %+v", i, rc.Type, rc.DestID, rc.LevelID, tc.want[i])
				}
				if rc.IPAddress != tc.router || rc.DeviceType != codec.DeviceType("ROUTER") {
					t.Errorf("item %d addressed %s/%s, want %s/ROUTER", i, rc.IPAddress, rc.DeviceType, tc.router)
				}
			}
		})
	}
}

// cerebrumNameJoinTargets: one DETAILS target per IP carrying the class the
// LIST row had — the fix for every name printing "-" on 2.9.3 (the server
// NACKs 10 a DETAILS without DEVICE_TYPE).
func TestCerebrumNameJoinTargets(t *testing.T) {
	t.Parallel()
	entries := []codec.DeviceEntry{
		{IPAddress: "0.0.0.0", DeviceType: "ROUTER"},
		{IPAddress: "10.6.255.102", DeviceType: "DEVICE"},
		{IPAddress: "10.6.255.102", DeviceType: "ROUTER"}, // same IP, second class: one target only
		{IPAddress: "1.0.0.1", DeviceTypes: []codec.DeviceType{"DEVICE"}},
		{IPAddress: "3.0.0.1"}, // no class on the row: DEVICE
		{IPAddress: ""},        // no IP: skipped
	}
	got := cerebrumNameJoinTargets(entries)
	want := []cerebrumDeviceTarget{
		{"0.0.0.0", "ROUTER"},
		{"10.6.255.102", "DEVICE"},
		{"1.0.0.1", "DEVICE"},
		{"3.0.0.1", "DEVICE"},
	}
	if len(got) != len(want) {
		t.Fatalf("%d targets, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("target %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}
