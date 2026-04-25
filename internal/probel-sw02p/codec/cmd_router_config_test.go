package codec

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

// TestEncodeRouterConfigRequestWireLayout locks rx 075 — zero-length
// MESSAGE like rx 050, 3-byte frame total.
func TestEncodeRouterConfigRequestWireLayout(t *testing.T) {
	wire := Pack(EncodeRouterConfigRequest(RouterConfigRequestParams{}))
	// checksum7({4B}) = (-(0x4B)) & 0x7F = 0x35.
	want := []byte{SOM, 0x4B, 0x35}
	if !bytes.Equal(wire, want) {
		t.Fatalf("wire\n got %X\nwant %X", wire, want)
	}
	parsed, n, err := Unpack(wire)
	if err != nil || n != len(wire) {
		t.Fatalf("Unpack: n=%d err=%v", n, err)
	}
	if _, err := DecodeRouterConfigRequest(parsed); err != nil {
		t.Errorf("Decode: %v", err)
	}
}

// TestDecodeRouterConfigRequestRejects confirms guards.
func TestDecodeRouterConfigRequestRejects(t *testing.T) {
	if _, err := DecodeRouterConfigRequest(Frame{ID: TxRouterConfigResponse1}); !errors.Is(err, ErrWrongCommand) {
		t.Errorf("wrong ID: got %v; want ErrWrongCommand", err)
	}
}

// TestPackLevelMapBitOrder pins the 28-bit bit map layout from
// §3.2.58: byte 1 bits 0-6 = levels 21-27, byte 4 bits 0-6 = levels
// 0-6.
func TestPackLevelMapBitOrder(t *testing.T) {
	// Set level 0, level 7, level 14, level 21 — one bit per byte.
	in := uint32((1 << 0) | (1 << 7) | (1 << 14) | (1 << 21))
	got := packLevelMap(in)
	want := [4]byte{0x01, 0x01, 0x01, 0x01}
	if got != want {
		t.Errorf("pack(%#x) = %X; want %X", in, got, want)
	}
	if round := unpackLevelMap(got); round != in {
		t.Errorf("unpack round-trip = %#x; want %#x", round, in)
	}
	// Level 27 = byte 1 bit 6.
	in = 1 << 27
	got = packLevelMap(in)
	if got != [4]byte{0x40, 0x00, 0x00, 0x00} {
		t.Errorf("pack(level 27) = %X; want [40 00 00 00]", got)
	}
	// Level 6 = byte 4 bit 6.
	in = 1 << 6
	got = packLevelMap(in)
	if got != [4]byte{0x00, 0x00, 0x00, 0x40} {
		t.Errorf("pack(level 6) = %X; want [00 00 00 40]", got)
	}
}

// TestEncodeRouterConfigResponse1RoundTrip pins tx 076 with a
// realistic multi-level fixture — 3 levels (0, 1, 2) with per-level
// counts, plus a scanner-variability test.
func TestEncodeRouterConfigResponse1RoundTrip(t *testing.T) {
	p := RouterConfigResponse1Params{
		LevelMap: (1 << 0) | (1 << 1) | (1 << 2),
		Levels: []RouterConfigResponse1LevelEntry{
			{NumDestinations: 128, NumSources: 128},
			{NumDestinations: 64, NumSources: 64},
			{NumDestinations: 32, NumSources: 32},
		},
	}
	wire := Pack(EncodeRouterConfigResponse1(p))
	parsed, n, err := Unpack(wire)
	if err != nil || n != len(wire) {
		t.Fatalf("Unpack: n=%d err=%v", n, err)
	}
	got, err := DecodeRouterConfigResponse1(parsed)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.LevelMap != p.LevelMap {
		t.Errorf("LevelMap = %#x; want %#x", got.LevelMap, p.LevelMap)
	}
	if len(got.Levels) != len(p.Levels) {
		t.Fatalf("Levels len = %d; want %d", len(got.Levels), len(p.Levels))
	}
	for i, want := range p.Levels {
		if got.Levels[i] != want {
			t.Errorf("Levels[%d] = %+v; want %+v", i, got.Levels[i], want)
		}
	}
}

// TestEncodeRouterConfigResponse1Empty covers the zero-level case —
// bit map all zero, no entries, payload = 4 bytes.
func TestEncodeRouterConfigResponse1Empty(t *testing.T) {
	p := RouterConfigResponse1Params{LevelMap: 0}
	wire := Pack(EncodeRouterConfigResponse1(p))
	parsed, _, err := Unpack(wire)
	if err != nil {
		t.Fatalf("Unpack: %v", err)
	}
	got, err := DecodeRouterConfigResponse1(parsed)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.LevelMap != 0 || len(got.Levels) != 0 {
		t.Errorf("decoded = %+v; want empty", got)
	}
}

// TestUnpackRouterConfigResponse1NeedsHeader verifies the scanner
// returns io.ErrUnexpectedEOF when the 4-byte bitmap isn't fully
// buffered yet.
func TestUnpackRouterConfigResponse1NeedsHeader(t *testing.T) {
	p := RouterConfigResponse1Params{LevelMap: (1 << 0)}
	p.Levels = []RouterConfigResponse1LevelEntry{{NumDestinations: 8, NumSources: 8}}
	wire := Pack(EncodeRouterConfigResponse1(p))
	// Truncate after SOM + CMD + 2 bitmap bytes.
	_, _, err := Unpack(wire[:4])
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("truncated: got %v; want io.ErrUnexpectedEOF", err)
	}
}

// TestEncodeRouterConfigResponse2RoundTrip pins tx 077 with Start
// Dst/Src populated and Reserved bytes forced to zero.
func TestEncodeRouterConfigResponse2RoundTrip(t *testing.T) {
	p := RouterConfigResponse2Params{
		LevelMap: (1 << 0) | (1 << 2),
		Levels: []RouterConfigResponse2LevelEntry{
			{NumDestinations: 128, NumSources: 64, StartDestination: 0, StartSource: 0},
			{NumDestinations: 32, NumSources: 16, StartDestination: 192, StartSource: 0},
		},
	}
	wire := Pack(EncodeRouterConfigResponse2(p))
	parsed, _, err := Unpack(wire)
	if err != nil {
		t.Fatalf("Unpack: %v", err)
	}
	got, err := DecodeRouterConfigResponse2(parsed)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.LevelMap != p.LevelMap {
		t.Errorf("LevelMap = %#x; want %#x", got.LevelMap, p.LevelMap)
	}
	for i, want := range p.Levels {
		if got.Levels[i] != want {
			t.Errorf("Levels[%d] = %+v; want %+v", i, got.Levels[i], want)
		}
	}
	// Reserved bytes on encode (positions off+8 / off+9 within each
	// 10-byte level entry, starting after the 4-byte bitmap) must be
	// zero per §3.2.59.
	for i := 0; i < len(p.Levels); i++ {
		off := RouterConfigLevelMapBytes + RouterConfigResponse2EntrySize*i
		// Payload starts at wire[2], so wire[2+off+8] and wire[2+off+9].
		if wire[2+off+8] != 0 || wire[2+off+9] != 0 {
			t.Errorf("level %d reserved bytes = %02X %02X; want 00 00",
				i, wire[2+off+8], wire[2+off+9])
		}
	}
}

// TestDecodeRouterConfigResponse2IgnoresReservedGarbage confirms the
// decoder tolerates non-zero reserved bytes (§3.2.59 says bit 7 of
// each is 0 — anything else is still ignored on decode).
func TestDecodeRouterConfigResponse2IgnoresReservedGarbage(t *testing.T) {
	// Build a valid tx 077 wire, then flip the reserved bytes.
	p := RouterConfigResponse2Params{
		LevelMap: 1 << 0,
		Levels:   []RouterConfigResponse2LevelEntry{{NumDestinations: 4, NumSources: 4}},
	}
	wire := Pack(EncodeRouterConfigResponse2(p))
	// Reserved positions: wire[2 + 4 + 8] and wire[2 + 4 + 9].
	wire[2+4+8] = 0x55
	wire[2+4+9] = 0x7E
	// Recompute checksum.
	payloadLen := RouterConfigLevelMapBytes + RouterConfigResponse2EntrySize
	sum := byte(0)
	for _, b := range wire[1 : 1+1+payloadLen] {
		sum += b
	}
	wire[1+1+payloadLen] = byte(-sum) & 0x7F
	parsed, _, err := Unpack(wire)
	if err != nil {
		t.Fatalf("Unpack: %v", err)
	}
	got, err := DecodeRouterConfigResponse2(parsed)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Levels[0].NumDestinations != 4 {
		t.Errorf("decoded Levels[0].NumDestinations = %d; want 4", got.Levels[0].NumDestinations)
	}
}
