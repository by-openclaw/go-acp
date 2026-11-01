package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"dhs/internal/snmp/mib"
)

const registry = `PRIVATE ENTERPRISE NUMBERS

(last updated 2026-10-30)

Decimal
| Organization
| | Contact
| | | Email
| | | |
0
  Reserved
    Internet Assigned Numbers Authority
      iana&iana.org
7995
  Snell & Wilcox Ltd
    Somebody
      somebody&example.com
1
  NxNetworks
    Somebody Else
      else&example.com
2
  ---none---
3
  Unassigned
`

func writeRegistry(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "enterprise-numbers.txt")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func exec(t *testing.T, argv ...string) (int, string, string) {
	t.Helper()
	var out, errs bytes.Buffer
	code := run(argv, &out, &errs)
	return code, out.String(), errs.String()
}

func TestItKeepsNamesAndNothingElse(t *testing.T) {
	rows, date, err := parse(strings.NewReader(registry))
	if err != nil {
		t.Fatal(err)
	}
	if date != "2026-10-30" || len(rows) != 2 {
		t.Fatalf("%q %+v", date, rows)
	}
	if rows[0] != (row{1, "NxNetworks"}) || rows[1] != (row{7995, "Snell & Wilcox Ltd"}) {
		t.Errorf("%+v", rows)
	}
}

func TestTheFileIsDeterministic(t *testing.T) {
	in := writeRegistry(t, registry)
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	for _, out := range []string{a, b} {
		if code, stdout, stderr := exec(t, "-in", in, "-out", out); code != 0 || !strings.Contains(stdout, "2 enterprises, registry of 2026-10-30") {
			t.Fatalf("%d %q %q", code, stdout, stderr)
		}
	}
	x, _ := os.ReadFile(a)
	y, _ := os.ReadFile(b)
	if !bytes.Equal(x, y) {
		t.Error("the same input gave different bytes")
	}
}

// The shipped file parses, and carries the registry's date.
func TestTheShippedFileIsThisToolsOutput(t *testing.T) {
	if name, ok := mib.Enterprise(30065); !ok || !strings.HasPrefix(name, "Arista") {
		t.Errorf("%q %v", name, ok)
	}
}

func TestUsageAndRefusals(t *testing.T) {
	good := writeRegistry(t, registry)
	for _, tc := range []struct {
		name string
		argv []string
		want string
	}{
		{"no flags", nil, "usage"},
		{"a bad flag", []string{"-nope"}, "flag provided"},
		{"no such file", []string{"-in", filepath.Join(t.TempDir(), "x"), "-out", filepath.Join(t.TempDir(), "o")}, "genpen: open"},
		{"not the registry", []string{"-in", writeRegistry(t, "hello\n"), "-out", filepath.Join(t.TempDir(), "o")}, "not IANA"},
		{"a number too large", []string{"-in", writeRegistry(t, "99999999999\n  x\n"), "-out", filepath.Join(t.TempDir(), "o")}, "not an enterprise number"},
		{"a line too long", []string{"-in", writeRegistry(t, strings.Repeat("x", 2<<20)), "-out", filepath.Join(t.TempDir(), "o")}, "too long"},
		{"an out that cannot be written", []string{"-in", good, "-out", t.TempDir()}, "is a directory"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, _, stderr := exec(t, tc.argv...)
			if code != 2 || !strings.Contains(stderr, tc.want) {
				t.Errorf("%d %q", code, stderr)
			}
		})
	}
}

// A number on the last line has no organisation under it.
func TestARegistryCutShortKeepsWhatItHas(t *testing.T) {
	rows, _, err := parse(strings.NewReader("(last updated 2026-10-30)\n1\n  a\n2\n"))
	if err != nil || len(rows) != 1 {
		t.Fatalf("%+v %v", rows, err)
	}
}

type failWriter struct{ left int }

func (w *failWriter) Write(p []byte) (int, error) {
	if w.left--; w.left < 0 {
		return 0, errors.New("disk full")
	}
	return len(p), nil
}

func TestEncodeRefusals(t *testing.T) {
	// Nothing a compressor can shorten, so writes reach the writer while
	// rows are still going in.
	var noise strings.Builder
	for i := 0; i < 1<<18; i++ {
		noise.WriteString(strconv.Itoa(i * 2654435761))
	}
	rows := []row{{1, noise.String()}, {2, noise.String()}}
	for _, n := range []int{0, 1, 2, 8} {
		if err := encode(&failWriter{left: n}, rows, "d"); err == nil {
			t.Errorf("a write that fails after %d must be reported", n)
		}
	}
}

func TestMainWiring(t *testing.T) {
	code := -1
	osExit = func(c int) { code = c }
	defer func() { osExit = os.Exit }()
	old := os.Args
	os.Args = []string{"genpen"}
	defer func() { os.Args = old }()
	main()
	if code != 2 {
		t.Errorf("exit %d", code)
	}
}
