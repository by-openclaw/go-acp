package main

import (
	"flag"
	"io"
	"reflect"
	"testing"
)

// A flag written after the target is honoured, as the usage line of every
// verb promises (`<verb> <target> [flags]`). The standard parser stops at
// the first positional and drops the rest without a word.
func TestFlagsAfterTheTargetAreHonoured(t *testing.T) {
	newSet := func() (*flag.FlagSet, *string, *int, *bool) {
		fs := flag.NewFlagSet("walk", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		oid := fs.String("oid", "default", "")
		limit := fs.Int("limit", 0, "")
		numeric := fs.Bool("numeric", false, "")
		return fs, oid, limit, numeric
	}

	for _, tc := range []struct {
		why      string
		args     []string
		oid      string
		limit    int
		numeric  bool
		wantArgs []string
	}{
		{"flags after the target", []string{"10.0.0.1", "--oid", "1.3.6.1", "--limit", "5"}, "1.3.6.1", 5, false, []string{"10.0.0.1"}},
		{"flags before the target, as before", []string{"--oid", "1.3.6.1", "10.0.0.1"}, "1.3.6.1", 0, false, []string{"10.0.0.1"}},
		{"on both sides", []string{"--limit", "3", "10.0.0.1", "--oid=sysDescr.0"}, "sysDescr.0", 3, false, []string{"10.0.0.1"}},
		{"a boolean flag takes no value", []string{"10.0.0.1", "--numeric", "extra"}, "default", 0, true, []string{"10.0.0.1", "extra"}},
		{"a single dash", []string{"10.0.0.1", "-oid", "x"}, "x", 0, false, []string{"10.0.0.1"}},
		{"a negative number is a value, not a flag", []string{"10.0.0.1", "-5", "--limit", "2"}, "default", 2, false, []string{"10.0.0.1", "-5"}},
		{"after -- everything is positional", []string{"--limit", "1", "--", "10.0.0.1", "--oid", "x"}, "default", 1, false, []string{"10.0.0.1", "--oid", "x"}},
		{"no positional at all", []string{"--limit", "9"}, "default", 9, false, []string{}},
		{"a lone dash is positional", []string{"-", "--limit", "4"}, "default", 4, false, []string{"-"}},
	} {
		fs, oid, limit, numeric := newSet()
		if err := parseVerbFlags(fs, tc.args); err != nil {
			t.Errorf("%s: %v", tc.why, err)
			continue
		}
		if *oid != tc.oid || *limit != tc.limit || *numeric != tc.numeric {
			t.Errorf("%s: oid=%q limit=%d numeric=%v, want %q %d %v", tc.why, *oid, *limit, *numeric, tc.oid, tc.limit, tc.numeric)
		}
		if got := fs.Args(); !reflect.DeepEqual(append([]string{}, got...), tc.wantArgs) {
			t.Errorf("%s: positionals %q, want %q", tc.why, got, tc.wantArgs)
		}
	}

	// A flag the verb does not have is still reported, wherever it stands.
	for _, args := range [][]string{
		{"10.0.0.1", "--nope", "1"},
		{"--nope", "10.0.0.1"},
		{"10.0.0.1", "--=x"},
	} {
		fs, _, _, _ := newSet()
		if err := parseVerbFlags(fs, args); err == nil && len(args) > 0 && args[len(args)-1] != "--=x" {
			t.Errorf("%q: an unknown flag was accepted", args)
		}
	}
	// Help is help after the target too.
	fs, _, _, _ := newSet()
	if err := parseVerbFlags(fs, []string{"10.0.0.1", "--help"}); err != flag.ErrHelp {
		t.Errorf("--help after the target: %v, want flag.ErrHelp", err)
	}
	// A value flag at the very end has no value to take: the parser says so.
	fs, _, _, _ = newSet()
	if err := parseVerbFlags(fs, []string{"10.0.0.1", "--oid"}); err == nil {
		t.Error("--oid with no value was accepted")
	}
}
