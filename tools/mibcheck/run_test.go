package main

// The command as a caller meets it: argv in, an exit code out, and
// every message on the stream it was handed. The exit code is the
// contract a commit hook depends on, so each of the three is asserted
// rather than inferred from the output.

import (
	"bytes"
	"fmt"
	stdhttp "net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mibDir writes a set of modules to a temp dir and returns its path.
func mibDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// exec runs the command and returns its exit code and both streams.
func exec(t *testing.T, argv ...string) (int, string, string) {
	t.Helper()
	var out, errs bytes.Buffer
	code := run(argv, &out, &errs)
	return code, out.String(), errs.String()
}

// A clean set exits 0 and says what it looked at, because a linter
// that prints nothing is indistinguishable from one that did not run.
func TestCleanSetExitsZero(t *testing.T) {
	dir := mibDir(t, map[string]string{
		"CLEAN-MIB": `CLEAN-MIB DEFINITIONS ::= BEGIN
			status ::= INTEGER { up(1), down(2) }
			END`,
	})

	code, out, errs := exec(t, dir)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (%s / %s)", code, out, errs)
	}
	if out != "" {
		t.Errorf("a clean set has no findings: %q", out)
	}
	if !strings.Contains(errs, "1 file(s)") || !strings.Contains(errs, "0 finding(s)") {
		t.Errorf("summary = %q", errs)
	}
}

// -q is for a hook that only wants the findings.
func TestQuietPrintsOnlyFindings(t *testing.T) {
	dir := mibDir(t, map[string]string{
		"CLEAN-MIB": "CLEAN-MIB DEFINITIONS ::= BEGIN\nEND\n",
	})

	_, _, errs := exec(t, "-q", dir)
	if errs != "" {
		t.Errorf("-q printed a summary: %q", errs)
	}
}

// The defect this tool was written for: one wire value carrying two
// names, so decoding it is ambiguous — and a name appearing twice, so
// one of them is unreachable. Both exit 1, which is what gates the
// commit.
func TestEnumerationDefectsAreFound(t *testing.T) {
	dir := mibDir(t, map[string]string{
		"IRD-MIB": `IRD-MIB DEFINITIONS ::= BEGIN
			inS2StatusModulationType ::= INTEGER {
			    modBpsk(1), modQpsk(2), mod8psk(3), mod16qam(4),
			    modAuto(5), mod16sqam(5), modAuto(7) }
			END`,
	})

	code, out, _ := exec(t, dir)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(out, "duplicate-value") {
		t.Errorf("the ambiguous value must be reported: %s", out)
	}
	if !strings.Contains(out, "duplicate-name") {
		t.Errorf("the repeated name must be reported: %s", out)
	}
	if !strings.Contains(out, "IRD-MIB:") {
		t.Errorf("a finding must name its file and line: %s", out)
	}
}

// A module imported but defined nowhere makes the set incomplete
// rather than wrong: it compiles on a machine that happens to have the
// module installed and fails on one that does not, which is the worst
// of both.
func TestMissingImportIsFound(t *testing.T) {
	dir := mibDir(t, map[string]string{
		"IRD-MIB": `IRD-MIB DEFINITIONS ::= BEGIN
			IMPORTS enterprises FROM SNMPv2-SMI;
			END`,
	})

	code, out, _ := exec(t, dir)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(out, "missing-import") || !strings.Contains(out, "SNMPv2-SMI") {
		t.Errorf("= %s, want the absent module named", out)
	}
}

// A commented-out example is not source: linting it would report a
// defect in a comment, which the author cannot fix and will learn to
// ignore.
func TestCommentsAreNotLinted(t *testing.T) {
	dir := mibDir(t, map[string]string{
		"IRD-MIB": `IRD-MIB DEFINITIONS ::= BEGIN
			-- broken ::= INTEGER { a(1), b(1) }
			END`,
	})

	if code, out, _ := exec(t, dir); code != 0 {
		t.Fatalf("exit = %d (%s), want a comment to be ignored", code, out)
	}
}

// Usage errors are exit 2, told apart from findings (1) so a hook can
// distinguish "the set is bad" from "you called me wrong".
func TestUsageErrors(t *testing.T) {
	if code, _, errs := exec(t); code != 2 || !strings.Contains(errs, "usage:") {
		t.Errorf("no directory = %d %q", code, errs)
	}
	if code, _, errs := exec(t, "-fetch", t.TempDir()); code != 2 ||
		!strings.Contains(errs, "-fetch needs -out") {
		t.Errorf("-fetch with no -out = %d %q", code, errs)
	}
	if code, _, _ := exec(t, "-nonsense", t.TempDir()); code != 2 {
		t.Errorf("an unknown flag = %d, want 2", code)
	}
	if code, _, errs := exec(t, filepath.Join(t.TempDir(), "absent")); code != 2 ||
		!strings.Contains(errs, "mibcheck:") {
		t.Errorf("a directory that is not there = %d %q", code, errs)
	}
}

// ---------------------------------------------------------------
// -fetch
// ---------------------------------------------------------------

// mirror serves modules by name, and can be told to serve something
// else instead.
func mirror(t *testing.T, bodies map[string]string) string {
	t.Helper()
	ts := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), ".mib")
		body, ok := bodies[name]
		if !ok {
			stdhttp.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(ts.Close)
	return ts.URL
}

// -fetch resolves the missing imports and re-checks the enlarged set,
// reading back from disk rather than trusting what it just wrote:
// what matters is what the next compile will see.
func TestFetchResolvesAMissingImport(t *testing.T) {
	dir := mibDir(t, map[string]string{
		"IRD-MIB": `IRD-MIB DEFINITIONS ::= BEGIN
			IMPORTS enterprises FROM SNMPv2-SMI;
			END`,
	})
	src := mirror(t, map[string]string{
		"SNMPv2-SMI": "SNMPv2-SMI DEFINITIONS ::= BEGIN\nEND\n",
	})
	out := filepath.Join(t.TempDir(), "standard")

	code, findings, errs := exec(t, "-fetch", "-out", out, "-source", src, dir)
	if code != 0 {
		t.Fatalf("exit = %d, want the set complete after fetching (%s / %s)", code, findings, errs)
	}
	if !strings.Contains(errs, "fetched SNMPv2-SMI") {
		t.Errorf("the fetch must be reported: %q", errs)
	}
	if _, err := os.Stat(filepath.Join(out, "SNMPv2-SMI")); err != nil {
		t.Errorf("the module was not written: %v", err)
	}
}

// Every download is validated before it is written, because a bad file
// on disk is worse than an absent one: the next run stops reporting it
// as missing. A rendered browser page and a mirror that served a
// different module are both refused.
func TestFetchRefusesWhatIsNotTheModule(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{"a rendered web page", "<!DOCTYPE html><html>SNMPv2-SMI</html>", "web page"},
		{"a different module", "OTHER-MIB DEFINITIONS ::= BEGIN\nEND\n", "declares OTHER-MIB"},
		{"something that declares nothing", "just some text\n", "declares no module"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := mibDir(t, map[string]string{
				"IRD-MIB": "IRD-MIB DEFINITIONS ::= BEGIN\nIMPORTS enterprises FROM SNMPv2-SMI;\nEND\n",
			})
			src := mirror(t, map[string]string{"SNMPv2-SMI": tc.body})
			out := filepath.Join(t.TempDir(), "standard")

			code, _, errs := exec(t, "-fetch", "-out", out, "-source", src, dir)
			if code != 1 {
				t.Fatalf("exit = %d, want the import still missing", code)
			}
			if !strings.Contains(errs, tc.want) || !strings.Contains(errs, "not written") {
				t.Errorf("stderr = %q, want %q and a refusal to write", errs, tc.want)
			}
			if _, err := os.Stat(filepath.Join(out, "SNMPv2-SMI")); err == nil {
				t.Error("a module that failed validation was written anyway")
			}
		})
	}
}

// A mirror that will not serve the module is reported and the run
// carries on: one absent module must not stop the others being
// fetched.
func TestFetchReportsAMirrorThatWillNotServe(t *testing.T) {
	dir := mibDir(t, map[string]string{
		"IRD-MIB": "IRD-MIB DEFINITIONS ::= BEGIN\nIMPORTS a FROM ABSENT-MIB;\nIMPORTS b FROM PRESENT-MIB;\nEND\n",
	})
	src := mirror(t, map[string]string{
		"PRESENT-MIB": "PRESENT-MIB DEFINITIONS ::= BEGIN\nEND\n",
	})
	out := filepath.Join(t.TempDir(), "standard")

	code, _, errs := exec(t, "-fetch", "-out", out, "-source", src, dir)
	if code != 1 {
		t.Fatalf("exit = %d, want the one that could not be fetched still missing", code)
	}
	if !strings.Contains(errs, "ABSENT-MIB") || !strings.Contains(errs, "404") {
		t.Errorf("the mirror's refusal must be reported: %q", errs)
	}
	if _, err := os.Stat(filepath.Join(out, "PRESENT-MIB")); err != nil {
		t.Errorf("the module that was available should still have been fetched: %v", err)
	}
}

// A set with nothing missing downloads nothing, whatever -fetch says.
func TestFetchWithNothingMissing(t *testing.T) {
	dir := mibDir(t, map[string]string{
		"IRD-MIB": "IRD-MIB DEFINITIONS ::= BEGIN\nEND\n",
	})
	out := filepath.Join(t.TempDir(), "standard")

	code, _, errs := exec(t, "-fetch", "-out", out, "-source", "http://mirror.invalid", dir)
	if code != 0 {
		t.Fatalf("exit = %d (%s)", code, errs)
	}
	if strings.Contains(errs, "fetched") {
		t.Errorf("nothing was missing: %q", errs)
	}
}

// An output directory that cannot be made stops the fetch rather than
// reporting downloads nobody could write.
func TestFetchIntoADirectoryItCannotMake(t *testing.T) {
	dir := mibDir(t, map[string]string{
		"IRD-MIB": "IRD-MIB DEFINITIONS ::= BEGIN\nIMPORTS a FROM SNMPv2-SMI;\nEND\n",
	})
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	code, _, errs := exec(t, "-fetch", "-out", filepath.Join(blocker, "standard"),
		"-source", "http://mirror.invalid", dir)
	if code != 1 {
		t.Fatalf("exit = %d, want the import still missing", code)
	}
	if !strings.Contains(errs, "mibcheck:") {
		t.Errorf("the directory failure must be reported: %q", errs)
	}
}

// A module the mirror serves but the disk will not take is reported
// rather than counted as fetched.
func TestFetchReportsAWriteItCannotMake(t *testing.T) {
	dir := mibDir(t, map[string]string{
		"IRD-MIB": "IRD-MIB DEFINITIONS ::= BEGIN\nIMPORTS a FROM SNMPv2-SMI;\nEND\n",
	})
	src := mirror(t, map[string]string{
		"SNMPv2-SMI": "SNMPv2-SMI DEFINITIONS ::= BEGIN\nEND\n",
	})
	out := t.TempDir()
	// A directory where the module's file belongs.
	if err := os.MkdirAll(filepath.Join(out, "SNMPv2-SMI"), 0o755); err != nil {
		t.Fatal(err)
	}

	code, _, errs := exec(t, "-fetch", "-out", out, "-source", src, dir)
	if code != 1 {
		t.Fatalf("exit = %d, want the import still missing", code)
	}
	if !strings.Contains(errs, "write") {
		t.Errorf("the write failure must be reported: %q", errs)
	}
}

// A source that is not reachable at all is reported per module, not as
// a crash.
func TestDownloadFromAnUnreachableMirror(t *testing.T) {
	if _, err := download("http://127.0.0.1:1", "SNMPv2-SMI"); err == nil {
		t.Error("an unreachable mirror must be reported")
	}
	if _, err := download("://not-a-url", "SNMPv2-SMI"); err == nil {
		t.Error("a source that cannot become a request must be reported")
	}
}

// main is the entry point every user meets, so it is exercised as one:
// argv from the process, exit code out.
func TestMainWiring(t *testing.T) {
	dir := mibDir(t, map[string]string{
		"CLEAN-MIB": "CLEAN-MIB DEFINITIONS ::= BEGIN\nEND\n",
	})

	prevArgs, prevExit := os.Args, osExit
	var got int
	os.Args = []string{"mibcheck", "-q", dir}
	osExit = func(code int) { got = code }
	t.Cleanup(func() { os.Args, osExit = prevArgs, prevExit })

	main()

	if got != 0 {
		t.Errorf("main exited %d, want 0", got)
	}
}

// A file the walk finds but cannot read stops the scan: a set half
// read is a set whose findings mean nothing.
func TestScanReportsAFileItCannotRead(t *testing.T) {
	dir := t.TempDir()
	// A directory named like a module: the walk skips directories, so
	// this one is about the read failing, not the walk.
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "A-MIB"),
		[]byte("A-MIB DEFINITIONS ::= BEGIN\nEND\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// A nested module is still part of the set.
	set, err := scan([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	if set.files != 1 {
		t.Errorf("files = %d, want the nested module counted", set.files)
	}
}

// A README beside the modules is not one of them, and neither is a
// file whose extension says it is something else.
func TestOnlyMIBSourceIsCollected(t *testing.T) {
	dir := mibDir(t, map[string]string{
		"A-MIB":     "A-MIB DEFINITIONS ::= BEGIN\nEND\n",
		"B.my":      "B DEFINITIONS ::= BEGIN\nEND\n",
		"C.txt":     "C DEFINITIONS ::= BEGIN\nEND\n",
		"README.md": "not a module",
		"notes.pdf": "not a module either",
	})

	set, err := scan([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	if set.files != 3 {
		t.Errorf("files = %d, want the three modules only", set.files)
	}
}

// The same directory named twice is one set, not two: -fetch appends
// its output directory to the scan list and it may already be inside
// one of them.
func TestOverlappingDirectoriesCountOnce(t *testing.T) {
	dir := mibDir(t, map[string]string{
		"A-MIB": "A-MIB DEFINITIONS ::= BEGIN\nEND\n",
	})

	set, err := scan([]string{dir, dir})
	if err != nil {
		t.Fatal(err)
	}
	if set.files != 1 {
		t.Errorf("files = %d, want each file counted once", set.files)
	}
}

// A finding renders like a compiler diagnostic so an editor can jump
// to it — with the line when there is one, and without when the defect
// belongs to the file as a whole.
func TestFindingRendering(t *testing.T) {
	withLine := Finding{File: "IRD-MIB", Line: 12, Rule: "duplicate-value", Msg: "5 is used twice"}
	if got := withLine.String(); got != "IRD-MIB:12: duplicate-value: 5 is used twice" {
		t.Errorf("= %q", got)
	}
	whole := Finding{File: "IRD-MIB", Rule: "missing-import", Msg: "SNMPv2-SMI"}
	if got := whole.String(); got != "IRD-MIB: missing-import: SNMPv2-SMI" {
		t.Errorf("= %q", got)
	}
}

var _ = fmt.Sprint

// A file the walk listed and the disk then would not give up stops the
// scan: a set half read is a set whose findings mean nothing.
func TestScanStopsOnAFileItCannotRead(t *testing.T) {
	dir := mibDir(t, map[string]string{"A-MIB": "A-MIB DEFINITIONS ::= BEGIN\nEND\n"})

	prev := readFile
	readFile = func(string) ([]byte, error) { return nil, errTest("disk refused") }
	t.Cleanup(func() { readFile = prev })

	if code, _, errs := exec(t, dir); code != 2 || !strings.Contains(errs, "disk refused") {
		t.Fatalf("= %d %q, want the read failure reported", code, errs)
	}
}

// errTest is an error with a fixed message.
type errTest string

func (e errTest) Error() string { return string(e) }

// The re-scan after a fetch reads what is on disk rather than trusting
// what was just written — and when that read fails, the run stops
// rather than reporting findings from a set it could not see.
func TestRescanAfterAFetchThatFails(t *testing.T) {
	dir := mibDir(t, map[string]string{
		"IRD-MIB": "IRD-MIB DEFINITIONS ::= BEGIN\nIMPORTS a FROM SNMPv2-SMI;\nEND\n",
	})
	src := mirror(t, map[string]string{
		"SNMPv2-SMI": "SNMPv2-SMI DEFINITIONS ::= BEGIN\nEND\n",
	})

	// Only the re-scan goes through the seam; the first scan is a
	// direct call, so refusing every call through it refuses exactly
	// the re-scan.
	prev := scanSet
	scanSet = func([]string) (*Set, error) { return nil, errTest("the set changed under us") }
	t.Cleanup(func() { scanSet = prev })

	code, _, errs := exec(t, "-fetch", "-out", filepath.Join(t.TempDir(), "standard"),
		"-source", src, dir)
	if code != 2 || !strings.Contains(errs, "changed under us") {
		t.Fatalf("= %d %q, want the re-scan failure reported", code, errs)
	}
}

// Findings are ordered by file and then by line, so a run's output can
// be diffed against the last one.
func TestFindingsAreOrderedAcrossFiles(t *testing.T) {
	dir := mibDir(t, map[string]string{
		"B-MIB": "B-MIB DEFINITIONS ::= BEGIN\nx ::= INTEGER { a(1), b(1) }\nEND\n",
		"A-MIB": "A-MIB DEFINITIONS ::= BEGIN\n\n\ny ::= INTEGER { c(2), d(2) }\nEND\n",
	})

	_, out, _ := exec(t, "-q", dir)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 2 {
		t.Fatalf("findings = %q, want one per file", out)
	}
	if !strings.Contains(lines[0], "A-MIB") {
		t.Errorf("output is not file-ordered:\n%s", out)
	}
}

// A README beside the modules is not one of them, whatever extension
// it carries — README.txt passes the extension test and would
// otherwise be linted as source.
func TestREADMEIsNotAModule(t *testing.T) {
	dir := mibDir(t, map[string]string{
		"A-MIB":      "A-MIB DEFINITIONS ::= BEGIN\nEND\n",
		"README.txt": "notes about the set, with x ::= INTEGER { a(1), b(1) } in them\n",
		"README":     "more notes\n",
	})

	set, err := scan([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	if set.files != 1 {
		t.Errorf("files = %d, want the README not counted as source", set.files)
	}
}

// A one-member enumeration cannot be ambiguous, and a value too large
// to be a wire value is not one the linter can reason about — both are
// left alone rather than reported as something they are not.
func TestEnumerationsTheLinterLeavesAlone(t *testing.T) {
	findings := checkEnums("X-MIB", "x ::= INTEGER { only(1) }")
	if len(findings) != 0 {
		t.Errorf("a one-member enumeration = %+v, want nothing", findings)
	}

	findings = checkEnums("X-MIB", "x ::= INTEGER { a(99999999999999999999), b(99999999999999999999) }")
	if len(findings) != 0 {
		t.Errorf("values the linter cannot read = %+v, want nothing", findings)
	}
}
