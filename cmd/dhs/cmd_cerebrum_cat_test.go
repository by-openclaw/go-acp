package main

import (
	"reflect"
	"strings"
	"testing"

	"dhs/internal/cerebrum-nb/codec"
)

// TestParseCerebrumCatCSV pins the minimal category CSV: row order = slot
// order, §3.3 type validation, and the SRC/DST file separation rule.
func TestParseCerebrumCatCSV(t *testing.T) {
	csv := "category,type,value\n" +
		"# nav panel\n" +
		"SRC-STUDIO-A,TEXT,Cameras\n" +
		"SRC-STUDIO-A,SOURCE,10001\n" +
		"SRC-ALL,CATEGORY,SRC-STUDIO-A\n"
	defs, err := parseCerebrumCatCSV([]byte(csv), "src", "t.csv")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := []cerebrumCatDef{
		{Name: "SRC-STUDIO-A", Items: []cerebrumCatItem{{Type: "TEXT", Value: "Cameras"}, {Type: "SOURCE", Value: "10001"}}},
		{Name: "SRC-ALL", Items: []cerebrumCatItem{{Type: "CATEGORY", Value: "SRC-STUDIO-A"}}},
	}
	if !reflect.DeepEqual(defs, want) {
		t.Errorf("defs:\ngot  %+v\nwant %+v", defs, want)
	}

	// Round-trip: format -> parse is stable.
	again, err := parseCerebrumCatCSV([]byte(formatCerebrumCatCSV(defs)), "src", "rt")
	if err != nil || !reflect.DeepEqual(again, defs) {
		t.Errorf("round-trip: %v / %+v", err, again)
	}

	for _, tc := range []struct{ name, kind, csv, want string }{
		{"dest in src file", "src", "category,type,value\nC,DEST,5\n", "keep SRC and DST files separate"},
		{"source in dst file", "dst", "category,type,value\nC,SOURCE,5\n", "keep SRC and DST files separate"},
		{"bad type", "src", "category,type,value\nC,WAT,5\n", "unknown type"},
		{"missing value", "src", "category,type,value\nC,SOURCE,\n", "value is required"},
		{"no rows", "src", "category,type,value\n", "no category rows"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseCerebrumCatCSV([]byte(tc.csv), tc.kind, "t.csv")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want substring %q", err, tc.want)
			}
		})
	}

	// kind=mixed accepts both resource kinds (the -cat-mixed.csv file).
	mixedCSV := "category,type,value\nDESTINATIONS,DEST,4201\nDESTINATIONS,CATEGORY,SRC-ALL\nDESTINATIONS,SOURCE,7\n"
	if _, err := parseCerebrumCatCSV([]byte(mixedCSV), "mixed", "m.csv"); err != nil {
		t.Errorf("mixed kind must accept both: %v", err)
	}
}

// TestDiffCerebrumCategory pins the per-slot ensure: identical grid = no
// changes; differing/missing slots = MODIFY_ITEM; live slots beyond the
// desired grid clear via ITEM_TYPE=BLANK (never DELETE_ITEM — index-shift
// semantics undefined in the spec); absent category = CREATE + slots;
// run-twice = 0.
func TestDiffCerebrumCategory(t *testing.T) {
	live := &codec.CategoryDetailsInfo{Items: []codec.CategoryItem{
		{Index: 1, Type: "TEXT", Value: "Cameras"},
		{Index: 2, Type: "SOURCE", Value: "10001"},
		{Index: 3, Type: "SOURCE", Value: "10099"},
	}}
	desired := []cerebrumCatItem{
		{Type: "TEXT", Value: "Cameras"},   // identical -> none
		{Type: "SOURCE", Value: "10002"},   // differs -> modify slot 2
	}
	got := diffCerebrumCategory("C", live, desired)
	want := []cerebrumCatChange{
		{Cat: "C", Op: "MODIFY_ITEM", Index: 2, Type: "SOURCE", Value: "10002", From: "SOURCE 10001"},
		{Cat: "C", Op: "MODIFY_ITEM", Index: 3, Type: "BLANK", Value: "", From: "SOURCE 10099"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("diff:\ngot  %+v\nwant %+v", got, want)
	}

	// Absent category: CREATE first, then every slot.
	got = diffCerebrumCategory("NEW", nil, []cerebrumCatItem{{Type: "SOURCE", Value: "1"}})
	if len(got) != 2 || got[0].Op != "CREATE" || got[1].Op != "MODIFY_ITEM" || got[1].Index != 1 {
		t.Errorf("create diff = %+v", got)
	}

	// Run-twice = 0: live equal to desired yields nothing.
	after := &codec.CategoryDetailsInfo{Items: []codec.CategoryItem{
		{Index: 1, Type: "TEXT", Value: "Cameras"},
		{Index: 2, Type: "SOURCE", Value: "10002"},
		{Index: 3, Type: "BLANK", Value: ""},
	}}
	if again := diffCerebrumCategory("C", after, desired); len(again) != 0 {
		t.Errorf("run-twice must be 0, got %+v", again)
	}
}

// TestClassifyCerebrumCategories pins the SRC/DST export split: a
// category's own DIRECT resource items decide (a "DST-…" gateway holding
// SOURCE ports is a src category — names are convention); resource-less
// parents inherit from their children; a parent with direct DEST items is
// dst even when a referenced child carries sources (live NOC shape:
// DESTINATIONS -> DST-PASSERELLES gateway); mixed = direct both only.
func TestClassifyCerebrumCategories(t *testing.T) {
	names := []string{"SRC-A", "DST-B", "MIX", "PARENT-SRC", "DST-GATEWAY", "DESTINATIONS"}
	details := map[string]*codec.CategoryDetailsInfo{
		"SRC-A":       {Items: []codec.CategoryItem{{Index: 1, Type: "SOURCE", Value: "1"}}},
		"DST-B":       {Items: []codec.CategoryItem{{Index: 1, Type: "DEST", Value: "2"}}},
		"MIX":         {Items: []codec.CategoryItem{{Index: 1, Type: "SOURCE", Value: "1"}, {Index: 2, Type: "DEST", Value: "2"}}},
		"PARENT-SRC":  {Items: []codec.CategoryItem{{Index: 1, Type: "CATEGORY", Value: "SRC-A"}}},
		"DST-GATEWAY": {Items: []codec.CategoryItem{{Index: 1, Type: "SOURCE", Value: "8601"}}},
		"DESTINATIONS": {Items: []codec.CategoryItem{
			{Index: 1, Type: "CATEGORY", Value: "DST-GATEWAY"},
			{Index: 2, Type: "CATEGORY", Value: "DST-B"},
			{Index: 3, Type: "DEST", Value: "4201"},
		}},
	}
	src, dst, both := classifyCerebrumCategories(names, details)
	if !src["SRC-A"] || !src["PARENT-SRC"] || !src["DST-GATEWAY"] || src["DST-B"] || src["DESTINATIONS"] {
		t.Errorf("src = %v", src)
	}
	if !dst["DST-B"] || !dst["DESTINATIONS"] || dst["SRC-A"] || dst["DST-GATEWAY"] {
		t.Errorf("dst = %v", dst)
	}
	if len(both) != 1 || both[0] != "MIX" || src["MIX"] || dst["MIX"] {
		t.Errorf("both = %v (src[MIX]=%v dst[MIX]=%v)", both, src["MIX"], dst["MIX"])
	}
}
