package emberplus

import (
	"context"
	"os"
	"strings"
	"testing"

	"dhs/internal/consumer"
	"dhs/internal/wiretrace"
)

// TinyEmber+ Router decoder oracle (ADR-0020 Bucket 5). Complements the DHD
// fixture: this device exposes Functions + 3 matrices (the Function/RPC and
// writable-matrix surface the DHD console lacks). See
// internal/emberplus/testdata/fixtures/tiny-ember-router/README.md.
const (
	tinyEmberRawFixture  = "../testdata/fixtures/tiny-ember-router/raw.s101.jsonl"
	tinyEmberTreeFixture = "../testdata/fixtures/tiny-ember-router/tree.json"
	tinyEmberFrameCount  = 331
)

// TestTinyEmberCapture_DecodesCleanThroughCodec replays real TinyEmber+
// router frames through the S101 + Glow codec: 0 errors, 0 wire-invariants.
func TestTinyEmberCapture_DecodesCleanThroughCodec(t *testing.T) {
	f, err := os.Open(tinyEmberRawFixture)
	if err != nil {
		t.Fatalf("open TinyEmber fixture: %v", err)
	}
	defer func() { _ = f.Close() }()

	trames, err := wiretrace.ReadTrames(f)
	if err != nil {
		t.Fatalf("ReadTrames: %v", err)
	}
	if len(trames) != tinyEmberFrameCount {
		t.Fatalf("frame count = %d, want %d (fixture edited/truncated?)", len(trames), tinyEmberFrameCount)
	}

	rep, err := newTreePlugin().Validate(context.Background(), trames, consumer.ValidateOpts{})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if rep.TramesProcessed != tinyEmberFrameCount {
		t.Errorf("processed %d, want %d", rep.TramesProcessed, tinyEmberFrameCount)
	}
	if len(rep.Errors) != 0 {
		t.Errorf("clean decode expected, got %d errors: %v", len(rep.Errors), rep.Errors)
	}
	if len(rep.Invariants) != 0 {
		t.Errorf("clean decode expected, got %d invariants: %v", len(rep.Invariants), rep.Invariants)
	}
}

// TestTinyEmberCapture_TreeHasFunctionsAndMatrices guards that the committed
// canonical tree carries the Function surface (DHD's gap) and all three
// matrices with NON-ZERO targetCounts — the latter proving the
// connection-only-delta-preserves-MatrixContents fix on a second device.
func TestTinyEmberCapture_TreeHasFunctionsAndMatrices(t *testing.T) {
	tree, err := os.ReadFile(tinyEmberTreeFixture)
	if err != nil {
		t.Fatalf("read TinyEmber tree fixture: %v", err)
	}
	s := string(tree)
	for _, want := range []string{
		"router.oneToN.matrix", "router.nToN.matrix", "router.dynamic.matrix",
		"add", "doNothing",
		`"targetCount": 200`, `"targetCount": 4`, `"targetCount": 2000`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("TinyEmber tree fixture missing %q", want)
		}
	}
	// Regression guard: a clobbered matrix would serialize targetCount 0.
	if strings.Contains(s, `"element": "matrix"`) && strings.Contains(s, `"targetCount": 0`) {
		t.Errorf("a matrix has targetCount 0 — connection-only delta wiped MatrixContents")
	}
}
