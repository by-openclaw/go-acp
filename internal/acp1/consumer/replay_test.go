// Package acp1_test — replay tests run captured Trames (per ADR-0021)
// through the ACP1 plugin's Validate() method, verifying that every
// message decodes cleanly and that protocol invariants hold.
package acp1_test

import (
	"context"
	"dhs/internal/plugin"
	"encoding/hex"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"dhs/internal/acp1/codec"
	"dhs/internal/acp1/consumer"
	"dhs/internal/consumer"
	"dhs/internal/wiretrace"
)

// loadTrames reads a captured wire trace from
// captures/acp1/<scenario>/frames.jsonl (gitignored, local-only per
// ADR-0021). Skips cleanly when the file is missing — re-capture with
// `dhs consumer acp1 walk <ip> --slot N --capture <path>`.
func loadTrames(t *testing.T, scenario string) []wiretrace.Trame {
	t.Helper()
	path := filepath.Join("..", "..", "..", "captures", "acp1", scenario, "frames.jsonl")
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		t.Skipf("capture %s missing — recapture with `dhs consumer acp1 walk <ip> --slot N --capture %s`", path, path)
	}
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()

	trames, err := wiretrace.ReadTrames(f)
	if err != nil {
		t.Fatalf("ReadTrames: %v", err)
	}
	return trames
}

func newPlugin(t *testing.T) consumer.Validator {
	t.Helper()
	f := &acp1.Factory{}
	plug := f.New(plugin.Deps{Logger: slog.Default()})
	v, ok := plug.(consumer.Validator)
	if !ok {
		t.Fatal("acp1.Plugin does not implement consumer.Validator")
	}
	return v
}

// TestReplay_ACP1MessageDecode runs every Trame through Plugin.Validate
// and asserts no decode errors and no invariant violations on the
// reference slot 0 walk capture.
func TestReplay_ACP1MessageDecode(t *testing.T) {
	trames := loadTrames(t, "slot0_walk")
	if len(trames) == 0 {
		t.Fatal("no trames in capture")
	}
	v := newPlugin(t)
	report, err := v.Validate(context.Background(), trames, consumer.ValidateOpts{})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	t.Logf("ACP1 trames: %d decoded (%d tx, %d rx)",
		report.TramesProcessed,
		report.PerDirection[wiretrace.DirectionTx],
		report.PerDirection[wiretrace.DirectionRx])
	if len(report.Errors) > 0 {
		for _, e := range report.Errors {
			t.Errorf("trame %d (%s): %s", e.TrameIndex, e.Direction, e.Err)
		}
	}
	if len(report.Invariants) > 0 {
		for _, inv := range report.Invariants {
			t.Errorf("invariant: %s", inv)
		}
	}
}

// TestReplay_ACP1PropertyDecode verifies that getObject replies decode
// into typed objects with valid properties. Walks the fixture and
// counts reply objects via the ACP1 decoder directly (Validate covers
// the smoke; this asserts the spec-cited object-count floor).
func TestReplay_ACP1PropertyDecode(t *testing.T) {
	trames := loadTrames(t, "slot0_walk")

	var objects int
	typeCounts := map[codec.ObjectType]int{}

	for _, tr := range trames {
		raw, err := hex.DecodeString(tr.Hex)
		if err != nil {
			continue
		}
		msg, err := codec.Decode(raw)
		if err != nil {
			continue
		}
		if msg.MType != codec.MTypeReply || msg.MCode != byte(codec.MethodGetObject) {
			continue
		}
		obj, err := codec.DecodeObject(msg.Value)
		if err != nil {
			t.Errorf("DecodeObject(group=%d, id=%d): %v", msg.ObjGroup, msg.ObjID, err)
			continue
		}
		objects++
		typeCounts[obj.Type]++
	}

	t.Logf("objects decoded: %d", objects)
	for typ, count := range typeCounts {
		t.Logf("  type %d: %d", typ, count)
	}

	// Reference emulator slot 0 has at least 50 objects across all groups.
	if objects < 50 {
		t.Errorf("expected ≥50 objects for slot 0 walk, got %d", objects)
	}
}
