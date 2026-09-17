package probelsw02p

import (
	"context"
	"encoding/hex"
	"strings"
	"testing"

	"dhs/internal/consumer"
	"dhs/internal/probel-sw02p/codec"
	"dhs/internal/wiretrace"
)

// frameHex packs a Frame and returns its lowercase, space-free hex —
// the wiretrace.Trame.Hex convention.
func frameHex(f codec.Frame) string {
	return hex.EncodeToString(codec.Pack(f))
}

// TestValidateHappyPath runs a mix of well-formed tx/rx frames through
// Validate and asserts the per-direction counts plus zero errors /
// invariants.
func TestValidateHappyPath(t *testing.T) {
	p := &Plugin{logger: quietLogger()}
	trames := []wiretrace.Trame{
		{Direction: wiretrace.DirectionRx, Hex: frameHex(codec.EncodeInterrogate(codec.InterrogateParams{Destination: 3}))},
		{Direction: wiretrace.DirectionTx, Hex: frameHex(codec.EncodeTally(codec.TallyParams{Destination: 3, Source: 7}))},
		{Direction: wiretrace.DirectionTx, Hex: frameHex(codec.EncodeConnected(codec.ConnectedParams{Destination: 3, Source: 7}))},
	}
	report, err := p.Validate(context.Background(), trames, consumer.ValidateOpts{})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if report.TramesProcessed != 3 {
		t.Errorf("TramesProcessed = %d; want 3", report.TramesProcessed)
	}
	if report.PerDirection[wiretrace.DirectionRx] != 1 || report.PerDirection[wiretrace.DirectionTx] != 2 {
		t.Errorf("PerDirection = %+v; want rx=1 tx=2", report.PerDirection)
	}
	if len(report.Errors) != 0 {
		t.Errorf("Errors = %+v; want none", report.Errors)
	}
	if len(report.Invariants) != 0 {
		t.Errorf("Invariants = %+v; want none", report.Invariants)
	}
}

// TestValidateHexDecodeError: an odd-length / non-hex string records a
// per-trame error and does not abort the run.
func TestValidateHexDecodeError(t *testing.T) {
	p := &Plugin{logger: quietLogger()}
	trames := []wiretrace.Trame{
		{Direction: wiretrace.DirectionRx, Hex: "zz"}, // not hex
		{Direction: wiretrace.DirectionTx, Hex: frameHex(codec.EncodeTally(codec.TallyParams{Destination: 1, Source: 1}))},
	}
	report, err := p.Validate(context.Background(), trames, consumer.ValidateOpts{})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(report.Errors) != 1 {
		t.Fatalf("Errors = %d; want 1 hex-decode error", len(report.Errors))
	}
	if report.Errors[0].TrameIndex != 0 || !strings.Contains(report.Errors[0].Err, "hex decode") {
		t.Errorf("Errors[0] = %+v; want index 0 hex decode", report.Errors[0])
	}
	if report.TramesProcessed != 1 {
		t.Errorf("TramesProcessed = %d; want 1 (the valid frame after the bad one)", report.TramesProcessed)
	}
}

// TestValidateCodecDecodeError: a hex-valid but mis-framed byte run
// (bad SOM) records a sw-p-02 decode error with a hex prefix.
func TestValidateCodecDecodeError(t *testing.T) {
	p := &Plugin{logger: quietLogger()}
	// Valid hex, but first byte is not SOM (0xFF) → codec.Unpack fails.
	trames := []wiretrace.Trame{
		{Direction: wiretrace.DirectionRx, Hex: "00010203"},
	}
	report, err := p.Validate(context.Background(), trames, consumer.ValidateOpts{})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(report.Errors) != 1 {
		t.Fatalf("Errors = %d; want 1 decode error", len(report.Errors))
	}
	if !strings.Contains(report.Errors[0].Err, "sw-p-02 decode") {
		t.Errorf("Errors[0].Err = %q; want sw-p-02 decode", report.Errors[0].Err)
	}
	if report.Errors[0].HexPrefix == "" {
		t.Error("Errors[0].HexPrefix empty; want a short hex prefix")
	}
}

// TestValidateStopAt: a trame whose Note matches opts.StopAt halts the
// scan and records StoppedAt.
func TestValidateStopAt(t *testing.T) {
	p := &Plugin{logger: quietLogger()}
	trames := []wiretrace.Trame{
		{Direction: wiretrace.DirectionTx, Hex: frameHex(codec.EncodeTally(codec.TallyParams{Destination: 1, Source: 1})), Note: "marker"},
		{Direction: wiretrace.DirectionTx, Hex: frameHex(codec.EncodeTally(codec.TallyParams{Destination: 2, Source: 2}))},
	}
	report, err := p.Validate(context.Background(), trames, consumer.ValidateOpts{StopAt: "marker"})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if report.StoppedAt != "marker" {
		t.Errorf("StoppedAt = %q; want marker", report.StoppedAt)
	}
	if report.TramesProcessed != 1 {
		t.Errorf("TramesProcessed = %d; want 1 (stopped after first)", report.TramesProcessed)
	}
}

// TestValidateOutTreeUnsupported: requesting --out-tree returns the
// not-implemented error early.
func TestValidateOutTreeUnsupported(t *testing.T) {
	p := &Plugin{logger: quietLogger()}
	if _, err := p.Validate(context.Background(), nil, consumer.ValidateOpts{OutTree: "tree.json"}); err == nil {
		t.Error("Validate with OutTree = nil error; want not-implemented")
	}
	if _, err := p.Validate(context.Background(), nil, consumer.ValidateOpts{OutParams: "params.csv"}); err == nil {
		t.Error("Validate with OutParams = nil error; want not-implemented")
	}
}

// TestValidateContextCanceled: a context cancelled before the loop body
// returns the ctx error.
func TestValidateContextCanceled(t *testing.T) {
	p := &Plugin{logger: quietLogger()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	trames := []wiretrace.Trame{
		{Direction: wiretrace.DirectionTx, Hex: frameHex(codec.EncodeTally(codec.TallyParams{Destination: 1, Source: 1}))},
	}
	if _, err := p.Validate(ctx, trames, consumer.ValidateOpts{}); err == nil {
		t.Error("Validate(canceled ctx) = nil; want ctx error")
	}
}

// TestShortHexTruncates pins shortHex: long input is truncated to the
// first 16 bytes; short input passes through unchanged.
func TestShortHexTruncates(t *testing.T) {
	long := make([]byte, 32)
	for i := range long {
		long[i] = byte(i)
	}
	got := shortHex(long)
	if len(got) != 32 { // 16 bytes → 32 hex chars
		t.Errorf("shortHex(32 bytes) len = %d; want 32 hex chars (16 bytes)", len(got))
	}
	short := []byte{0xAB, 0xCD}
	if shortHex(short) != "abcd" {
		t.Errorf("shortHex(short) = %q; want abcd", shortHex(short))
	}
}
