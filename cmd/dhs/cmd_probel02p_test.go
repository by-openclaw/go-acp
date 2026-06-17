package main

import (
	"context"
	"strings"
	"testing"

	codec02 "dhs/internal/probel-sw02p/codec"
)

// TestProbelSW02UnknownSubcommand — an unrecognised verb is a usage
// error, not a panic / silent no-op. Mirrors the SW-P-08 dispatcher
// contract (runProbelsw08p returns "unknown probel subcommand").
func TestProbelSW02UnknownSubcommand(t *testing.T) {
	err := runProbelsw02p(context.Background(), []string{"bogus-verb"})
	if err == nil {
		t.Fatal("expected error for unknown subcommand, got nil")
	}
	if !strings.Contains(err.Error(), "unknown probel-sw02p subcommand") {
		t.Errorf("error = %q, want it to mention unknown subcommand", err.Error())
	}
}

// TestProbelSW02HelpNoArgs — no args prints help (not an error). This is
// the same contract every per-protocol dispatcher honours so
// `dhs consumer probel-sw02p` shows the verb catalogue.
func TestProbelSW02HelpNoArgs(t *testing.T) {
	if err := runProbelsw02p(context.Background(), nil); err != nil {
		t.Fatalf("help dispatch returned error: %v", err)
	}
	if err := runProbelsw02p(context.Background(), []string{"-h"}); err != nil {
		t.Fatalf("help dispatch (-h) returned error: %v", err)
	}
}

// TestProbelSW02VerbsRequireAddr — every dispatched verb that talks to a
// device must reject a missing <host:port> with a usage error before it
// ever dials. Catches a verb wired into the switch but missing its
// popPositional guard. Mirrors SW-P-08's "missing <host:port>" contract.
func TestProbelSW02VerbsRequireAddr(t *testing.T) {
	verbs := []string{
		"interrogate", "connect", "connect-on-go", "go", "salvo-connect",
		"protect-connect", "protect-disconnect", "protect-interrogate",
		"protect-dump", "protect-name", "dual-status", "lock-status",
		"status", "router-config", "watch",
	}
	for _, v := range verbs {
		t.Run(v, func(t *testing.T) {
			err := runProbelsw02p(context.Background(), []string{v})
			if err == nil {
				t.Fatalf("verb %q with no <host:port> returned nil error", v)
			}
			if !strings.Contains(err.Error(), "missing <host:port>") {
				t.Errorf("verb %q error = %q, want 'missing <host:port>'", v, err.Error())
			}
		})
	}
}

// TestProbelSW02FlagValidation — out-of-range / malformed flags are
// rejected at parse time (before dial). Pins the per-verb bounds checks
// that mirror SW-P-08's --dst / --src / --device range guards.
func TestProbelSW02FlagValidation(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"dst-range", []string{"interrogate", "127.0.0.1:2002", "--dst", "99999"}, "--dst out of range"},
		{"src-range", []string{"connect", "127.0.0.1:2002", "--dst", "1", "--src", "99999"}, "--src out of range"},
		{"device-range", []string{"protect-connect", "127.0.0.1:2002", "--dst", "1", "--device", "99999"}, "--device out of range"},
		{"go-bad-op", []string{"go", "127.0.0.1:2002", "--op", "frobnicate"}, "unknown --op"},
		{"salvo-no-dsts", []string{"salvo-connect", "127.0.0.1:2002", "--src", "1"}, "--dsts is required"},
		{"status-bad-ctrl", []string{"status", "127.0.0.1:2002", "--controller", "middle"}, "unknown --controller"},
		{"lock-bad-ctrl", []string{"lock-status", "127.0.0.1:2002", "--controller", "middle"}, "unknown --controller"},
		{"protect-dump-count", []string{"protect-dump", "127.0.0.1:2002", "--count", "0"}, "--count out of range"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := runProbelsw02p(context.Background(), c.args)
			if err == nil {
				t.Fatalf("args %v returned nil error, want %q", c.args, c.want)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("args %v error = %q, want substring %q", c.args, err.Error(), c.want)
			}
		})
	}
}

// TestProbelSW02ParseGoOp pins the --op string → codec.GoOperation map.
func TestProbelSW02ParseGoOp(t *testing.T) {
	if op, err := parseGoOp("set"); err != nil || op != codec02.GoOpSet {
		t.Errorf("parseGoOp(set) = %v, %v; want GoOpSet", op, err)
	}
	if op, err := parseGoOp("clear"); err != nil || op != codec02.GoOpClear {
		t.Errorf("parseGoOp(clear) = %v, %v; want GoOpClear", op, err)
	}
	if _, err := parseGoOp("nope"); err == nil {
		t.Error("parseGoOp(nope) = nil error, want failure")
	}
}

// TestProbelSW02ParseController pins the --controller string → codec.Controller map.
func TestProbelSW02ParseController(t *testing.T) {
	for _, in := range []string{"lh", "LH", "0"} {
		if c, err := parseController(in); err != nil || c != codec02.ControllerLH {
			t.Errorf("parseController(%q) = %v, %v; want ControllerLH", in, c, err)
		}
	}
	for _, in := range []string{"rh", "RH", "1"} {
		if c, err := parseController(in); err != nil || c != codec02.ControllerRH {
			t.Errorf("parseController(%q) = %v, %v; want ControllerRH", in, c, err)
		}
	}
	if _, err := parseController("xx"); err == nil {
		t.Error("parseController(xx) = nil error, want failure")
	}
}

// TestProbelSW02ProtectStateName pins the display strings for every
// ProtectState so the CLI output stays stable.
func TestProbelSW02ProtectStateName(t *testing.T) {
	cases := map[codec02.ProtectState]string{
		codec02.ProtectNone:           "none",
		codec02.ProtectProBel:         "probel",
		codec02.ProtectProBelOverride: "probel-override",
		codec02.ProtectOEM:            "oem",
	}
	for st, want := range cases {
		if got := protectStateName(st); got != want {
			t.Errorf("protectStateName(%d) = %q, want %q", st, got, want)
		}
	}
}

// TestProbelSW02GoGroupResultName pins the salvo go-done result strings.
func TestProbelSW02GoGroupResultName(t *testing.T) {
	cases := map[codec02.GoGroupResult]string{
		codec02.GoGroupResultSet:     "set",
		codec02.GoGroupResultCleared: "cleared",
		codec02.GoGroupResultEmpty:   "empty",
	}
	for r, want := range cases {
		if got := goGroupResultName(r); got != want {
			t.Errorf("goGroupResultName(%d) = %q, want %q", r, got, want)
		}
	}
}
