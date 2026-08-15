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
		rows, err := parseCerebrumMneCSV([]byte(csv), "srce", "t.csv")
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
		rows, err := parseCerebrumMneCSV([]byte("mnemonic,dest\nMON1,5123\n"), "dest", "t.csv")
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if len(rows) != 1 || rows[0].ID != "5123" || rows[0].Mnemonic != "MON1" {
			t.Errorf("rows = %+v", rows)
		}
	})

	t.Run("level key", func(t *testing.T) {
		rows, err := parseCerebrumMneCSV([]byte("level,mnemonic\n1,LVL-1\n2,LVL-2\n"), "level", "t.csv")
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
			if _, err := parseCerebrumMneCSV([]byte(tc.csv), tc.key, "t.csv"); err == nil {
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

// TestCerebrumImportSetCheckOffline pins that `import --check` with all four
// files is a pure offline dry-run covering routes + src/dst/level mnemonics.
func TestCerebrumImportSetCheckOffline(t *testing.T) {
	dir := t.TempDir()
	xp := filepath.Join(dir, "x.csv")
	src := filepath.Join(dir, "s.csv")
	dst := filepath.Join(dir, "d.csv")
	lvl := filepath.Join(dir, "l.csv")
	if err := os.WriteFile(xp, []byte("dest,srce,levels\n5123,5121,1;2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("srce,mnemonic\n5121,CAM1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("dest,mnemonic\n5123,MON1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lvl, []byte("level,mnemonic\n1,LVL-1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cerebrumImportXpoint(context.Background(), []string{"--xpoint", xp, "--src", src, "--dst", dst, "--levels", lvl, "--check"}); err != nil {
		t.Fatalf("set --check offline: err = %v, want nil", err)
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
