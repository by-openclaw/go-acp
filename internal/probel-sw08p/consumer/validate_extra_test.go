package probelsw08p

import (
	"context"
	"encoding/hex"
	"testing"

	"dhs/internal/probel-sw08p/codec"
	"dhs/internal/consumer"
	"dhs/internal/wiretrace"
)

func hexFrame(f codec.Frame) string {
	return hex.EncodeToString(codec.Pack(f))
}

// TestValidateHappyPath drives ACK, NAK, a good application frame, an
// unknown-command frame, a bad-hex trame, and a malformed (Unpack-failing)
// frame in one pass — covering every branch of Validate + shortHex.
func TestValidateHappyPath(t *testing.T) {
	good := codec.EncodeCrosspointTally(codec.CrosspointTallyParams{DestinationID: 1, SourceID: 2})

	// Unknown command id: a well-framed frame whose ID is not in the
	// catalogue. 0x7E (126) is unused in the SW-P-08 table.
	unknown := codec.Frame{ID: codec.CommandID(0x7E), Payload: []byte{0x00}}

	// A frame that Unpack rejects: DLE STX then garbage with no DLE ETX
	// resolution — corrupt the checksum by flipping the last data byte.
	bad := codec.Pack(good)
	badHex := hex.EncodeToString(bad[:len(bad)-2]) // truncate the DLE ETX → ErrUnpack path

	trames := []wiretrace.Trame{
		{Direction: wiretrace.DirectionRx, Hex: hex.EncodeToString(codec.PackACK())},
		{Direction: wiretrace.DirectionRx, Hex: hex.EncodeToString(codec.PackNAK())},
		{Direction: wiretrace.DirectionTx, Hex: hexFrame(good)},
		{Direction: wiretrace.DirectionTx, Hex: hexFrame(unknown)},
		{Direction: wiretrace.DirectionRx, Hex: "zzzz"},  // bad hex
		{Direction: wiretrace.DirectionRx, Hex: badHex},  // Unpack error
	}

	p := freshPlugin()
	report, err := p.Validate(context.Background(), trames, consumer.ValidateOpts{})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	// ACK + NAK + good + unknown = 4 processed; bad-hex + Unpack-err = 2 errors.
	if report.TramesProcessed != 4 {
		t.Errorf("TramesProcessed = %d; want 4", report.TramesProcessed)
	}
	if len(report.Errors) != 2 {
		t.Errorf("Errors = %d; want 2", len(report.Errors))
	}
	// NAK invariant + unknown-command invariant = at least 2 invariants.
	if len(report.Invariants) < 2 {
		t.Errorf("Invariants = %d; want >= 2", len(report.Invariants))
	}
}

// TestValidateShortHexPrefix exercises the >16-byte truncation in shortHex
// via a long malformed frame.
func TestValidateShortHexPrefix(t *testing.T) {
	// A long DLE STX-prefixed buffer with no valid framing → Unpack fails
	// and HexPrefix runs shortHex over >16 bytes.
	long := append([]byte{codec.DLE, codec.STX}, make([]byte, 40)...)
	trames := []wiretrace.Trame{
		{Direction: wiretrace.DirectionRx, Hex: hex.EncodeToString(long)},
	}
	p := freshPlugin()
	report, err := p.Validate(context.Background(), trames, consumer.ValidateOpts{})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(report.Errors) != 1 {
		t.Fatalf("Errors = %d; want 1", len(report.Errors))
	}
	if len(report.Errors[0].HexPrefix) != 32 { // 16 bytes → 32 hex chars
		t.Errorf("HexPrefix len = %d; want 32 (16 bytes)", len(report.Errors[0].HexPrefix))
	}
}

func TestValidateStopAt(t *testing.T) {
	good := codec.EncodeCrosspointTally(codec.CrosspointTallyParams{DestinationID: 1, SourceID: 2})
	trames := []wiretrace.Trame{
		{Direction: wiretrace.DirectionTx, Hex: hexFrame(good), Note: "first"},
		{Direction: wiretrace.DirectionTx, Hex: hexFrame(good), Note: "stophere"},
		{Direction: wiretrace.DirectionTx, Hex: hexFrame(good), Note: "after"},
	}
	p := freshPlugin()
	report, err := p.Validate(context.Background(), trames, consumer.ValidateOpts{StopAt: "stophere"})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if report.StoppedAt != "stophere" {
		t.Errorf("StoppedAt = %q; want stophere", report.StoppedAt)
	}
	if report.TramesProcessed != 2 {
		t.Errorf("TramesProcessed = %d; want 2 (stopped after second)", report.TramesProcessed)
	}
}

func TestValidateOutTreeUnsupported(t *testing.T) {
	p := freshPlugin()
	if _, err := p.Validate(context.Background(), nil, consumer.ValidateOpts{OutTree: "x.json"}); err == nil {
		t.Fatal("Validate with OutTree returned nil; want not-implemented error")
	}
	if _, err := p.Validate(context.Background(), nil, consumer.ValidateOpts{OutParams: "y.json"}); err == nil {
		t.Fatal("Validate with OutParams returned nil; want not-implemented error")
	}
}

func TestValidateContextCanceled(t *testing.T) {
	good := codec.EncodeCrosspointTally(codec.CrosspointTallyParams{DestinationID: 1, SourceID: 2})
	trames := []wiretrace.Trame{{Direction: wiretrace.DirectionTx, Hex: hexFrame(good)}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := freshPlugin()
	if _, err := p.Validate(ctx, trames, consumer.ValidateOpts{}); err == nil {
		t.Fatal("Validate with canceled ctx returned nil; want ctx error")
	}
}
