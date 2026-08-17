package main

import (
	"context"
	"os"
	"strings"
	"testing"

	"dhs/internal/consumer"
	"dhs/internal/datastore"
)

// Flag-validation tests only — same topology as the rest of the
// cerebrum-nb CLI tests: everything here errors before any dial; the
// wire behaviour of the walk is the consumer package's fake-WS
// territory and the walk core itself is shared verbatim with `tree
// --device` (live-proven on the NOC 2026-08-16).
func TestCerebrumExtractValidateFlags(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"no-device", []string{"extract", "h"}, "--device is required"},
		{"no-subdev", []string{"extract", "h", "--device", "D", "--by-name"}, "--sub-device is required"},
		{"no-host", []string{"extract", "--device", "D", "--by-name", "--sub-device", "1", "--path", "G"}, "missing host"},
		{"no-host-no-path", []string{"extract", "--device", "D", "--by-name", "--sub-device", "1"}, "missing host"},
		{"get-no-path", []string{"get", "h"}, "--path is required"},
		{"get-no-host", []string{"get", "--path", "d.1.O"}, "missing host"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := runCerebrum(context.Background(), c.args)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("args %v: got %v, want %q", c.args, err, c.want)
			}
		})
	}
}

// TestWriteCerebrumExtract drives the persistence half without wire:
// DM + manifest land under the temp store, names / refs / endpoint
// as the ADR-0022 layout requires.
func TestWriteCerebrumExtract(t *testing.T) {
	prev := treeStore
	treeStore = datastore.NewTreeStore(t.TempDir())
	t.Cleanup(func() { treeStore = prev })

	// Device-agnostic DM paths (no device/sub prefix) — the binding
	// lives in the manifest.
	objs := []consumer.Object{{
		ID: 0, Path: []string{"A", "Delay"},
		Label: "A.Delay", Kind: consumer.KindFloat,
		Value: consumer.Value{Kind: consumer.KindFloat, Float: 5.5},
	}}
	// Trailing space on the wire name — the manifest Name is trimmed,
	// the Addr keeps the verbatim form for addressing.
	dmPath, mfPath, err := writeCerebrumExtract("SHPRM1@5.3.5", "cvt 1 ", "10.44.55.39", 40009, "1", objs)
	if err != nil {
		t.Fatalf("writeCerebrumExtract: %v", err)
	}

	dm, err := os.ReadFile(dmPath)
	if err != nil {
		t.Fatalf("DM file: %v", err)
	}
	for _, want := range []string{`"SHPRM1"`, `"5.3.5"`, `"cerebrum-nb"`, `"A.Delay"`} {
		if !strings.Contains(string(dm), want) {
			t.Fatalf("DM missing %s:\n%s", want, dm)
		}
	}
	mf, err := os.ReadFile(mfPath)
	if err != nil {
		t.Fatalf("manifest file: %v", err)
	}
	for _, want := range []string{`"cvt 1"`, `"cvt 1 "`, `"sub_device": "1"`, `"SHPRM1@5.3.5.json"`, `"10.44.55.39"`, `40009`} {
		if !strings.Contains(string(mf), want) {
			t.Fatalf("manifest missing %s:\n%s", want, mf)
		}
	}
}

// Error arms: nil store, WriteDM refusal (empty identity), manifest
// refusal (empty device name).
func TestWriteCerebrumExtract_Errors(t *testing.T) {
	prev := treeStore
	t.Cleanup(func() { treeStore = prev })

	treeStore = nil
	if _, _, err := writeCerebrumExtract("X@1", "d", "h", 1, "1", nil); err == nil || !strings.Contains(err.Error(), "not initialised") {
		t.Fatalf("nil store: %v", err)
	}

	treeStore = datastore.NewTreeStore(t.TempDir())
	if _, _, err := writeCerebrumExtract("", "d", "h", 1, "1", nil); err == nil || !strings.Contains(err.Error(), "write DM") {
		t.Fatalf("empty identity: %v", err)
	}
	if _, _, err := writeCerebrumExtract("X@1", "", "h", 1, "1", nil); err == nil || !strings.Contains(err.Error(), "write manifest") {
		t.Fatalf("empty device name: %v", err)
	}
}
