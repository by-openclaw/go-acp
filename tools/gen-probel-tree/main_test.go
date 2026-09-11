package main

// The generator's contract: a tree the Probel producer can load, on
// stdout or in a file, and a refusal for a shape the protocol cannot
// address.

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dhs/internal/export/canonical"
)

// execGen runs the generator and returns its exit code and both
// streams.
func execGen(t *testing.T, argv ...string) (int, string, string) {
	t.Helper()
	var out, errs bytes.Buffer
	code := run(argv, &out, &errs)
	return code, out.String(), errs.String()
}

// decode parses the generated tree the way the producer's loader does.
func decode(t *testing.T, body string) *canonical.Export {
	t.Helper()
	var exp canonical.Export
	if err := json.Unmarshal([]byte(body), &exp); err != nil {
		t.Fatalf("the tree must parse as a canonical export: %v", err)
	}
	return &exp
}

// The tree goes to stdout when no file is named, so it can be piped —
// and nothing else goes there, or the pipe carries a summary line into
// the producer's parser.
func TestTreeGoesToStdout(t *testing.T) {
	code, out, errs := execGen(t, "-matrices", "2", "-size", "3", "-levels", "2")
	if code != 0 {
		t.Fatalf("exit = %d (%s)", code, errs)
	}
	if errs != "" {
		t.Errorf("stderr = %q, want the tree alone on stdout", errs)
	}

	exp := decode(t, out)
	if exp.Root == nil {
		t.Fatal("the tree has a root")
	}
	if got := len(exp.Root.Common().Children); got != 2 {
		t.Fatalf("matrices = %d, want 2", got)
	}
}

// The shape asked for is the shape produced: one matrix per -matrices,
// one label set per level, and one name per target and source — the
// point of the tree is to exercise the name and label command paths at
// scale, so a tree missing them exercises nothing.
func TestTreeShapeMatchesTheRequest(t *testing.T) {
	_, out, _ := execGen(t, "-matrices", "2", "-size", "4", "-levels", "3")
	exp := decode(t, out)

	for i, child := range exp.Root.Common().Children {
		m, ok := child.(*canonical.Matrix)
		if !ok {
			t.Fatalf("child %d is a %s, want a matrix", i, child.Kind())
		}
		if m.TargetCount != 4 || m.SourceCount != 4 {
			t.Errorf("matrix %d = %d×%d, want 4×4", i, m.TargetCount, m.SourceCount)
		}
		if len(m.Labels) != 3 {
			t.Errorf("matrix %d has %d level label sets, want 3", i, len(m.Labels))
		}
		if len(m.TargetLabels) != 3 || len(m.SourceLabels) != 3 {
			t.Errorf("matrix %d labels %d targets / %d sources, want one map per level",
				i, len(m.TargetLabels), len(m.SourceLabels))
		}
		for lvl, names := range m.TargetLabels {
			if len(names) != 4 {
				t.Errorf("matrix %d %s: %d target names, want one per target", i, lvl, len(names))
			}
		}
		if got := m.TargetLabels["L0"]["0"]; !strings.HasPrefix(got, "TGT_M") {
			t.Errorf("target name = %q, want the positional label", got)
		}
		if got := m.SourceLabels["L0"]["0"]; !strings.HasPrefix(got, "SRC_M") {
			t.Errorf("source name = %q, want the positional label", got)
		}
	}
}

// With -out the tree lands in the file and the summary goes to stderr,
// so the two never mix.
func TestOutWritesTheFileAndSummarisesToStderr(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tree.json")

	code, out, errs := execGen(t, "-matrices", "1", "-size", "2", "-levels", "1", "-out", path)
	if code != 0 {
		t.Fatalf("exit = %d (%s)", code, errs)
	}
	if out != "" {
		t.Errorf("stdout = %q, want nothing when a file was named", out)
	}
	if !strings.Contains(errs, "wrote "+path) || !strings.Contains(errs, "elapsed") {
		t.Errorf("summary = %q", errs)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	decode(t, string(body))
}

// The bounds are the protocol's address widths, not this tool's taste:
// a tree outside them describes a router SW-P-08 cannot name, and the
// producer would serve it right up to the first command that could not
// address a crosspoint.
func TestBoundsAreRefused(t *testing.T) {
	for _, tc := range []struct {
		name string
		argv []string
		want string
	}{
		{"no matrices", []string{"-matrices", "0"}, "matrices must be 1..255"},
		{"more matrices than the address space", []string{"-matrices", "256"}, "matrices must be 1..255"},
		{"no targets", []string{"-size", "0"}, "size must be 1..65535"},
		{"more targets than the address space", []string{"-size", "65536"}, "size must be 1..65535"},
		{"no levels", []string{"-levels", "0"}, "levels must be 1..255"},
		{"more levels than the address space", []string{"-levels", "256"}, "levels must be 1..255"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, out, errs := execGen(t, tc.argv...)
			if code != 1 {
				t.Fatalf("exit = %d, want 1", code)
			}
			if out != "" {
				t.Errorf("a refused shape must produce no tree: %q", out)
			}
			if !strings.Contains(errs, tc.want) {
				t.Errorf("stderr = %q, want %q", errs, tc.want)
			}
		})
	}
}

// A flag the tool does not have is a usage error, not a default-valued
// run: generating a 65535×65535 tree because a flag was mistyped is
// half an hour of somebody's afternoon.
func TestAnUnknownFlagIsRefused(t *testing.T) {
	if code, out, _ := execGen(t, "-nonsense"); code != 1 || out != "" {
		t.Errorf("= %d %q, want a refusal and no tree", code, out)
	}
}

// A file that cannot be created is reported rather than leaving the
// caller to wonder where the tree went.
func TestOutThatCannotBeCreated(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	code, _, errs := execGen(t, "-size", "1", "-out", filepath.Join(blocker, "tree.json"))
	if code != 1 || !strings.Contains(errs, "gen-probel-tree:") {
		t.Fatalf("= %d %q, want the write refusal reported", code, errs)
	}
}

// A writer that refuses mid-encode is reported: a half-written tree on
// stdout is worse than none, because the producer would load whatever
// parsed.
func TestAWriterThatRefuses(t *testing.T) {
	var errs bytes.Buffer
	code := run([]string{"-matrices", "1", "-size", "1", "-levels", "1"},
		refusingWriter{}, &errs)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(errs.String(), "gen-probel-tree:") {
		t.Errorf("stderr = %q, want the encode failure reported", errs.String())
	}
}

type refusingWriter struct{}

func (refusingWriter) Write([]byte) (int, error) { return 0, errWrite }

type errString string

func (e errString) Error() string { return string(e) }

const errWrite = errString("the pipe went away")

// main is the entry point, exercised as one.
func TestMainWiring(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tree.json")

	prevArgs, prevExit := os.Args, osExit
	var got int
	os.Args = []string{"gen-probel-tree", "-matrices", "1", "-size", "1", "-levels", "1", "-out", path}
	osExit = func(code int) { got = code }
	t.Cleanup(func() { os.Args, osExit = prevArgs, prevExit })

	main()

	if got != 0 {
		t.Errorf("main exited %d, want 0", got)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("the tree was not written: %v", err)
	}
}
