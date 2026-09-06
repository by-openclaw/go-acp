// Package probelsw08p_test — replay tests run captured Trames (per
// ADR-0021) through the SW-P-08 plugin's Validate() method. Skips
// cleanly when no local capture is present so a fresh clone is
// green without `git lfs pull` or any pre-loaded fixtures.
package probelsw08p_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"dhs/internal/consumer"
	"dhs/internal/probel-sw08p/consumer"
	"dhs/internal/wiretrace"
)

// loadTrames reads a captured wire trace from
// captures/probel-sw08p/<scenario>/frames.jsonl (gitignored, local-
// only per ADR-0021). Skips cleanly when the file is missing —
// re-capture with `dhs consumer probel-sw08p connect <ip>:<port> --capture <path>`.
func loadTrames(t *testing.T, scenario string) []wiretrace.Trame {
	t.Helper()
	path := filepath.Join("..", "..", "..", "captures", "probel-sw08p", scenario, "frames.jsonl")
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		t.Skipf("capture %s missing — recapture with `dhs consumer probel-sw08p ... --capture %s`", path, path)
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

func newValidator(t *testing.T) consumer.Validator {
	t.Helper()
	f := &probelsw08p.Factory{}
	plug := f.New(slog.Default())
	v, ok := plug.(consumer.Validator)
	if !ok {
		t.Fatal("probel-sw08p Plugin does not implement consumer.Validator")
	}
	return v
}

// TestReplay_ProbelSw08pCrosspointConnect runs every Trame through
// Plugin.Validate and asserts no decode errors and no SW-P-08
// invariant violations on a connect-burst capture.
func TestReplay_ProbelSw08pCrosspointConnect(t *testing.T) {
	trames := loadTrames(t, "crosspoint_connect")
	if len(trames) == 0 {
		t.Fatal("no trames in capture")
	}
	v := newValidator(t)
	report, err := v.Validate(context.Background(), trames, consumer.ValidateOpts{})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	t.Logf("probel-sw08p trames: %d decoded (%d tx, %d rx)",
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
