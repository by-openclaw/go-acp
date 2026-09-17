package main

import (
	"reflect"
	"testing"
)

// TestDiffCerebrumMnemonics pins the label ensure-diff (ADR-0007): only
// non-empty desired cells that differ from live become changes; empty cells
// and resources absent from the CSV are never touched; slot 0 = primary,
// slots >= 1 = alternates; deterministic (ID, slot) order.
func TestDiffCerebrumMnemonics(t *testing.T) {
	live := []cerebrumMneRow{
		{ID: "11", Mnemonic: "SRC-A", Alts: map[int]string{1: "Black"}},
		{ID: "12", Mnemonic: "SRC-B"},
		{ID: "13", Mnemonic: "SRC-C", Alts: map[int]string{1: "Mire-3"}},
	}
	desired := []cerebrumMneRow{
		{ID: "11", Mnemonic: "SRC-A", Alts: map[int]string{1: "Cam-A", 4: "ENG"}}, // alt1 rename + alt4 new
		{ID: "12", Mnemonic: "SRC-B2"},                                            // primary rename
		{ID: "13", Mnemonic: "SRC-C", Alts: map[int]string{1: "Mire-3"}},          // identical -> no change
		{ID: "99", Mnemonic: "NEW"},                                               // live row absent -> set from ""
	}
	got := diffCerebrumMnemonics("SRCE_MNE", live, desired, nil, false)
	want := []cerebrumMneChange{
		{Kind: "SRCE_MNE", ID: "11", Slot: 1, From: "Black", To: "Cam-A"},
		{Kind: "SRCE_MNE", ID: "11", Slot: 4, From: "", To: "ENG"},
		{Kind: "SRCE_MNE", ID: "12", Slot: 0, From: "SRC-B", To: "SRC-B2"},
		{Kind: "SRCE_MNE", ID: "99", Slot: 0, From: "", To: "NEW"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("diff:\ngot  %+v\nwant %+v", got, want)
	}

	// Empty desired cells never clear: live has alt_1, desired row carries no
	// alts and empty primary -> zero changes.
	got = diffCerebrumMnemonics("SRCE_MNE", live, []cerebrumMneRow{{ID: "11", Mnemonic: ""}}, nil, false)
	if len(got) != 0 {
		t.Errorf("empty desired cells must be ignored, got %+v", got)
	}

	// --allow-clear: an empty managed cell whose live value is set becomes a
	// clear-write (To: ""), for the primary and for header-managed alt slots.
	clr := diffCerebrumMnemonics("SRCE_MNE", live, []cerebrumMneRow{{ID: "11", Mnemonic: ""}}, []int{1}, true)
	wantClr := []cerebrumMneChange{
		{Kind: "SRCE_MNE", ID: "11", Slot: 0, From: "SRC-A", To: ""},
		{Kind: "SRCE_MNE", ID: "11", Slot: 1, From: "Black", To: ""},
	}
	if !reflect.DeepEqual(clr, wantClr) {
		t.Errorf("allow-clear diff:\ngot  %+v\nwant %+v", clr, wantClr)
	}
	// Clear mode never touches unmanaged columns (slot 1 absent from header)
	// or cells already empty live.
	clr = diffCerebrumMnemonics("SRCE_MNE", live, []cerebrumMneRow{{ID: "11", Mnemonic: "SRC-A"}}, []int{4}, true)
	if len(clr) != 0 {
		t.Errorf("allow-clear must not touch unmanaged/empty cells, got %+v", clr)
	}

	// Run-twice = 0: applying `want` to live then re-diffing yields nothing.
	after := []cerebrumMneRow{
		{ID: "11", Mnemonic: "SRC-A", Alts: map[int]string{1: "Cam-A", 4: "ENG"}},
		{ID: "12", Mnemonic: "SRC-B2"},
		{ID: "13", Mnemonic: "SRC-C", Alts: map[int]string{1: "Mire-3"}},
		{ID: "99", Mnemonic: "NEW"},
	}
	if again := diffCerebrumMnemonics("SRCE_MNE", after, desired, nil, false); len(again) != 0 {
		t.Errorf("run-twice must be 0 changes, got %+v", again)
	}
}

// TestDiffCerebrumRoutes pins the crosspoint ensure-diff: only cells whose
// desired source differs from live change; identical cells and cells absent
// from the CSV are untouched (import never disconnects); run-twice = 0.
func TestDiffCerebrumRoutes(t *testing.T) {
	live := []routeSpec{
		{Dest: "13", Srce: "12", Level: "1"},
		{Dest: "13", Srce: "14", Level: "2"},
		{Dest: "20", Srce: "5", Level: "1"},
	}
	desired := []routeSpec{
		{Dest: "13", Srce: "12", Level: "1"}, // identical -> none
		{Dest: "13", Srce: "11", Level: "2"}, // differs -> change
		{Dest: "30", Srce: "7", Level: "1"},  // unrouted live -> change from ""
	}
	got := diffCerebrumRoutes(live, desired)
	want := []cerebrumRouteChange{
		{Dest: "13", Level: "2", From: "14", To: "11"},
		{Dest: "30", Level: "1", From: "", To: "7"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("diff:\ngot  %+v\nwant %+v", got, want)
	}
	// Live cell 20<-5 untouched (absent from CSV). Run-twice:
	after := []routeSpec{
		{Dest: "13", Srce: "12", Level: "1"},
		{Dest: "13", Srce: "11", Level: "2"},
		{Dest: "20", Srce: "5", Level: "1"},
		{Dest: "30", Srce: "7", Level: "1"},
	}
	if again := diffCerebrumRoutes(after, desired); len(again) != 0 {
		t.Errorf("run-twice must be 0 changes, got %+v", again)
	}
}

// TestAltSlotArg pins the SetMnemonic slot rendering: primary = empty,
// alternate = its index.
func TestAltSlotArg(t *testing.T) {
	if got := altSlotArg(0); got != "" {
		t.Errorf("slot 0 = %q, want empty", got)
	}
	if got := altSlotArg(4); got != "4" {
		t.Errorf("slot 4 = %q", got)
	}
}
