package main

import (
	"context"
	"strings"
	"testing"
)

// Regression for the salvo-connect --dsts shadowing bug (both probel
// dispatchers). The GLOBAL matrix-config flag extractor parsed --dsts as
// a single uint (matrix bootstrap shape) BEFORE subcommand dispatch, so
// `salvo-connect ... --dsts 0-2` failed two ways at once:
//   - strconv.ParseUint parsing "0-2": invalid syntax (range eaten by global)
//   - --dsts is required (single value consumed, verb saw nothing)
// The fix detects the salvo-connect subcommand up front and leaves
// --dsts (and --level for SW-P-08) for the verb's own flag.FlagSet.
//
// These tests drive the real CLI parse path (runProbelsw08p /
// runProbelsw02p) and assert the parse no longer errors. They use an
// unreachable address with a tiny --timeout so the call fails on the
// network dial (proving parsing succeeded) without a real device. The
// assertion is purely on the error STRING: it must NOT mention the flag
// parse failures, regardless of whether the dial itself errors.

// flagParseErrorSubstrings are the two failure signatures the bug
// produced. None may appear once parsing is fixed.
var salvoFlagParseErrors = []string{
	"--dsts is required",
	`parsing "0-2"`,
	"invalid syntax",
}

func assertNoFlagParseError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		// A nil error is also acceptable: it means parsing succeeded and
		// the (loopback) dial happened to not error in this environment.
		return
	}
	for _, bad := range salvoFlagParseErrors {
		if strings.Contains(err.Error(), bad) {
			t.Fatalf("salvo-connect --dsts 0-2 still hits a flag-parse error: %q (must contain none of %v)",
				err.Error(), salvoFlagParseErrors)
		}
	}
}

// TestProbelSW08SalvoConnectDstsRange — `dhs consumer probel-sw08p
// salvo-connect <addr> --dsts 0-2` parses the CSV/range --dsts owned by
// the verb instead of failing in the global uint parser.
func TestProbelSW08SalvoConnectDstsRange(t *testing.T) {
	for _, dsts := range []string{"0-2", "1,3,5"} {
		t.Run(dsts, func(t *testing.T) {
			err := runProbelsw08p(context.Background(), []string{
				"salvo-connect", "127.0.0.1:1",
				"--src", "12", "--dsts", dsts, "--timeout", "50ms",
			})
			assertNoFlagParseError(t, err)
		})
	}
}

// TestProbelSW02SalvoConnectDstsRange — same for SW-P-02.
func TestProbelSW02SalvoConnectDstsRange(t *testing.T) {
	for _, dsts := range []string{"0-2", "1,3,5"} {
		t.Run(dsts, func(t *testing.T) {
			err := runProbelsw02p(context.Background(), []string{
				"salvo-connect", "127.0.0.1:1",
				"--src", "3", "--dsts", dsts, "--timeout", "50ms",
			})
			assertNoFlagParseError(t, err)
		})
	}
}

// TestProbelSubcommandDetection pins the up-front verb detector that
// decides whether to skip the global --dsts / --level. It must find the
// verb even when global matrix-config flags precede it (space and "="
// forms).
func TestProbelSubcommandDetection(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"plain", []string{"salvo-connect", "127.0.0.1:2008"}, "salvo-connect"},
		{"after-mtx-space", []string{"--mtx-id", "0", "salvo-connect", "addr"}, "salvo-connect"},
		{"after-mtx-eq", []string{"--mtx-id=0", "salvo-connect", "addr"}, "salvo-connect"},
		{"other-verb", []string{"connect", "addr"}, "connect"},
		{"empty", nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := probelSubcommand(c.args); got != c.want {
				t.Errorf("probelSubcommand(%v) = %q, want %q", c.args, got, c.want)
			}
			if got := probelSW02Subcommand(c.args); got != c.want {
				t.Errorf("probelSW02Subcommand(%v) = %q, want %q", c.args, got, c.want)
			}
		})
	}
}

// TestProbelGlobalDstsStillBootstraps — the global --dsts bootstrap
// shape must keep working for NON-salvo verbs (e.g. watch). A single
// uint --dsts is consumed by the global extractor as before; passing a
// range to a non-salvo verb still fails in the global parser (unchanged
// behavior). This guards against the fix over-broadly skipping --dsts.
func TestProbelGlobalDstsStillBootstraps(t *testing.T) {
	// Non-salvo verb keeps the global uint semantics: a range is rejected
	// by the global parser, proving --dsts is NOT skipped for it.
	err := runProbelsw08p(context.Background(), []string{
		"watch", "127.0.0.1:1", "--dsts", "0-2", "--timeout", "50ms",
	})
	if err == nil || !strings.Contains(err.Error(), "--dsts") {
		t.Fatalf("non-salvo --dsts 0-2 should fail in the global parser, got: %v", err)
	}
}
