package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"dhs/internal/cerebrum-nb/codec"
)

// TestParseCerebrumMneCSV pins the mnemonic CSV parse: two columns (key +
// mnemonic — router mnemonics are per-ID, 0v16 §4.1.5/§4.1.6), RFC 4180
// quoting (mnemonics may contain commas), comments, column-order
// independence, all three key kinds, and the error cases.
func TestParseCerebrumMneCSV(t *testing.T) {
	t.Run("quoted comma mnemonic", func(t *testing.T) {
		csv := "srce,mnemonic\n" +
			"# names\n" +
			"5121,\"CAM 1, main\"\n" +
			"5122,CAM2\n"
		rows, _, err := parseCerebrumMneCSV([]byte(csv), "srce", "t.csv")
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		want := []cerebrumMneRow{
			{ID: "5121", Mnemonic: "CAM 1, main"},
			{ID: "5122", Mnemonic: "CAM2"},
		}
		if !reflect.DeepEqual(rows, want) {
			t.Errorf("rows = %+v, want %+v", rows, want)
		}
	})

	t.Run("dest key, reordered columns", func(t *testing.T) {
		rows, _, err := parseCerebrumMneCSV([]byte("mnemonic,dest\nMON1,5123\n"), "dest", "t.csv")
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if len(rows) != 1 || rows[0].ID != "5123" || rows[0].Mnemonic != "MON1" {
			t.Errorf("rows = %+v", rows)
		}
	})

	t.Run("level key", func(t *testing.T) {
		rows, _, err := parseCerebrumMneCSV([]byte("level,mnemonic\n1,LVL-1\n2,LVL-2\n"), "level", "t.csv")
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if len(rows) != 2 || rows[1].ID != "2" || rows[1].Mnemonic != "LVL-2" {
			t.Errorf("rows = %+v", rows)
		}
	})

	for _, tc := range []struct{ name, csv, key string }{
		{"missing key col", "mnemonic\nX\n", "srce"},
		{"missing mnemonic col", "srce\n1\n", "srce"},
		{"empty file", "", "srce"},
		{"blank fields", "srce,mnemonic\n,\n", "srce"},
	} {
		t.Run("error: "+tc.name, func(t *testing.T) {
			if _, _, err := parseCerebrumMneCSV([]byte(tc.csv), tc.key, "t.csv"); err == nil {
				t.Errorf("parse %q: err = nil, want error", tc.csv)
			}
		})
	}
}

// TestDedupeCerebrumMnes pins the export cleanup: the OBTAIN snapshot may
// deliver one row per level all carrying the same per-ID router mnemonic —
// exact (ID,mnemonic) duplicates collapse to one row; a CONFLICTING mnemonic
// for the same ID is kept visible; ordering is numeric-aware.
func TestDedupeCerebrumMnes(t *testing.T) {
	got := dedupeCerebrumMnes([]cerebrumMneRow{
		{ID: "5121", Mnemonic: "CAM1"},
		{ID: "5121", Mnemonic: "CAM1"}, // per-level repeat
		{ID: "5121", Mnemonic: "CAM1"},
		{ID: "5121", Mnemonic: "CAM1-B"}, // conflict stays visible
		{ID: "111", Mnemonic: "VTR"},
	})
	want := []cerebrumMneRow{
		{ID: "111", Mnemonic: "VTR"},
		{ID: "5121", Mnemonic: "CAM1"},
		{ID: "5121", Mnemonic: "CAM1-B"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("dedupe = %+v, want %+v", got, want)
	}
}

// TestCerebrumMneCSVRoundTrip pins format -> parse stability, including a
// mnemonic containing a comma and a quote.
func TestCerebrumMneCSVRoundTrip(t *testing.T) {
	orig := []cerebrumMneRow{
		{ID: "5121", Mnemonic: `CAM "A", main`},
		{ID: "5122", Mnemonic: "plain"},
	}
	csv := formatCerebrumMneCSV("srce", orig)
	back, _, err := parseCerebrumMneCSV([]byte(csv), "srce", "roundtrip")
	if err != nil {
		t.Fatalf("reparse: %v\n%s", err, csv)
	}
	if !reflect.DeepEqual(back, orig) {
		t.Errorf("round-trip drifted:\ngot  %+v\nwant %+v\ncsv:\n%s", back, orig, csv)
	}
}

// TestCerebrumAltMnemonics pins the alternate-label pipeline: extraction from
// the RX slot map (slot>=1 only), dynamic alt_N CSV columns sized to the
// highest used slot, byte-stable round-trip, dedupe slot-merge, and the
// compact table rendering.
func TestCerebrumAltMnemonics(t *testing.T) {
	t.Run("extract slots >= 1", func(t *testing.T) {
		got := altMnemonics(&codec.RoutingChange{Mnemonics: map[int]string{0: "PRIMARY", 1: "Black", 4: "ENG"}})
		if !reflect.DeepEqual(got, map[int]string{1: "Black", 4: "ENG"}) {
			t.Errorf("alts = %v", got)
		}
		if altMnemonics(nil) != nil || altMnemonics(&codec.RoutingChange{}) != nil {
			t.Error("nil/empty rc must yield nil alts")
		}
	})
	t.Run("csv columns + round-trip", func(t *testing.T) {
		rows := []cerebrumMneRow{
			{ID: "11", Levels: []string{"1", "2"}, Mnemonic: "SRC-A", Alts: map[int]string{1: "Black", 4: "ENG, x"}},
			{ID: "12", Levels: []string{"1"}, Mnemonic: "SRC-B"},
		}
		csvText := formatCerebrumMneCSV("srce", rows)
		wantHdr := "srce,levels,mnemonic,alt_1,alt_2,alt_3,alt_4\n"
		if !strings.HasPrefix(csvText, wantHdr) {
			t.Fatalf("header = %q, want prefix %q", csvText[:60], wantHdr)
		}
		back, _, err := parseCerebrumMneCSV([]byte(csvText), "srce", "rt")
		if err != nil {
			t.Fatalf("reparse: %v", err)
		}
		if !reflect.DeepEqual(back, rows) {
			t.Errorf("round-trip drifted:\ngot  %+v\nwant %+v", back, rows)
		}
	})
	t.Run("no alts -> no alt columns", func(t *testing.T) {
		csvText := formatCerebrumMneCSV("srce", []cerebrumMneRow{{ID: "1", Mnemonic: "X"}})
		if !strings.HasPrefix(csvText, "srce,levels,mnemonic\n") {
			t.Errorf("unexpected header: %q", csvText)
		}
	})
	t.Run("dedupe merges slots", func(t *testing.T) {
		got := dedupeCerebrumMnes([]cerebrumMneRow{
			{ID: "11", Mnemonic: "A", Alts: map[int]string{1: "Black"}},
			{ID: "11", Mnemonic: "A", Alts: map[int]string{4: "ENG"}},
		})
		want := []cerebrumMneRow{{ID: "11", Mnemonic: "A", Alts: map[int]string{1: "Black", 4: "ENG"}}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("dedupe = %+v, want %+v", got, want)
		}
	})
	t.Run("table rendering", func(t *testing.T) {
		if got := formatAlts(map[int]string{4: "ENG", 1: "Black"}); got != "1=Black 4=ENG" {
			t.Errorf("formatAlts = %q", got)
		}
		if got := formatAlts(nil); got != "-" {
			t.Errorf("formatAlts(nil) = %q", got)
		}
	})
}

// TestCerebrumNoRouteSentinel pins the live-wire "no route" markers (NOC
// 2026-08-15): 0, 0xFFFFFFFE, 0xFFFFFFFF and empty are unrouted cells; real
// source IDs are not.
func TestCerebrumNoRouteSentinel(t *testing.T) {
	for _, s := range []string{"", "0", "4294967294", "4294967295"} {
		if !cerebrumNoRouteSentinel(s) {
			t.Errorf("sentinel %q not detected", s)
		}
	}
	for _, s := range []string{"1", "11", "9191", "28576"} {
		if cerebrumNoRouteSentinel(s) {
			t.Errorf("real source %q misclassified as sentinel", s)
		}
	}
}

// TestCrossLevelRoute pins the cross-level detector export uses to refuse
// silently flattening SRCE_LEVEL != DEST_LEVEL rows.
func TestCrossLevelRoute(t *testing.T) {
	cases := []struct {
		destLvl, srcLvl string
		want            bool
	}{
		{"1", "1", false},
		{"1", "", false}, // no source level on the row = same-level
		{"", "1", false}, // defensive: no dest level
		{"1", "2", true},
		{"2", "1", true},
	}
	for _, tc := range cases {
		if got := crossLevelRoute(tc.destLvl, tc.srcLvl); got != tc.want {
			t.Errorf("crossLevelRoute(%q,%q) = %v, want %v", tc.destLvl, tc.srcLvl, got, tc.want)
		}
	}
}

// TestPrimaryMnemonic pins the RX extraction: slot 0 of the flattened
// Mnemonics map wins; the TX-attr field is the fallback; nil-safe.
func TestPrimaryMnemonic(t *testing.T) {
	if got := primaryMnemonic(nil); got != "" {
		t.Errorf("nil = %q, want empty", got)
	}
	if got := primaryMnemonic(&codec.RoutingChange{Mnemonics: map[int]string{0: "HD", 1: "V_HD"}}); got != "HD" {
		t.Errorf("slot0 = %q, want HD", got)
	}
	if got := primaryMnemonic(&codec.RoutingChange{Mnemonic: "FALLBACK"}); got != "FALLBACK" {
		t.Errorf("fallback = %q, want FALLBACK", got)
	}
}

// TestCerebrumImportCheckNeedsHost pins the ENSURE contract (ADR-0007):
// `--check` reads live state to compute would_change, so it requires the
// host — a check without one errors, mentioning live state, and the error
// arrives only AFTER all files parsed cleanly (parse-before-wire).
func TestCerebrumImportCheckNeedsHost(t *testing.T) {
	dir := t.TempDir()
	xp := filepath.Join(dir, "x.csv")
	src := filepath.Join(dir, "s.csv")
	if err := os.WriteFile(xp, []byte("dest,srce,levels\n5123,5121,1;2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("srce,mnemonic,alt_1\n5121,CAM1,Black\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := cerebrumImportXpoint(context.Background(), []string{"--xpoint", xp, "--src", src, "--check"})
	if err == nil {
		t.Fatal("--check without host: err = nil, want error (ensure needs live state)")
	}
	if !strings.Contains(err.Error(), "live state") {
		t.Errorf("error should explain the live-state requirement, got: %v", err)
	}
}

// TestCerebrumImportSetGuardRails pins the flag validation: --csv/--xpoint
// exclusivity, nothing-to-import, malformed mnemonic CSV, apply needs host.
func TestCerebrumImportSetGuardRails(t *testing.T) {
	dir := t.TempDir()
	xp := filepath.Join(dir, "x.csv")
	_ = os.WriteFile(xp, []byte("dest,srce,levels\n1,2,1\n"), 0o644)

	if err := cerebrumImportXpoint(context.Background(), []string{"--csv", xp, "--xpoint", xp, "--check"}); err == nil {
		t.Error("--csv + --xpoint: err = nil, want error")
	}
	if err := cerebrumImportXpoint(context.Background(), []string{"--check"}); err == nil {
		t.Error("no inputs: err = nil, want error")
	}
	bad := filepath.Join(dir, "bad.csv")
	_ = os.WriteFile(bad, []byte("srce\n1\n"), 0o644) // no mnemonic col
	if err := cerebrumImportXpoint(context.Background(), []string{"--src", bad, "--check"}); err == nil {
		t.Error("malformed --src: err = nil, want error")
	}
	if err := cerebrumImportXpoint(context.Background(), []string{"--xpoint", xp}); err == nil {
		t.Error("apply without host: err = nil, want error")
	}
}

// TestCerebrumMneCapabilityLevels pins the capability-levels pipeline: parse
// accepts an optional `levels` column; dedupe MERGES levels across per-level
// snapshot repeats (numeric order); format writes the levels column for
// src/dst files but NOT for the level file; empty levels round-trip as empty.
func TestCerebrumMneCapabilityLevels(t *testing.T) {
	t.Run("dedupe merges levels", func(t *testing.T) {
		got := dedupeCerebrumMnes([]cerebrumMneRow{
			{ID: "5121", Mnemonic: "CAM1", Levels: []string{"2"}},
			{ID: "5121", Mnemonic: "CAM1", Levels: []string{"1", "2"}},
			{ID: "5121", Mnemonic: "CAM1", Levels: []string{"12"}},
		})
		want := []cerebrumMneRow{{ID: "5121", Mnemonic: "CAM1", Levels: []string{"1", "2", "12"}}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("dedupe = %+v, want %+v", got, want)
		}
	})
	t.Run("src file carries levels column", func(t *testing.T) {
		got := formatCerebrumMneCSV("srce", []cerebrumMneRow{{ID: "5121", Mnemonic: "CAM1", Levels: []string{"1", "2", "12"}}})
		want := "srce,levels,mnemonic\n5121,1;2;12,CAM1\n"
		if got != want {
			t.Errorf("src csv = %q, want %q", got, want)
		}
	})
	t.Run("level file has no levels column", func(t *testing.T) {
		got := formatCerebrumMneCSV("level", []cerebrumMneRow{{ID: "1", Mnemonic: "Video"}})
		want := "level,mnemonic\n1,Video\n"
		if got != want {
			t.Errorf("level csv = %q, want %q", got, want)
		}
	})
	t.Run("levels round-trip", func(t *testing.T) {
		orig := []cerebrumMneRow{
			{ID: "5121", Mnemonic: "CAM1", Levels: []string{"1", "2", "12"}},
			{ID: "5122", Mnemonic: "GFX"}, // no levels -> empty cell -> stays nil
		}
		back, _, err := parseCerebrumMneCSV([]byte(formatCerebrumMneCSV("srce", orig)), "srce", "rt")
		if err != nil {
			t.Fatalf("reparse: %v", err)
		}
		if !reflect.DeepEqual(back, orig) {
			t.Errorf("round-trip drifted:\ngot  %+v\nwant %+v", back, orig)
		}
	})
}

// TestMneLevelsFromChange pins capability extraction from a *_MNE RX row —
// LIVE-WIRE semantics (NOC frames-src.log 2026-08-15, cross-checked against
// the Routemaster UI): the ASSOCIATION_n INDEX is the Routemaster level; the
// association SRCE_ID is a device port UID (never the row's resource ID, so
// no filtering); RM_LEVEL_ID is the level inside the physical device and is
// NOT used. Row LEVEL_ID is the fallback, except the "*" wildcard echo.
func TestMneLevelsFromChange(t *testing.T) {
	if got := mneLevelsFromChange(nil); got != nil {
		t.Errorf("nil rc = %v, want nil", got)
	}
	// Live shape: port-UID SRCE_ID, RM_LEVEL_ID=0, index = RM level.
	rc := &codec.RoutingChange{
		SrceID:  "11",
		LevelID: "*",
		Associations: []codec.RoutingAssociation{
			{Index: 1, SrceID: "4026531837", RMLevelID: "0"},
			{Index: 2, SrceID: "4026531837", RMLevelID: "0"},
			{Index: 3, SrceID: "4026531837", RMLevelID: "0"},
		},
	}
	if got := mneLevelsFromChange(rc); !reflect.DeepEqual(got, []string{"1", "2", "3"}) {
		t.Errorf("assoc index levels = %v, want [1 2 3]", got)
	}
	if got := mneLevelsFromChange(&codec.RoutingChange{SrceID: "7", LevelID: "4"}); !reflect.DeepEqual(got, []string{"4"}) {
		t.Errorf("fallback = %v, want [4]", got)
	}
	// The wildcard echo must never leak into capability: no associations and
	// LEVEL_ID="*" (our own filter echoed back) yields NO levels.
	if got := mneLevelsFromChange(&codec.RoutingChange{SrceID: "28576", LevelID: "*"}); got != nil {
		t.Errorf("wildcard echo = %v, want nil", got)
	}
}

// TestCerebrumListMneRequiresHost pins that the three inventory verbs error
// before dialling when no host is given (offline guard; the OBTAIN itself is
// device-gated).
func TestCerebrumListMneRequiresHost(t *testing.T) {
	for _, tc := range []struct{ mneType, key string }{
		{"SRCE_MNE", "srce"},
		{"DEST_MNE", "dest"},
		{"LEVEL_MNE", "level"},
	} {
		if err := cerebrumListMne(context.Background(), []string{"--idle", "1s"}, tc.mneType, tc.key); err == nil {
			t.Errorf("list-%s without host: err = nil, want error", tc.key)
		}
	}
}

// TestCerebrumExportSetFlags pins export's flag validation offline:
// --out vs --out-dir exclusivity and the missing-host guard.
func TestCerebrumExportSetFlags(t *testing.T) {
	if err := cerebrumExportXpoint(context.Background(), []string{"--out", "a.csv", "--out-dir", "d"}); err == nil ||
		!strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("--out + --out-dir: err = %v, want mutually-exclusive error", err)
	}
	if err := cerebrumExportXpoint(context.Background(), []string{"--out-dir", t.TempDir()}); err == nil {
		t.Error("export without host: err = nil, want error")
	}
}
