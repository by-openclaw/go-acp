// Package probelsw02p_test — replay tests run captured Trames (per
// ADR-0021) through the SW-P-02 plugin's Validate() method. Skips
// cleanly when no local capture is present so a fresh clone is
// green without `git lfs pull` or any pre-loaded fixtures.
package probelsw02p_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"dhs/internal/probel-sw02p/consumer"
	"dhs/internal/protocol"
	"dhs/internal/wiretrace"
)

// loadTrames reads a captured wire trace from
// captures/probel-sw02p/<scenario>/frames.jsonl (gitignored, local-
// only per ADR-0021). Skips cleanly when the file is missing —
// re-capture with `dhs consumer probel-sw02p connect <ip>:<port> --capture <path>`.
func loadTrames(t *testing.T, scenario string) []wiretrace.Trame {
	t.Helper()
	path := filepath.Join("..", "..", "..", "captures", "probel-sw02p", scenario, "frames.jsonl")
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		t.Skipf("capture %s missing — recapture with `dhs consumer probel-sw02p ... --capture %s`", path, path)
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
	f := &probelsw02p.Factory{}
	plug := f.New(slog.Default())
	v, ok := plug.(protocol.Validator)
	if !ok {
		t.Fatal("probel-sw02p Plugin does not implement protocol.Validator")
	}
	return v
}

// TestReplay_ProbelSw02pBootstrap runs every Trame through
// Plugin.Validate and asserts no decode errors and no SW-P-02
// invariant violations on a bootstrap-sweep capture.
func TestReplay_ProbelSw02pBootstrap(t *testing.T) {
	trames := loadTrames(t, "bootstrap_sweep")
	if len(trames) == 0 {
		t.Fatal("no trames in capture")
	}
	v := newValidator(t)
	report, err := v.Validate(context.Background(), trames, protocol.ValidateOpts{})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	t.Logf("probel-sw02p trames: %d decoded (%d tx, %d rx)",
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
