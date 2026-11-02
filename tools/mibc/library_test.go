package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dhs/internal/snmp/codec"
	"dhs/internal/snmp/mib"
)

// libraryTree is two models of one maker defining the same module, a shared
// module a sibling folder holds, and another maker's module of that name.
func libraryTree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("standard/SNMPv2-SMI", `SNMPv2-SMI DEFINITIONS ::= BEGIN
  enterprises OBJECT IDENTIFIER ::= { iso 3 6 1 4 1 }
END`)
	for model, leaf := range map[string]string{"1986": "poLabel", "7113": "poDWDMChannelNumber"} {
		write("acme/rf-switch/"+model+"/switch.mib", `DEV-SWITCH-MIB DEFINITIONS ::= BEGIN
  IMPORTS enterprises FROM SNMPv2-SMI  acmeRoot FROM ACME-ROOT-MIB;
  `+leaf+` OBJECT IDENTIFIER ::= { acmeRoot 29 }
END`)
	}
	write("acme/rf-switch/root/root.mib", `ACME-ROOT-MIB DEFINITIONS ::= BEGIN
  IMPORTS enterprises FROM SNMPv2-SMI;
  acmeRoot OBJECT IDENTIFIER ::= { enterprises 21035 }
END`)
	write("other/ird/x1/root.mib", `ACME-ROOT-MIB DEFINITIONS ::= BEGIN
  IMPORTS enterprises FROM SNMPv2-SMI;
  acmeRoot OBJECT IDENTIFIER ::= { enterprises 9 }
END`)
	write("assets/presentation/old/readme.mib", "must never be read: broken \"")
	write(".git/objects/aa/pack.mib", "must never be read: broken \"")
	write("extra/DHS.mib", `DHS-MIB DEFINITIONS ::= BEGIN
  IMPORTS enterprises FROM SNMPv2-SMI;
  dhsRoot OBJECT IDENTIFIER ::= { enterprises 54981 }
END`)
	return dir
}

func oidOf(t *testing.T, s string) codec.OID {
	t.Helper()
	o, err := codec.ParseOID(s)
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func loadTable(t *testing.T, dir, name string) *mib.Table {
	t.Helper()
	f, err := os.Open(filepath.Join(dir, name+".tsv.gz"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	tb, err := mib.ParseTable(f)
	if err != nil {
		t.Fatal(err)
	}
	return tb
}

// One device, one table: a model never sees what another defines at the
// same OID, and an import stays inside its manufacturer.
func TestLibraryWritesOneTablePerModel(t *testing.T) {
	dir := libraryTree(t)
	out := t.TempDir()
	code, stdout, stderr := execMibc(t, "-library", dir, "-out", out,
		"-set", "standard="+filepath.Join(dir, "extra"))
	if code != 0 {
		t.Fatal(stderr)
	}
	if !strings.Contains(stdout, "5 tables") {
		t.Errorf("stdout = %q", stdout)
	}
	for model, want := range map[string]string{"1986": "poLabel", "7113": "poDWDMChannelNumber"} {
		tb := loadTable(t, out, "acme-"+model)
		if got := tb.Name(oidOf(t, "1.3.6.1.4.1.21035.29")); got != want {
			t.Errorf("acme-%s names .29 %q, want %q", model, got, want)
		}
	}
	if got := loadTable(t, out, "acme-1986").Name(oidOf(t, "1.3.6.1.4.1.9")); got == "acmeRoot" {
		t.Error("another manufacturer's module reached the table")
	}
	if got := loadTable(t, out, "standard").Name(oidOf(t, "1.3.6.1.4.1.54981")); got != "dhsRoot" {
		t.Errorf("-set table names %q", got)
	}
}

// A stray file at model depth is not a model.
func TestLibrarySkipsAFileWhereAModelWouldBe(t *testing.T) {
	dir := libraryTree(t)
	stray := filepath.Join(dir, "acme", "rf-switch", "notes.mib")
	if err := os.WriteFile(stray, []byte("not a model"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := execMibc(t, "-library", dir, "-out", t.TempDir())
	if code != 0 || !strings.Contains(stdout, "4 tables") {
		t.Errorf("code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

func TestLibraryUsage(t *testing.T) {
	if code, _, stderr := execMibc(t, "-out", t.TempDir()); code != 2 || !strings.Contains(stderr, "-library DIR") {
		t.Errorf("code %d, stderr %q", code, stderr)
	}
}

func TestLibraryRefusals(t *testing.T) {
	dir := libraryTree(t)
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	broken := libraryTree(t)
	if err := os.WriteFile(filepath.Join(broken, "standard", "BROKEN"), []byte("X DEFINITIONS ::= BEGIN \""), 0o600); err != nil {
		t.Fatal(err)
	}
	full := t.TempDir()
	if err := os.Mkdir(filepath.Join(full, "acme-1986.tsv.gz"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		argv []string
		want string
	}{
		{"a malformed set", []string{"-library", dir, "-out", t.TempDir(), "-set", "nope"}, "NAME=DIR"},
		{"no library", []string{"-library", filepath.Join(dir, "absent"), "-out", t.TempDir()}, "absent"},
		{"a library no glob can read", []string{"-library", dir + "[", "-out", t.TempDir()}, "syntax error"},
		{"a set with no folder", []string{"-library", dir, "-out", t.TempDir(), "-set", "x=" + filepath.Join(dir, "absent")}, "absent"},
		{"source that does not parse", []string{"-library", broken, "-out", t.TempDir()}, "BROKEN"},
		{"an output that is a file", []string{"-library", dir, "-out", file}, "file"},
		{"a table that cannot be written", []string{"-library", dir, "-out", full}, "acme-1986"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, _, stderr := execMibc(t, tc.argv...)
			if code != 2 || !strings.Contains(stderr, tc.want) {
				t.Errorf("code %d, stderr %q", code, stderr)
			}
		})
	}
}

func TestLibraryStopsOnABrokenPatchFile(t *testing.T) {
	prev := patches
	patches = "not a patch"
	t.Cleanup(func() { patches = prev })
	if code, _, _ := execMibc(t, "-library", libraryTree(t), "-out", t.TempDir()); code != 2 {
		t.Errorf("code = %d", code)
	}
}

// A file that vanishes between the index and the compile stops the run.
func TestLibraryStopsOnASecondReadFailure(t *testing.T) {
	dir := libraryTree(t)
	calls := 0
	prev := readFile
	readFile = func(p string) ([]byte, error) {
		calls++
		if calls > 8 {
			return nil, errors.New("gone")
		}
		return prev(p)
	}
	t.Cleanup(func() { readFile = prev })
	code, _, stderr := execMibc(t, "-library", dir, "-out", t.TempDir())
	if code != 2 || !strings.Contains(stderr, "gone") {
		t.Errorf("code %d, stderr %q", code, stderr)
	}
}

// A table this run would not compile stops it, before anything is written:
// a recompile must not keep last year's file, or lose it, in silence.
func TestARecompileCannotLeaveATableBehind(t *testing.T) {
	dir := libraryTree(t)
	out := t.TempDir()
	stale := filepath.Join(out, "arista-eos.tsv.gz")
	if err := os.WriteFile(stale, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := execMibc(t, "-library", dir, "-out", out)
	if code != 2 || !strings.Contains(stderr, "arista-eos not compiled") {
		t.Fatalf("%d %q", code, stderr)
	}
	if entries, _ := os.ReadDir(out); len(entries) != 1 {
		t.Errorf("a refused run wrote %d files", len(entries))
	}
	// With a source it compiles.
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "A.mib"), []byte("A DEFINITIONS ::= BEGIN END"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := execMibc(t, "-library", dir, "-out", out, "-set", "arista-eos="+source); code != 0 {
		t.Fatalf("%d %q", code, stderr)
	}
	if b, _ := os.ReadFile(stale); string(b) == "old" {
		t.Error("the table was not recompiled")
	}
}

func TestABadOutDirIsRefused(t *testing.T) {
	if err := refuseStale("[", nil); err == nil {
		t.Error("a pattern that cannot match must be reported")
	}
}
