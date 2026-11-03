package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dhs/internal/consumer"
)

// ADR-0037 P2: get, set, walk and export are the generic verbs, answered
// through the registered plugin. The fake gateway of the rrcs tests
// answers them as it answered the private verbs.

func TestRRCSGenericGet(t *testing.T) {
	f, _ := rrcsSetFake(t, true)
	ctx := context.Background()
	// One property, by its path: no walk, one GetPort (or the lists).
	out, err := rrcsStdout(t, func() error {
		return runGet(ctx, []string{f.addr(), "--protocol", "rrcs", "--path", "net.1.node.61.port.7.out.PortAes67Output.Multicast"})
	})
	if err != nil || !strings.Contains(out, "239.1.2.3") {
		t.Fatalf("get one property: %v\n%s", err, out)
	}
	// An object alone: every property, as one JSON document.
	out, err = rrcsStdout(t, func() error {
		return runGet(ctx, []string{f.addr(), "--protocol", "rrcs", "--path", "net.1.node.61.port.7.out"})
	})
	if err != nil || !strings.Contains(out, "Multicast") || !strings.Contains(out, "239.1.2.3") || !strings.Contains(out, "PortAes67Output") {
		t.Fatalf("get an object: %v\n%s", err, out)
	}
	// By ObjectID: GetObjectProperty.
	g := newRRCSFake(t, rrcsWalkAnswer)
	out, err = rrcsStdout(t, func() error {
		return runGet(ctx, []string{g.addr(), "--protocol", "rrcs", "--id", "77"})
	})
	if err != nil || !strings.Contains(out, "Conf 001") || !strings.Contains(out, "Owner") {
		t.Fatalf("get by id: %v\n%s", err, out)
	}
	out, err = rrcsStdout(t, func() error {
		return runGet(ctx, []string{g.addr(), "--protocol", "rrcs", "--id", "77", "--group", "names"})
	})
	if err != nil || !strings.Contains(out, "Label") || !strings.Contains(out, "Owner") {
		t.Fatalf("get names by id: %v\n%s", err, out)
	}
}

func TestRRCSGenericSet(t *testing.T) {
	f, changes := rrcsSetFake(t, true)
	ctx := context.Background()
	path := "net.1.node.61.port.7.out.PortAes67Output.Multicast"
	var val *consumer.ValidationError
	// Without --write-to: refused before anything is sent (P5).
	_, err := rrcsStdout(t, func() error {
		return runSet(ctx, []string{f.addr(), "--protocol", "rrcs", "--path", path, "--value", "239.9.9.9"})
	})
	if !errors.As(err, &val) || !strings.Contains(err.Error(), "--write-to "+f.addr()) {
		t.Fatalf("set without --write-to: %v", err)
	}
	// A dry run: validated, nothing sent.
	out, err := rrcsStdout(t, func() error {
		return runSet(ctx, []string{f.addr(), "--protocol", "rrcs", "--path", path, "--value", "239.9.9.9", "--dry-run"})
	})
	if err != nil || !strings.Contains(out, "would set") {
		t.Fatalf("dry run: %v\n%s", err, out)
	}
	// A value the specification refuses: validation error, nothing sent (P6).
	_, err = rrcsStdout(t, func() error {
		return runSet(ctx, []string{f.addr(), "--protocol", "rrcs", "--path", "net.1.node.61.port.7.out.PortAes67Output.Channels", "--value", "32", "--write-to", f.addr()})
	})
	if !errors.As(err, &val) || !strings.Contains(err.Error(), "maximum 16") {
		t.Fatalf("out of range: %v", err)
	}
	if len(*changes) != 0 {
		t.Fatalf("%d changes sent by refused or dry writes", len(*changes))
	}
	// Confirmed: one ConfigurationChange, read back, and the capture
	// holds the documents (the recorder reached the plugin).
	capture := filepath.Join(t.TempDir(), "set.jsonl")
	out, err = rrcsStdout(t, func() error {
		return runSet(ctx, []string{f.addr(), "--protocol", "rrcs", "--path", path, "--value", "239.9.9.9", "--write-to", f.addr(), "--capture", capture})
	})
	if err != nil || !strings.Contains(out, "confirmed") || !strings.Contains(out, "239.9.9.9") {
		t.Fatalf("set: %v\n%s", err, out)
	}
	if len(*changes) != 1 {
		t.Fatalf("%d changes sent, want 1", len(*changes))
	}
	raw, rerr := os.ReadFile(capture)
	if rerr != nil || !strings.Contains(string(raw), `"dir":"tx"`) || !strings.Contains(string(raw), `"dir":"rx"`) {
		t.Fatalf("capture: %v\n%s", rerr, raw)
	}
}

func TestRRCSGenericWalkAndExport(t *testing.T) {
	f := newRRCSFake(t, rrcsTreeAnswer)
	ctx := context.Background()
	out, err := rrcsStdout(t, func() error {
		return runWalk(ctx, []string{f.addr(), "--protocol", "rrcs", "--slot", "1"})
	})
	if err != nil || !strings.Contains(out, "Multicast") || !strings.Contains(out, "239.1.2.3") || !strings.Contains(out, "net.1.node.61.port.7.out") {
		t.Fatalf("walk slot 1: %v\n%s", err, out)
	}
	file := filepath.Join(t.TempDir(), "params.csv")
	if _, err := rrcsStdout(t, func() error {
		return runExport(ctx, []string{f.addr(), "--protocol", "rrcs", "--out", file})
	}); err != nil {
		t.Fatalf("export: %v", err)
	}
	raw, _ := os.ReadFile(file)
	if !strings.Contains(string(raw), "net.1.node.61.port.7.out.PortAes67Output.Multicast") || !strings.Contains(string(raw), "239.1.2.3") {
		t.Fatalf("export rows:\n%s", raw)
	}
}
