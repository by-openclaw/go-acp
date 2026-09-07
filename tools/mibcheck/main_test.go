package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The defect this tool was written for, reduced to its essentials: one wire
// value carrying two names, and one name carrying two values.
func TestCheckEnumsFindsTheEricssonDefect(t *testing.T) {
	src := `
inS2StatusModulationType OBJECT-TYPE
    SYNTAX    INTEGER { modBpsk(1), modQpsk(2), mod8psk(3), mod16qam(4), modAuto(5), mod16sqam(5), modAuto(7) }
    ACCESS    read-only
`
	got := checkEnums("TT1260-MIB.mib", stripComments(src))
	if len(got) != 2 {
		t.Fatalf("got %d findings, want 2:\n%v", len(got), got)
	}

	var sawValue, sawName bool
	for _, f := range got {
		if f.Line != 3 {
			t.Errorf("finding on line %d, want the SYNTAX line 3: %v", f.Line, f)
		}
		switch f.Rule {
		case "duplicate-value":
			sawValue = true
			if !strings.Contains(f.Msg, "modAuto") || !strings.Contains(f.Msg, "mod16sqam") {
				t.Errorf("duplicate-value must name both members: %q", f.Msg)
			}
		case "duplicate-name":
			sawName = true
			if !strings.Contains(f.Msg, "5") || !strings.Contains(f.Msg, "7") {
				t.Errorf("duplicate-name must name both values: %q", f.Msg)
			}
		}
	}
	if !sawValue || !sawName {
		t.Errorf("both rules must fire on this enum, got %v", got)
	}
}

// The RX8200 case: the VALUE is right and the LABEL is wrong, so only the
// name rule fires. A checker that only looked for duplicate values would
// miss it entirely.
func TestCheckEnumsFindsADuplicateLabelAlone(t *testing.T) {
	src := `
    mpegAlert OBJECT-TYPE
        SYNTAX Integer32 {
            NO_MPEG_ALERTS_ACTIVE(0),
            SYNC_LOSS(1),
            OUT_OF_REGULATION(2),
            SYNC_LOSS-OUT_OF_REGULATION(3),
            RS_THRESHOLD(4),
            SYNC_LOSS-RS_THRESHOLD(5),
            SYNC_LOSS-OUT_OF_REGULATION(6) }
`
	got := checkEnums("x.mib", stripComments(src))
	if len(got) != 1 || got[0].Rule != "duplicate-name" {
		t.Fatalf("want exactly one duplicate-name finding, got %v", got)
	}
}

// A well-formed enumeration produces nothing. Without this the tool could
// "pass" by reporting everything.
func TestCheckEnumsAcceptsAWellFormedEnum(t *testing.T) {
	src := `SYNTAX INTEGER { off(0), on(1), auto(2) }`
	if got := checkEnums("x.mib", stripComments(src)); len(got) != 0 {
		t.Errorf("a clean enum must produce no findings, got %v", got)
	}
}

// A commented-out enumeration is not source. Linting one would report
// defects nobody can fix, in text the compiler never sees.
func TestCommentedEnumIsNotLinted(t *testing.T) {
	src := `-- SYNTAX INTEGER { a(1), b(1) }
SYNTAX INTEGER { off(0), on(1) }`
	if got := checkEnums("x.mib", stripComments(src)); len(got) != 0 {
		t.Errorf("commented text must be ignored, got %v", got)
	}
}

// stripComments must not move any line, or every reported line number after
// the first comment would be wrong.
func TestStripCommentsPreservesLineNumbering(t *testing.T) {
	src := "a -- one\nb\n-- two\nc"
	got := stripComments(src)
	if strings.Count(got, "\n") != strings.Count(src, "\n") {
		t.Fatalf("line count changed: %q", got)
	}
	if lines := strings.Split(got, "\n"); lines[3] != "c" {
		t.Errorf("line 4 = %q, want %q", lines[3], "c")
	}
}

// "FROM" appears in prose too. Reading it outside an IMPORTS block would
// invent dependencies and make every set look incomplete.
func TestImportedModulesReadsOnlyTheImportsBlock(t *testing.T) {
	src := `
FOO-MIB DEFINITIONS ::= BEGIN
IMPORTS
    OBJECT-TYPE FROM SNMPv2-SMI
    Uint8       FROM ETV-Types-TC;

bar OBJECT-TYPE
    DESCRIPTION "the value read FROM RFC1213-MIB is not an import"
END`
	got := importedModules(src)
	if len(got) != 2 {
		t.Fatalf("got %v, want exactly the two real imports", got)
	}
	for _, g := range got {
		if g == "RFC1213-MIB" {
			t.Error("a FROM inside a DESCRIPTION was treated as an import")
		}
	}
	if defs := definedModules(src); len(defs) != 1 || defs[0] != "FOO-MIB" {
		t.Errorf("definedModules = %v, want [FOO-MIB]", defs)
	}
}

// The whole point of the import rule: a set that compiles here because the
// module happens to be installed, and fails on a colleague's machine.
func TestMissingReportsImportedButUndefined(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.mib", `A-MIB DEFINITIONS ::= BEGIN
IMPORTS OBJECT-TYPE FROM SNMPv2-SMI; END`)
	write(t, dir, "b.mib", `B-MIB DEFINITIONS ::= BEGIN
IMPORTS Uint8 FROM A-MIB; END`)

	set, err := scan([]string{dir})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	missing := set.missing()
	if len(missing) != 1 || missing[0] != "SNMPv2-SMI" {
		t.Fatalf("missing = %v, want [SNMPv2-SMI] — A-MIB is defined here", missing)
	}
	if set.files != 2 {
		t.Errorf("scanned %d files, want 2", set.files)
	}
}

// -fetch appends its output directory to the scan list, and that directory
// may sit inside one already listed. Counting a file twice would report
// every one of its enums twice.
func TestScanCountsEachFileOnce(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "standard")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, sub, "S.mib", `S-MIB DEFINITIONS ::= BEGIN END`)

	set, err := scan([]string{dir, sub})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if set.files != 1 {
		t.Errorf("scanned %d files, want 1 — the overlap must not double-count", set.files)
	}
}

// The lesson from observium: a URL that looks like a download can return a
// rendered web page, and writing it would be worse than fetching nothing,
// because the next run stops reporting the module as missing.
func TestValidateRejectsAWebPage(t *testing.T) {
	page := []byte("<!DOCTYPE html>\n<html lang=\"en\"><title>SNMPv2-SMI</title></html>")
	err := validate("SNMPv2-SMI", page)
	if err == nil {
		t.Fatal("a web page must be rejected")
	}
	if !strings.Contains(err.Error(), "web page") {
		t.Errorf("error should say what it is: %v", err)
	}
}

func TestValidateRejectsTheWrongModule(t *testing.T) {
	body := []byte("SNMPv2-TC DEFINITIONS ::= BEGIN\nEND")
	err := validate("SNMPv2-SMI", body)
	if err == nil {
		t.Fatal("a different module must be rejected")
	}
	if !strings.Contains(err.Error(), "SNMPv2-TC") {
		t.Errorf("error should name what arrived: %v", err)
	}
}

func TestValidateRejectsSomethingWithNoModule(t *testing.T) {
	if err := validate("SNMPv2-SMI", []byte("not a mib at all")); err == nil {
		t.Fatal("content declaring no module must be rejected")
	}
}

func TestValidateAcceptsTheRealThing(t *testing.T) {
	body := []byte("-- a comment\nSNMPv2-SMI DEFINITIONS ::= BEGIN\nEND")
	if err := validate("SNMPv2-SMI", body); err != nil {
		t.Errorf("valid source rejected: %v", err)
	}
}

// The IETF modules ship with no extension at all, so an extension allow-list
// that forgot that case would silently skip exactly the files the import
// rule needs.
func TestMibExtAcceptsExtensionlessModules(t *testing.T) {
	for _, name := range []string{"SNMPv2-SMI", "Base.mib", "RFC-1212.my", "IF-MIB.txt"} {
		if !mibExt(name) {
			t.Errorf("%s should be treated as MIB source", name)
		}
	}
	for _, name := range []string{"notes.pdf", "logo.png"} {
		if mibExt(name) {
			t.Errorf("%s should not be treated as MIB source", name)
		}
	}
}

func TestFindingFormat(t *testing.T) {
	withLine := Finding{"a.mib", 12, "duplicate-value", "boom"}
	if got := withLine.String(); got != "a.mib:12: duplicate-value: boom" {
		t.Errorf("got %q", got)
	}
	// A missing import belongs to a set, not to a line.
	noLine := Finding{"a.mib", 0, "missing-import", "boom"}
	if got := noLine.String(); got != "a.mib: missing-import: boom" {
		t.Errorf("got %q", got)
	}
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
