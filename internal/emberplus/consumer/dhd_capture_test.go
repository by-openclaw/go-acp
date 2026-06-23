package emberplus

import (
	"context"
	"os"
	"strings"
	"testing"

	"dhs/internal/consumer"
	"dhs/internal/wiretrace"
)

// dhdRawFixture is the real DHD console capture committed under testdata.
// See internal/emberplus/testdata/fixtures/dhd/README.md for provenance.
const dhdRawFixture = "../testdata/fixtures/dhd/raw.s101.jsonl"

// dhdFrameCount is the exact number of S101 frames in the committed
// capture (rx 12558 + tx 105). Pinned so a truncated/edited fixture fails.
const dhdFrameCount = 12663

// TestDHDCapture_DecodesCleanThroughCodec is the Tier-1 decoder oracle:
// every frame from a real DHD audio console must decode through our S101 +
// Glow codec with zero errors and zero wire-invariant violations, and the
// reconstructed tree must contain the device's matrix + identity. This is
// ground truth — real device bytes, not synthetic frames.
func TestDHDCapture_DecodesCleanThroughCodec(t *testing.T) {
	f, err := os.Open(dhdRawFixture)
	if err != nil {
		t.Fatalf("open DHD fixture: %v", err)
	}
	defer func() { _ = f.Close() }()

	trames, err := wiretrace.ReadTrames(f)
	if err != nil {
		t.Fatalf("ReadTrames: %v", err)
	}
	if len(trames) != dhdFrameCount {
		t.Fatalf("fixture frame count = %d, want %d (fixture edited/truncated?)", len(trames), dhdFrameCount)
	}

	rep, err := newTreePlugin().Validate(context.Background(), trames, consumer.ValidateOpts{})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}

	if rep.TramesProcessed != dhdFrameCount {
		t.Errorf("processed %d frames, want %d", rep.TramesProcessed, dhdFrameCount)
	}
	if len(rep.Errors) != 0 {
		t.Errorf("real DHD capture must decode with 0 errors, got %d: %v", len(rep.Errors), rep.Errors)
	}
	if len(rep.Invariants) != 0 {
		t.Errorf("real DHD capture must have 0 wire-invariant violations, got %d: %v", len(rep.Invariants), rep.Invariants)
	}
}

// TestDHDCapture_TreeFixtureHasMatrixAndIdentity guards the committed
// canonical tree.json — the real DHD DM must carry the Routing matrix (the
// flood/spin subject) and the identity node the probe keys on. (Validate
// can't yet emit a tree offline; this asserts against the captured DM.)
func TestDHDCapture_TreeFixtureHasMatrixAndIdentity(t *testing.T) {
	treeJSON, err := os.ReadFile("../testdata/fixtures/dhd/tree.json")
	if err != nil {
		t.Fatalf("read DHD tree fixture: %v", err)
	}
	for _, want := range []string{"Device.Routing", "Device.Identity.Product", "Firmwareversion"} {
		if !strings.Contains(string(treeJSON), want) {
			t.Errorf("DHD tree fixture missing %q", want)
		}
	}
}
