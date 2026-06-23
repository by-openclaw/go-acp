package emberplus

import (
	"context"
	"os"
	"testing"

	"dhs/internal/consumer"
	"dhs/internal/wiretrace"
)

// DHD stream capture oracle (ADR-0020 Bucket 5) — StreamEntry value pushes +
// S101 keep-alive traffic from a real DHD provider on port 9000. See
// internal/emberplus/testdata/fixtures/dhd-stream/README.md.
const (
	dhdStreamFixture    = "../testdata/fixtures/dhd-stream/stream.s101.jsonl"
	dhdStreamFrameCount = 530
)

// TestDHDStreamCapture_DecodesCleanThroughCodec replays a real streaming
// session (stream pushes + keep-alives) and requires a fully clean decode.
// This is the real-bytes regression for the keep-alive validate fix: before
// it, the keep-alives in this capture were flagged "unexpected command".
func TestDHDStreamCapture_DecodesCleanThroughCodec(t *testing.T) {
	f, err := os.Open(dhdStreamFixture)
	if err != nil {
		t.Fatalf("open DHD stream fixture: %v", err)
	}
	defer func() { _ = f.Close() }()

	trames, err := wiretrace.ReadTrames(f)
	if err != nil {
		t.Fatalf("ReadTrames: %v", err)
	}
	if len(trames) != dhdStreamFrameCount {
		t.Fatalf("frame count = %d, want %d (fixture edited/truncated?)", len(trames), dhdStreamFrameCount)
	}

	rep, err := newTreePlugin().Validate(context.Background(), trames, consumer.ValidateOpts{})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if rep.TramesProcessed != dhdStreamFrameCount {
		t.Errorf("processed %d, want %d", rep.TramesProcessed, dhdStreamFrameCount)
	}
	if len(rep.Errors) != 0 {
		t.Errorf("streaming capture must decode with 0 errors, got %d: %v", len(rep.Errors), rep.Errors)
	}
	if len(rep.Invariants) != 0 {
		t.Errorf("streaming capture must have 0 invariants (keep-alives are valid MsgEmBER cmd 1/2), got %d: %v",
			len(rep.Invariants), rep.Invariants)
	}
}
