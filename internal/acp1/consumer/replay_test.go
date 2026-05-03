// Package acp1_test — replay tests run captured Trames (per ADR-0021)
// through the ACP1 plugin's Validate() method, verifying that every
// message decodes cleanly and that protocol invariants hold.
package acp1_test

import (
	"context"
	"encoding/hex"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"dhs/internal/acp1/consumer"
	"dhs/internal/protocol"
	"dhs/internal/wiretrace"
)

// loadTrames reads a JSONL fixture from the testdata/fixtures
// directory. Skips the test if the file is a Git LFS pointer
// (CI runners without `git lfs pull`).
func loadTrames(t *testing.T, name string) []wiretrace.Trame {
	t.Helper()
	path := filepath.Join("..", "testdata", "fixtures", name)
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()

	trames, err := wiretrace.ReadTrames(f)
	if errors.Is(err, wiretrace.ErrLFSPointer) {
		t.Skipf("%s is a Git LFS pointer; install git-lfs or run `git lfs pull`", name)
	}
	if err != nil {
		t.Fatalf("ReadTrames: %v", err)
	}
	return trames
}

func newPlugin(t *testing.T) protocol.Validator {
	t.Helper()
	f := &acp1.Factory{}
	plug := f.New(slog.Default())
	v, ok := plug.(protocol.Validator)
	if !ok {
		t.Fatal("acp1.Plugin does not implement protocol.Validator")
	}
	return v
}

// TestReplay_ACP1MessageDecode runs every Trame through Plugin.Validate
// and asserts no decode errors and no invariant violations on the
// reference slot 0 walk capture.
func TestReplay_ACP1MessageDecode(t *testing.T) {
	trames := loadTrames(t, "slot0_walk.json")
	if len(trames) == 0 {
		t.Fatal("no trames in capture")
	}
	v := newPlugin(t)
	report, err := v.Validate(context.Background(), trames, protocol.ValidateOpts{})
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
	trames := loadTrames(t, "slot0_walk.json")

	var objects int
	typeCounts := map[acp1.ObjectType]int{}

	for _, tr := range trames {
		raw, err := hex.DecodeString(tr.Hex)
		if err != nil {
			continue
		}
		msg, err := acp1.Decode(raw)
		if err != nil {
			continue
		}
		if msg.MType != acp1.MTypeReply || msg.MCode != byte(acp1.MethodGetObject) {
			continue
		}
		obj, err := acp1.DecodeObject(msg.Value)
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
