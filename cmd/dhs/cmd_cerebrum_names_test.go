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

// TestParseCerebrumMneCSV pins the mnemonic CSV parse: multi-level `levels`
// lists, legacy `level`, RFC 4180 quoting (mnemonics may contain commas),
// comments, column order independence, and the error cases.
func TestParseCerebrumMneCSV(t *testing.T) {
	t.Run("multi-level + quoted comma mnemonic", func(t *testing.T) {
		csv := "srce,levels,mnemonic\n" +
			"# names\n" +
			"5121,1;2,\"CAM 1, main\"\n" +
			"5122,1,CAM2\n"
		rows, err := parseCerebrumMneCSV([]byte(csv), "srce", "t.csv")
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		want := []cerebrumMneRow{
			{ID: "5121", Levels: []string{"1", "2"}, Mnemonic: "CAM 1, main"},
			{ID: "5122", Levels: []string{"1"}, Mnemonic: "CAM2"},
		}
		if !reflect.DeepEqual(rows, want) {
			t.Errorf("rows = %+v, want %+v", rows, want)
		}
	})

	t.Run("legacy level column + dest key + reordered", func(t *testing.T) {
		rows, err := parseCerebrumMneCSV([]byte("mnemonic,dest,level\nMON1,5123,2\n"), "dest", "t.csv")
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if len(rows) != 1 || rows[0].ID != "5123" || rows[0].Mnemonic != "MON1" ||
			!reflect.DeepEqual(rows[0].Levels, []string{"2"}) {
			t.Errorf("rows = %+v", rows)
		}
	})

	for _, tc := range []struct{ name, csv, key string }{
		{"missing key col", "levels,mnemonic\n1,X\n", "srce"},
		{"missing mnemonic col", "srce,levels\n1,1\n", "srce"},
		{"missing level col", "srce,mnemonic\n1,X\n", "srce"},
		{"empty file", "", "srce"},
		{"blank fields", "srce,levels,mnemonic\n,,\n", "srce"},
	} {
		t.Run("error: "+tc.name, func(t *testing.T) {
			if _, err := parseCerebrumMneCSV([]byte(tc.csv), tc.key, "t.csv"); err == nil {
				t.Errorf("parse %q: err = nil, want error", tc.csv)
			}
		})
	}
}

// TestCollapseCerebrumMnes pins the export grouping: per-(id,level) snapshot
// rows with the same mnemonic coalesce into one multi-level row; a different
// mnemonic on another level stays separate; dedup + numeric ordering hold.
func TestCollapseCerebrumMnes(t *testing.T) {
	got := collapseCerebrumMnes([]cerebrumMneRow{
		{ID: "5121", Levels: []string{"2"}, Mnemonic: "CAM1"},
		{ID: "5121", Levels: []string{"10"}, Mnemonic: "CAM1"},
		{ID: "5121", Levels: []string{"1"}, Mnemonic: "CAM1"},
		{ID: "5121", Levels: []string{"1"}, Mnemonic: "CAM1"}, // dup
		{ID: "5121", Levels: []string{"3"}, Mnemonic: "CAM1-HD"},
		{ID: "111", Levels: []string{"1"}, Mnemonic: "VTR"},
	})
	want := []cerebrumMneRow{
		{ID: "111", Levels: []string{"1"}, Mnemonic: "VTR"},
		{ID: "5121", Levels: []string{"1", "2", "10"}, Mnemonic: "CAM1"},
		{ID: "5121", Levels: []string{"3"}, Mnemonic: "CAM1-HD"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("collapse = %+v, want %+v", got, want)
	}
}

// TestCerebrumMneCSVRoundTrip pins format -> parse stability, including a
// mnemonic containing a comma and a quote.
func TestCerebrumMneCSVRoundTrip(t *testing.T) {
	orig := []cerebrumMneRow{
		{ID: "5121", Levels: []string{"1", "2"}, Mnemonic: `CAM "A", main`},
		{ID: "5122", Levels: []string{"3"}, Mnemonic: "plain"},
	}
	csv := formatCerebrumMneCSV("srce", orig)
	back, err := parseCerebrumMneCSV([]byte(csv), "srce", "roundtrip")
	if err != nil {
		t.Fatalf("reparse: %v\n%s", err, csv)
	}
	if !reflect.DeepEqual(back, orig) {
		t.Errorf("round-trip drifted:\ngot  %+v\nwant %+v\ncsv:\n%s", back, orig, csv)
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

// TestCerebrumImportTrioCheckOffline pins that `import --check` with all three
// files is a pure offline dry-run covering routes + src/dst mnemonics.
func TestCerebrumImportTrioCheckOffline(t *testing.T) {
	dir := t.TempDir()
	xp := filepath.Join(dir, "x.csv")
	src := filepath.Join(dir, "s.csv")
	dst := filepath.Join(dir, "d.csv")
	if err := os.WriteFile(xp, []byte("dest,srce,levels\n5123,5121,1;2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("srce,levels,mnemonic\n5121,1;2,CAM1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("dest,level,mnemonic\n5123,1,MON1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cerebrumImportXpoint(context.Background(), []string{"--xpoint", xp, "--src", src, "--dst", dst, "--check"}); err != nil {
		t.Fatalf("trio --check offline: err = %v, want nil", err)
	}
}

// TestCerebrumImportTrioGuardRails pins the new flag validation: --csv/--xpoint
// exclusivity, nothing-to-import, malformed mnemonic CSV, apply needs host.
func TestCerebrumImportTrioGuardRails(t *testing.T) {
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
	_ = os.WriteFile(bad, []byte("srce,levels\n1,1\n"), 0o644) // no mnemonic col
	if err := cerebrumImportXpoint(context.Background(), []string{"--src", bad, "--check"}); err == nil {
		t.Error("malformed --src: err = nil, want error")
	}
	if err := cerebrumImportXpoint(context.Background(), []string{"--xpoint", xp}); err == nil {
		t.Error("apply without host: err = nil, want error")
	}
}

// TestCerebrumExportTrioFlags pins export's new flag validation offline:
// --out vs --out-dir exclusivity and the missing-host guard.
func TestCerebrumExportTrioFlags(t *testing.T) {
	if err := cerebrumExportXpoint(context.Background(), []string{"--out", "a.csv", "--out-dir", "d"}); err == nil ||
		!strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("--out + --out-dir: err = %v, want mutually-exclusive error", err)
	}
	if err := cerebrumExportXpoint(context.Background(), []string{"--out-dir", t.TempDir()}); err == nil {
		t.Error("export without host: err = nil, want error")
	}
}
