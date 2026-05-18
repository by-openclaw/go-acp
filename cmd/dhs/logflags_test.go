package main

import (
	"errors"
	"flag"
	"strings"
	"testing"

	"dhs/internal/errcode"
)

// TestLadderLevel verifies the -v / -vv / -vvv / -vvvv → level-name
// mapping covers every rung in the R15 #476 ladder. The "raw" rung
// today maps to "trace" because raw S101 hex remains a --capture
// concern.
func TestLadderLevel(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*logFlagSet)
		want   string
	}{
		{"none", func(*logFlagSet) {}, ""},
		{"v1", func(lf *logFlagSet) { lf.v1 = true }, "info"},
		{"v2", func(lf *logFlagSet) { lf.v2 = true }, "debug"},
		{"v3", func(lf *logFlagSet) { lf.v3 = true }, "trace"},
		{"v4", func(lf *logFlagSet) { lf.v4 = true }, "trace"},
		{"v2+v4 highest wins", func(lf *logFlagSet) { lf.v2 = true; lf.v4 = true }, "trace"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lf := &logFlagSet{}
			tc.mutate(lf)
			if got := lf.ladderLevel(); got != tc.want {
				t.Errorf("ladderLevel() = %q; want %q", got, tc.want)
			}
		})
	}
}

// TestEffectiveLevel_Conflict asserts the validation:log-level-conflict
// code is returned when -v… and --log-level are both set.
func TestEffectiveLevel_Conflict(t *testing.T) {
	lf := &logFlagSet{level: "warn", v2: true}
	_, err := lf.effectiveLevel("info")
	if err == nil {
		t.Fatal("expected conflict error, got nil")
	}
	if !errors.Is(err, errLogLevelConflict) {
		t.Errorf("error chain missing errLogLevelConflict: %v", err)
	}
	c := errcode.From(err)
	if c == nil || c.Layer != errcode.LayerValidation || c.Name != "log-level-conflict" {
		t.Errorf("typed code = %+v; want validation:log-level-conflict", c)
	}
	if c.Class != errcode.ClassUsage {
		t.Errorf("class = %v; want ClassUsage (exit 2)", c.Class)
	}
}

// TestEffectiveLevel_LadderOnly asserts the ladder wins when no
// --log-level is set explicitly. defaultLevel == lf.level means it
// stayed at the parser default.
func TestEffectiveLevel_LadderOnly(t *testing.T) {
	lf := &logFlagSet{level: "info", v2: true}
	got, err := lf.effectiveLevel("info")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if got != "debug" {
		t.Errorf("level = %q; want debug", got)
	}
}

// TestEffectiveLevel_LevelOnly asserts --log-level alone is honoured.
func TestEffectiveLevel_LevelOnly(t *testing.T) {
	lf := &logFlagSet{level: "warn"}
	got, err := lf.effectiveLevel("info")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if got != "warn" {
		t.Errorf("level = %q; want warn", got)
	}
}

// TestResolve_InvalidFormat asserts an unknown --log-format returns
// the validation:log-format-invalid code.
func TestResolve_InvalidFormat(t *testing.T) {
	lf := &logFlagSet{level: "info", format: "yaml"}
	_, err := lf.resolve("info")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, errLogFormatInvalid) {
		t.Errorf("error chain missing errLogFormatInvalid: %v", err)
	}
}

// TestResolve_AllFormats spins each --log-format through and verifies
// the resolver returns a non-nil logger. Output shape coverage lives
// next to the logger constructors in internal/logging/.
func TestResolve_AllFormats(t *testing.T) {
	for _, format := range []string{"text", "json", "loki"} {
		t.Run(format, func(t *testing.T) {
			lf := &logFlagSet{level: "info", format: format}
			logger, err := lf.resolve("info")
			if err != nil {
				t.Fatalf("resolve(%s): %v", format, err)
			}
			if logger == nil {
				t.Fatalf("logger nil for %s", format)
			}
		})
	}
}

// TestAddLogFlags_Parses asserts the flag set wired by addLogFlags
// accepts every R15 flag name without panicking.
func TestAddLogFlags_Parses(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(&strings.Builder{}) // suppress help on parse error
	lf := addLogFlags(fs, "info")
	if err := fs.Parse([]string{
		"-v",
		"-vvvv",
		"-log-format", "loki",
		"-log-only",
	}); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !lf.v1 || !lf.v4 {
		t.Errorf("v1=%v v4=%v; want both true", lf.v1, lf.v4)
	}
	if lf.format != "loki" {
		t.Errorf("format = %q; want loki", lf.format)
	}
	if !lf.logOnly {
		t.Error("logOnly not set")
	}
}
