package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestCerebrumImportXpointCheckNeedsHost pins the ensure contract for the
// crosspoint leg: --check computes would_change against live state, so a
// host is required even for the dry-run.
func TestCerebrumImportXpointCheckNeedsHost(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.csv")
	if err := os.WriteFile(p, []byte("dest,srce,levels\n60,60,1;2;3\n61,70,2\n"), 0o644); err != nil {
		t.Fatalf("seed csv: %v", err)
	}
	if err := cerebrumImportXpoint(context.Background(), []string{"--csv", p, "--check"}); err == nil {
		t.Fatal("import --check without host: err = nil, want error (ensure reads live state)")
	}
}

// TestCerebrumImportXpointErrors pins the guard rails: --csv is mandatory, a
// malformed CSV is rejected, and apply-mode (no --check) needs a host.
func TestCerebrumImportXpointErrors(t *testing.T) {
	t.Run("missing --csv", func(t *testing.T) {
		if err := cerebrumImportXpoint(context.Background(), []string{"--check"}); err == nil {
			t.Fatal("err = nil, want error (missing --csv)")
		}
	})
	t.Run("malformed csv", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "bad.csv")
		_ = os.WriteFile(p, []byte("dest,srce\n1,2\n"), 0o644) // no level column
		if err := cerebrumImportXpoint(context.Background(), []string{"--csv", p, "--check"}); err == nil {
			t.Fatal("err = nil, want error (no level column)")
		}
	})
	t.Run("unknown --output format", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "x.csv")
		_ = os.WriteFile(p, []byte("dest,srce,levels\n60,60,1\n"), 0o644)
		err := cerebrumImportXpoint(context.Background(), []string{"--csv", p, "--output", "xml", "--check"})
		if err == nil || !strings.Contains(err.Error(), "expected text | json") {
			t.Fatalf("err = %v, want expected-text|json error", err)
		}
	})
	t.Run("apply needs host", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "x.csv")
		_ = os.WriteFile(p, []byte("dest,srce,levels\n60,60,1\n"), 0o644)
		if err := cerebrumImportXpoint(context.Background(), []string{"--csv", p}); err == nil {
			t.Fatal("err = nil, want error (apply mode requires host)")
		}
	})
}

// TestCerebrumImportInDir pins the export-mirror form: --in-dir DIR --prefix P
// resolves <P>-xpoint/-src/-dst/-level.csv, keeps only files that exist
// (partial scope), and an empty dir is "nothing to import".
func TestCerebrumImportInDir(t *testing.T) {
	t.Run("resolves existing files", func(t *testing.T) {
		dir := t.TempDir()
		// Only the src file exists — xpoint/dst/level stay out of scope.
		_ = os.WriteFile(filepath.Join(dir, "noc-src.csv"), []byte("srce,mnemonic\n1,CAM1\n"), 0o644)
		err := cerebrumImportXpoint(context.Background(), []string{"--in-dir", dir, "--prefix", "noc", "--check"})
		// Files resolved + parsed fine, so the failure must be the missing
		// host (ensure reads live state) — NOT "nothing to import".
		if err == nil || !strings.Contains(err.Error(), "host") {
			t.Fatalf("err = %v, want missing-host error", err)
		}
	})
	t.Run("empty dir = nothing to import", func(t *testing.T) {
		err := cerebrumImportXpoint(context.Background(), []string{"--in-dir", t.TempDir(), "--check"})
		if err == nil || !strings.Contains(err.Error(), "nothing to import") {
			t.Fatalf("err = %v, want nothing-to-import error", err)
		}
	})
	t.Run("explicit flag wins over --in-dir", func(t *testing.T) {
		dir := t.TempDir()
		_ = os.WriteFile(filepath.Join(dir, "cerebrum-xpoint.csv"), []byte("dest,srce,levels\n1,2,1\n"), 0o644)
		bad := filepath.Join(dir, "explicit.csv")
		_ = os.WriteFile(bad, []byte("dest,srce\n1,2\n"), 0o644) // malformed: no level col
		err := cerebrumImportXpoint(context.Background(), []string{"--in-dir", dir, "--xpoint", bad, "--check"})
		// The malformed EXPLICIT file must be the one parsed (and rejected).
		if err == nil || !strings.Contains(err.Error(), "explicit.csv") {
			t.Fatalf("err = %v, want parse error on explicit.csv", err)
		}
	})
}

// TestCerebrumExportRequiresHost pins that export needs a host argument (it
// errors before dialling when none is given).
func TestCerebrumExportRequiresHost(t *testing.T) {
	if err := cerebrumExportXpoint(context.Background(), []string{"--out", filepath.Join(t.TempDir(), "o.csv")}); err == nil {
		t.Fatal("export without host: err = nil, want error")
	}
}

// TestParseCerebrumXpoint pins the multi-level CSV parse: the `levels` column
// splits on ';' onto one row; the legacy `level` column still yields a
// single-level row; comments/blanks are skipped; missing columns and short rows
// error.
func TestParseCerebrumXpoint(t *testing.T) {
	t.Run("multi-level levels column", func(t *testing.T) {
		csv := "dest,srce,levels\n" +
			"# a take across three levels\n" +
			"60,60,1;2;3\n" +
			"61, 70 , 2\n" // breakaway on level 2, with stray spaces
		rows, err := parseCerebrumXpoint([]byte(csv), "t.csv")
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		want := []cerebrumXpointRow{
			{Dest: "60", Srce: "60", Levels: []string{"1", "2", "3"}},
			{Dest: "61", Srce: "70", Levels: []string{"2"}},
		}
		if !reflect.DeepEqual(rows, want) {
			t.Errorf("rows = %+v, want %+v", rows, want)
		}
	})

	t.Run("legacy single level column", func(t *testing.T) {
		rows, err := parseCerebrumXpoint([]byte("dest,srce,level\n5,9,1\n"), "t.csv")
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if len(rows) != 1 || rows[0].Dest != "5" || rows[0].Srce != "9" ||
			!reflect.DeepEqual(rows[0].Levels, []string{"1"}) {
			t.Errorf("rows = %+v", rows)
		}
	})

	t.Run("column order independent + CRLF", func(t *testing.T) {
		rows, err := parseCerebrumXpoint([]byte("levels,dest,srce\r\n1;2,7,8\r\n"), "t.csv")
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if len(rows) != 1 || rows[0].Dest != "7" || rows[0].Srce != "8" ||
			!reflect.DeepEqual(rows[0].Levels, []string{"1", "2"}) {
			t.Errorf("rows = %+v", rows)
		}
	})

	for _, tc := range []struct{ name, csv string }{
		{"missing dest", "srce,levels\n1,2\n"},
		{"missing level col", "dest,srce\n1,2\n"},
		{"empty", "   \n # only a comment\n"},
		{"short row", "dest,srce,levels\n60,60\n"},
		{"blank fields", "dest,srce,levels\n,,\n"},
		{"no levels in cell", "dest,srce,levels\n60,60,\n"},
	} {
		t.Run("error: "+tc.name, func(t *testing.T) {
			if _, err := parseCerebrumXpoint([]byte(tc.csv), "t.csv"); err == nil {
				t.Errorf("parse %q: err = nil, want error", tc.csv)
			}
		})
	}
}

// TestExpandCerebrumXpoint pins that a multi-level row flattens to one routeSpec
// per level, row-then-level order (the ROUTE-action stream import sends).
func TestExpandCerebrumXpoint(t *testing.T) {
	got := expandCerebrumXpoint([]cerebrumXpointRow{
		{Dest: "60", Srce: "60", Levels: []string{"1", "2", "3"}},
		{Dest: "61", Srce: "70", Levels: []string{"2"}},
	})
	want := []routeSpec{
		{Dest: "60", Srce: "60", Level: "1"},
		{Dest: "60", Srce: "60", Level: "2"},
		{Dest: "60", Srce: "60", Level: "3"},
		{Dest: "61", Srce: "70", Level: "2"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("expand = %+v, want %+v", got, want)
	}
}

// TestCollapseCerebrumRoutes pins the export inverse: levels from the same
// source coalesce onto one row; a breakaway source splits; output is
// numeric-ordered and deduped.
func TestCollapseCerebrumRoutes(t *testing.T) {
	// Deliberately unordered + a duplicate + a breakaway (dest 61 level 2 from a
	// different source than its level 1).
	routes := []routeSpec{
		{Dest: "61", Srce: "70", Level: "2"},
		{Dest: "60", Srce: "60", Level: "10"},
		{Dest: "60", Srce: "60", Level: "2"},
		{Dest: "61", Srce: "61", Level: "1"},
		{Dest: "60", Srce: "60", Level: "1"},
		{Dest: "60", Srce: "60", Level: "2"}, // dup
	}
	got := collapseCerebrumRoutes(routes)
	want := []cerebrumXpointRow{
		{Dest: "60", Srce: "60", Levels: []string{"1", "2", "10"}}, // numeric: 2 < 10
		{Dest: "61", Srce: "61", Levels: []string{"1"}},
		{Dest: "61", Srce: "70", Levels: []string{"2"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("collapse = %+v, want %+v", got, want)
	}
}

// TestCerebrumXpointRoundTrip pins that expand -> collapse -> format -> parse is
// stable: a set of routes exported and re-imported yields the same flat route
// set.
func TestCerebrumXpointRoundTrip(t *testing.T) {
	orig := []routeSpec{
		{Dest: "60", Srce: "60", Level: "1"},
		{Dest: "60", Srce: "60", Level: "2"},
		{Dest: "61", Srce: "70", Level: "2"},
	}
	csv := formatCerebrumXpointCSV(collapseCerebrumRoutes(orig))
	rows, err := parseCerebrumXpoint([]byte(csv), "roundtrip")
	if err != nil {
		t.Fatalf("reparse exported CSV: %v\n%s", err, csv)
	}
	got := expandCerebrumXpoint(rows)
	if !reflect.DeepEqual(got, orig) {
		t.Errorf("round-trip mismatch:\ngot  %+v\nwant %+v\ncsv:\n%s", got, orig, csv)
	}
}

// TestFormatCerebrumXpointCSV pins the exact serialized shape (header +
// ';'-joined levels) so the format stays stable for operators + tooling.
func TestFormatCerebrumXpointCSV(t *testing.T) {
	got := formatCerebrumXpointCSV([]cerebrumXpointRow{{Dest: "60", Srce: "60", Levels: []string{"1", "2", "3"}}})
	want := "dest,srce,levels\n60,60,1;2;3\n"
	if got != want {
		t.Errorf("format = %q, want %q", got, want)
	}
}

// TestCmpCerebrumID pins numeric-aware ID comparison (so "2" < "10"), with a
// lexicographic fallback when a value is non-numeric.
func TestCmpCerebrumID(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"2", "10", -1},
		{"10", "2", 1},
		{"5", "5", 0},
		{"abc", "abd", -1},
		{"7", "x", -1}, // "7" < "x" lexicographically
	}
	for _, tc := range cases {
		if got := cmpCerebrumID(tc.a, tc.b); got != tc.want {
			t.Errorf("cmpCerebrumID(%q,%q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}
