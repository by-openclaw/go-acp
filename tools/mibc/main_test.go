package main

// The tool's contract: the table on -out, a summary on stdout, findings on
// stderr, exit 0 or 2, and bytes that depend only on the input.

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dhs/internal/snmp/mib"
	"dhs/internal/snmp/smi"
)

// tree writes a small MIB set: a base, a vendor module, and the kinds of
// file collect must skip.
func tree(t *testing.T) string {
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
	write("ird/TT1260-MIB.mib", `ETV-TT1260-MIB DEFINITIONS ::= BEGIN
  IMPORTS enterprises FROM SNMPv2-SMI;
  tt1260 OBJECT IDENTIFIER ::= { enterprises 1773 1 3 200 }
  controlMode OBJECT-TYPE
      SYNTAX INTEGER { fp(1), serial(2), ncp(3), snmp(4), web(5) }
      ACCESS read-write STATUS mandatory DESCRIPTION "who may drive it"
      ::= { tt1260 1 11 }
END`)
	// Two copies of one module: the pin decides.
	write("ird/a/IP-MIB.mib", "IP-MIB DEFINITIONS ::= BEGIN END")
	write("ird/RX1290/IP-MIB.mib", "IP-MIB DEFINITIONS ::= BEGIN END")
	write("ird/Copy of TT1260-MIB.mib", "must be skipped: broken \"")
	write("ird/README.txt", "not a module")
	write("ird/notes.xml", "<xml/>")
	return dir
}

func execMibc(t *testing.T, argv ...string) (int, string, string) {
	t.Helper()
	var out, errs bytes.Buffer
	code := run(argv, &out, &errs)
	return code, out.String(), errs.String()
}

func readTable(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	sc := bufio.NewScanner(zr)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines
}

func TestItCompilesATree(t *testing.T) {
	dir := tree(t)
	out := filepath.Join(t.TempDir(), "table.tsv.gz")

	code, stdout, stderr := execMibc(t, "-out", out, dir)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "wrote "+out) || !strings.Contains(stdout, "4 files") {
		t.Errorf("summary = %q", stdout)
	}
	// The findings are counted, not listed, without -v.
	if !strings.Contains(stderr, "1 duplicate-module (run with -v") {
		t.Errorf("stderr = %q", stderr)
	}

	lines := readTable(t, out)
	if lines[0] != mib.TableHeader {
		t.Fatalf("header = %q", lines[0])
	}

	// What this tool writes is what internal/snmp/mib reads: the contract
	// is tested end to end, not by two tests agreeing with themselves.
	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	table, err := mib.ParseTable(f)
	if err != nil {
		t.Fatalf("the reader refused the table: %v", err)
	}
	if got, _ := table.Resolve("controlMode.0"); got.String() != "1.3.6.1.4.1.1773.1.3.200.1.11.0" {
		t.Errorf("controlMode.0 read back as %s", got)
	}
	var row string
	for _, l := range lines {
		if strings.HasPrefix(l, "1.3.6.1.4.1.1773.1.3.200.1.11\t") {
			row = l
		}
	}
	want := "1.3.6.1.4.1.1773.1.3.200.1.11\tcontrolMode\tETV-TT1260-MIB\tobject-type\tINTEGER\t\tread-write\tfp=1;serial=2;ncp=3;snmp=4;web=5\t"
	if row != want {
		t.Errorf("controlMode row\n got %q\nwant %q", row, want)
	}
}

// The same roots produce the same bytes, so a regenerated table that
// differs is a real change and not a timestamp.
func TestTheTableIsDeterministic(t *testing.T) {
	dir := tree(t)
	a := filepath.Join(t.TempDir(), "a.gz")
	b := filepath.Join(t.TempDir(), "b.gz")
	if code, _, e := execMibc(t, "-out", a, dir); code != 0 {
		t.Fatal(e)
	}
	if code, _, e := execMibc(t, "-out", b, dir); code != 0 {
		t.Fatal(e)
	}
	ab, _ := os.ReadFile(a)
	bb, _ := os.ReadFile(b)
	if !bytes.Equal(ab, bb) {
		t.Error("two runs over one tree produced different bytes")
	}
}

// -v lists each finding, including which copy of a module won and why.
func TestVerboseListsFindings(t *testing.T) {
	dir := tree(t)
	code, _, stderr := execMibc(t, "-v", "-out", filepath.Join(t.TempDir(), "t.gz"), dir)
	if code != 0 {
		t.Fatal(stderr)
	}
	if !strings.Contains(stderr, "duplicate-module") || !strings.Contains(stderr, "(pinned)") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestUsageAndRefusals(t *testing.T) {
	dir := tree(t)
	for _, tc := range []struct {
		name string
		argv []string
		want string
	}{
		{"no -out", []string{dir}, "usage"},
		{"no roots", []string{"-out", "x.gz"}, "usage"},
		{"an unknown flag", []string{"-nonsense"}, "nonsense"},
		{"a malformed pin", []string{"-out", "x.gz", "-pin", "nope", dir}, "MODULE=PATH-SUBSTRING"},
		{"a root that is not there", []string{"-out", "x.gz", filepath.Join(dir, "absent")}, "absent"},
		{"an -out that cannot be written", []string{"-out", filepath.Join(dir, "no", "such", "x.gz"), dir}, "mibc:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, _, stderr := execMibc(t, tc.argv...)
			if code != 2 || !strings.Contains(stderr, tc.want) {
				t.Errorf("= %d %q, want exit 2 and %q", code, stderr, tc.want)
			}
		})
	}
}

// A file that cannot be tokenised stops the run: it is source the
// compiler cannot read at all, not a finding it can work around.
func TestAnUnreadableModuleStopsTheRun(t *testing.T) {
	dir := tree(t)
	if err := os.WriteFile(filepath.Join(dir, "bad.mib"),
		[]byte(`X DEFINITIONS ::= BEGIN "never closed`), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := execMibc(t, "-out", filepath.Join(t.TempDir(), "t.gz"), dir); code != 2 ||
		!strings.Contains(stderr, "unterminated") {
		t.Errorf("= %d %q", code, stderr)
	}

	prev := readFile
	readFile = func(string) ([]byte, error) { return nil, errors.New("gone") }
	t.Cleanup(func() { readFile = prev })
	if code, _, stderr := execMibc(t, "-out", filepath.Join(t.TempDir(), "t.gz"), tree(t)); code != 2 ||
		!strings.Contains(stderr, "gone") {
		t.Errorf("= %d %q", code, stderr)
	}
}

// The embedded patch list is applied on every run, and a malformed one
// stops the run rather than being ignored.
func TestPatchesAreApplied(t *testing.T) {
	prev := patches
	t.Cleanup(func() { patches = prev })

	patches = "ETV-TT1260-MIB missingNode tt1260 99 -- test evidence\n"
	code, _, stderr := execMibc(t, "-v", "-out", filepath.Join(t.TempDir(), "t.gz"), tree(t))
	if code != 0 || !strings.Contains(stderr, "patched") {
		t.Errorf("= %d %q", code, stderr)
	}

	patches = "not a patch line"
	if code, _, stderr := execMibc(t, "-out", filepath.Join(t.TempDir(), "t.gz"), tree(t)); code != 2 ||
		!strings.Contains(stderr, "no evidence") {
		t.Errorf("= %d %q", code, stderr)
	}
}

// The shipped patch list parses, so a bad edit to it fails here rather
// than at the next regeneration.
func TestTheShippedPatchesParse(t *testing.T) {
	ps, err := smi.ParsePatches(patches)
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) == 0 {
		t.Error("the shipped patch list is empty")
	}
}

func TestHelpers(t *testing.T) {
	if got := splitList(" a, ,b "); strings.Join(got, "|") != "a|b" {
		t.Errorf("splitList = %v", got)
	}
	if m, err := parsePins("A=x, B=y"); err != nil || m["A"] != "x" || m["B"] != "y" {
		t.Errorf("parsePins = %v %v", m, err)
	}
	for _, bad := range []string{"=x", "A=", "A"} {
		if _, err := parsePins(bad); err == nil {
			t.Errorf("parsePins(%q) must fail", bad)
		}
	}
	for _, tc := range []struct {
		name string
		want bool
	}{
		{"X.mib", true}, {"X.MY", true}, {"X.txt", true}, {"SNMPv2-SMI", true},
		{"README.txt", false}, {"readme", false}, {"X.xml", false}, {"Copy of X.mib", false},
	} {
		if got := isMIB(tc.name, []string{"Copy of "}); got != tc.want {
			t.Errorf("isMIB(%q) = %v", tc.name, got)
		}
	}
}

// failAfter accepts n bytes and then refuses, so each stage of writing the
// table can be made to fail.
type failAfter struct{ n int }

func (f *failAfter) Write(p []byte) (int, error) {
	if f.n <= 0 {
		return 0, errors.New("disk full")
	}
	if len(p) > f.n {
		k := f.n
		f.n = 0
		return k, errors.New("disk full")
	}
	f.n -= len(p)
	return len(p), nil
}

func TestEncodeTableRefusals(t *testing.T) {
	obj := smi.Object{OID: []uint32{1, 3}, Name: "x", Enums: []smi.Enum{{Name: "a", Value: 1}}, Index: []string{"i"}}

	// The header itself.
	if err := encodeTable(&failAfter{0}, nil); err == nil {
		t.Error("a writer that refuses the header must be reported")
	}
	// A row, once enough of them force the compressor to flush.
	many := make([]smi.Object, 20000)
	for i := range many {
		many[i] = obj
		many[i].OID = []uint32{1, 3, uint32(i)}
	}
	if err := encodeTable(&failAfter{64}, many); err == nil {
		t.Error("a writer that refuses mid-table must be reported")
	}
	// The final flush.
	if err := encodeTable(&failAfter{16}, []smi.Object{obj}); err == nil {
		t.Error("a writer that refuses the final flush must be reported")
	}
}

func TestMainWiring(t *testing.T) {
	out := filepath.Join(t.TempDir(), "t.gz")
	prevArgs, prevExit := os.Args, osExit
	var got int
	os.Args = []string{"mibc", "-out", out, tree(t)}
	osExit = func(code int) { got = code }
	t.Cleanup(func() { os.Args, osExit = prevArgs, prevExit })

	main()

	if got != 0 {
		t.Errorf("main exited %d", got)
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("no table written: %v", err)
	}
}
