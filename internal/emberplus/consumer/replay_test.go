// Package emberplus_test — replay tests run captured Trames (per
// ADR-0021) through the Ember+ plugin's Validate() method. Skips
// cleanly when no local capture is present so a fresh clone is
// green without `git lfs pull` or any pre-loaded fixtures.
package emberplus_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"dhs/internal/emberplus/consumer"
	"dhs/internal/protocol"
	"dhs/internal/wiretrace"
)

// loadTrames reads a captured wire trace from
// captures/emberplus/<scenario>/frames.jsonl (gitignored, local-only
// per ADR-0021). Skips cleanly when the file is missing — re-capture
// with `dhs consumer emberplus walk <ip> --capture <path>`.
func loadTrames(t *testing.T, scenario string) []wiretrace.Trame {
	t.Helper()
	path := filepath.Join("..", "..", "..", "captures", "emberplus", scenario, "frames.jsonl")
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		t.Skipf("capture %s missing — recapture with `dhs consumer emberplus walk <ip> --capture %s`", path, path)
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

func newValidator(t *testing.T) protocol.Validator {
	t.Helper()
	f := &emberplus.Factory{}
	plug := f.New(slog.Default())
	v, ok := plug.(protocol.Validator)
	if !ok {
		t.Fatal("emberplus.Plugin does not implement protocol.Validator")
	}
	return v
}

// TestReplay_EmberplusTreeWalk runs every Trame through Plugin.Validate
// and asserts no decode errors and no S101 / Glow invariant violations
// on a tree-walk capture.
func TestReplay_EmberplusTreeWalk(t *testing.T) {
	trames := loadTrames(t, "tree_walk")
	if len(trames) == 0 {
		t.Fatal("no trames in capture")
	}
	v := newValidator(t)
	report, err := v.Validate(context.Background(), trames, protocol.ValidateOpts{})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	t.Logf("emberplus trames: %d decoded (%d tx, %d rx)",
		report.TramesProcessed,
		report.PerDirection[wiretrace.DirectionTx],
		report.PerDirection[wiretrace.DirectionRx])
	for _, e := range report.Errors {
		t.Errorf("trame %d (%s): %s", e.TrameIndex, e.Direction, e.Err)
	}
	for _, inv := range report.Invariants {
		t.Errorf("invariant: %s", inv)
	}
}
