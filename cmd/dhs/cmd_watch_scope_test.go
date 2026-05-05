package main

import (
	"reflect"
	"testing"
)

func TestParseWalkScope(t *testing.T) {
	cases := []struct {
		name     string
		slot     int
		slots    string
		noWalk   bool
		want     walkScope
		wantErr  bool
	}{
		{name: "default-no-flags-walks-nothing", slot: -1, want: walkScope{mode: walkNone}},
		{name: "single-slot-legacy", slot: 1, want: walkScope{mode: walkList, slots: []int{1}}},
		{name: "slots-list", slots: "1,3,7", want: walkScope{mode: walkList, slots: []int{1, 3, 7}}},
		{name: "slots-list-trim", slots: " 1 , 3 , 7 ", want: walkScope{mode: walkList, slots: []int{1, 3, 7}}},
		{name: "slots-all", slots: "all", want: walkScope{mode: walkAll}},
		{name: "slots-ALL-case-insensitive", slots: "ALL", want: walkScope{mode: walkAll}},
		{name: "no-walk", noWalk: true, want: walkScope{mode: walkNone}},
		{name: "no-walk-overrides-slot", slot: 1, noWalk: true, want: walkScope{mode: walkNone}},
		{name: "slots-overrides-slot", slot: 1, slots: "5,7", want: walkScope{mode: walkList, slots: []int{5, 7}}},
		{name: "no-walk-with-slots-mutex", noWalk: true, slots: "1", wantErr: true},
		{name: "slots-empty-list-error", slots: ",,", wantErr: true},
		{name: "slots-non-int-error", slots: "abc", wantErr: true},
		{name: "slots-negative-error", slots: "-1", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.slot == 0 && tc.name != "" && tc.name[:7] != "single-" {
				tc.slot = -1 // default for tests that don't care about --slot
			}
			got, err := parseWalkScope(tc.slot, tc.slots, tc.noWalk)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.mode != tc.want.mode {
				t.Fatalf("mode = %v, want %v", got.mode, tc.want.mode)
			}
			if !reflect.DeepEqual(got.slots, tc.want.slots) {
				t.Fatalf("slots = %v, want %v", got.slots, tc.want.slots)
			}
		})
	}
}

func TestWalkScope_Empty(t *testing.T) {
	if !(walkScope{mode: walkNone}).empty() {
		t.Fatal("walkNone should be empty")
	}
	if (walkScope{mode: walkList, slots: []int{1}}).empty() {
		t.Fatal("walkList should not be empty")
	}
	if (walkScope{mode: walkAll}).empty() {
		t.Fatal("walkAll should not be empty")
	}
}
