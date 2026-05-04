package codec

import (
	"bytes"
	"reflect"
	"testing"
)

// Boundary tests for the Crosspoint Tally Dump form-selection ladder:
// cmd 22 (narrow byte) → cmd 23 (narrow word) → cmd 151 (extended word).
// Spec basis: SW-P-08 Issue 30 §3.3.10 (cmd 22 byte) + §3.3.11 (cmd 23
// word) + §3.5.7 (cmd 151 ext word).
//
// There is NO extended-byte form in the spec — when ANY of the byte-
// form's narrow ceilings is exceeded the encoder promotes to the word
// form, which itself promotes to the extended word form when matrix
// or level exceeds the 4-bit narrow packing.

// --- byte-form ceilings (cmd 22 narrow) -----------------------------

func TestTallyDumpByte_NarrowMin(t *testing.T) {
	in := CrosspointTallyDumpByteParams{
		MatrixID: 0, LevelID: 0,
		FirstDestinationID: 0,
		SourceIDs:          []uint8{0, 0, 0},
	}
	f := EncodeCrosspointTallyDumpByte(in)
	if f.ID != TxCrosspointTallyDumpByte {
		t.Errorf("min ID = %#x; want 0x16 (narrow byte)", f.ID)
	}
	want := []byte{0x00, 0x03, 0x00, 0x00, 0x00, 0x00}
	if !bytes.Equal(f.Payload, want) {
		t.Errorf("payload = %X; want %X", f.Payload, want)
	}
}

func TestTallyDumpByte_NarrowMax(t *testing.T) {
	// Spec §3.3.10 ceiling: matrix=15, level=15, firstdst=191, src=191.
	in := CrosspointTallyDumpByteParams{
		MatrixID: 15, LevelID: 15,
		FirstDestinationID: 191,
		SourceIDs:          []uint8{191, 0, 191},
	}
	f := EncodeCrosspointTallyDumpByte(in)
	if f.ID != TxCrosspointTallyDumpByte {
		t.Errorf("max-narrow ID = %#x; want 0x16 (still narrow byte)", f.ID)
	}
	want := []byte{0xFF, 0x03, 0xBF, 0xBF, 0x00, 0xBF}
	if !bytes.Equal(f.Payload, want) {
		t.Errorf("payload = %X; want %X", f.Payload, want)
	}
}

// --- byte → word promotion -------------------------------------------

func TestTallyDumpByte_PromoteOnDst(t *testing.T) {
	in := CrosspointTallyDumpByteParams{
		MatrixID: 0, LevelID: 0,
		FirstDestinationID: 192, // > 191 → must promote
		SourceIDs:          []uint8{0, 0},
	}
	f := EncodeCrosspointTallyDumpByte(in)
	if f.ID != TxCrosspointTallyDumpWord {
		t.Errorf("firstdst=192 ID = %#x; want 0x17 (promoted to narrow word; byte form caps at 191)", f.ID)
	}
	// Word narrow form: matrix/level=0x00, tallies=02, dst_hi=00, dst_lo=192, src0_hi=00, src0_lo=00, src1_hi=00, src1_lo=00
	want := []byte{0x00, 0x02, 0x00, 0xC0, 0x00, 0x00, 0x00, 0x00}
	if !bytes.Equal(f.Payload, want) {
		t.Errorf("payload = %X; want %X", f.Payload, want)
	}
}

func TestTallyDumpByte_PromoteOnSrc(t *testing.T) {
	in := CrosspointTallyDumpByteParams{
		MatrixID: 0, LevelID: 0,
		FirstDestinationID: 0,
		SourceIDs:          []uint8{50, 200, 100}, // 200 > 191 → must promote
	}
	f := EncodeCrosspointTallyDumpByte(in)
	if f.ID != TxCrosspointTallyDumpWord {
		t.Errorf("src=200 ID = %#x; want 0x17 (promoted; byte form caps at 191)", f.ID)
	}
	// Verify the payload has each source as 2 bytes.
	if len(f.Payload) != 4+2*3 {
		t.Errorf("word payload len = %d; want %d", len(f.Payload), 4+2*3)
	}
}

func TestTallyDumpByte_PromoteOnMatrix(t *testing.T) {
	in := CrosspointTallyDumpByteParams{
		MatrixID: 16, LevelID: 0,
		FirstDestinationID: 0,
		SourceIDs:          []uint8{1, 2},
	}
	f := EncodeCrosspointTallyDumpByte(in)
	if f.ID != TxCrosspointTallyDumpWordExt {
		t.Errorf("matrix=16 ID = %#x; want 0x97 (byte form promotes through narrow-word into extended-word)", f.ID)
	}
}

func TestTallyDumpByte_PromoteOnLevel(t *testing.T) {
	in := CrosspointTallyDumpByteParams{
		MatrixID: 0, LevelID: 16,
		FirstDestinationID: 0,
		SourceIDs:          []uint8{1},
	}
	f := EncodeCrosspointTallyDumpByte(in)
	if f.ID != TxCrosspointTallyDumpWordExt {
		t.Errorf("level=16 ID = %#x; want 0x97", f.ID)
	}
}

// --- word-form boundary tests ----------------------------------------

func TestTallyDumpWord_NarrowMaxStillNarrow(t *testing.T) {
	// Per spec §3.3.11 narrow word DOES support 0-65535 dst/src — only
	// matrix/level packing differs from extended. Verify dst=65535 +
	// src=65535 stays in narrow word.
	in := CrosspointTallyDumpWordParams{
		MatrixID: 15, LevelID: 15,
		FirstDestinationID: 65535,
		SourceIDs:          []uint16{65535, 0, 65535},
	}
	f := EncodeCrosspointTallyDumpWord(in)
	if f.ID != TxCrosspointTallyDumpWord {
		t.Errorf("max-narrow word ID = %#x; want 0x17 (matrix/level fit in narrow nibble; dst/src can be 0-65535)", f.ID)
	}
	want := []byte{0xFF, 0x03, 0xFF, 0xFF, 0xFF, 0xFF, 0x00, 0x00, 0xFF, 0xFF}
	if !bytes.Equal(f.Payload, want) {
		t.Errorf("payload = %X; want %X", f.Payload, want)
	}
}

func TestTallyDumpWord_PromoteOnMatrix(t *testing.T) {
	in := CrosspointTallyDumpWordParams{
		MatrixID: 16, LevelID: 0,
		FirstDestinationID: 100,
		SourceIDs:          []uint16{200, 300},
	}
	f := EncodeCrosspointTallyDumpWord(in)
	if f.ID != TxCrosspointTallyDumpWordExt {
		t.Errorf("matrix=16 ID = %#x; want 0x97 (extended)", f.ID)
	}
	want := []byte{16, 0, 0x02, 0x00, 0x64, 0x00, 0xC8, 0x01, 0x2C}
	if !bytes.Equal(f.Payload, want) {
		t.Errorf("payload = %X; want %X", f.Payload, want)
	}
}

func TestTallyDumpWord_ExtendedMax(t *testing.T) {
	in := CrosspointTallyDumpWordParams{
		MatrixID: 255, LevelID: 255,
		FirstDestinationID: 65535,
		SourceIDs:          []uint16{65535},
	}
	f := EncodeCrosspointTallyDumpWord(in)
	if f.ID != TxCrosspointTallyDumpWordExt {
		t.Fatalf("ID = %#x; want 0x97", f.ID)
	}
	want := []byte{0xFF, 0xFF, 0x01, 0xFF, 0xFF, 0xFF, 0xFF}
	if !bytes.Equal(f.Payload, want) {
		t.Errorf("payload = %X; want %X", f.Payload, want)
	}
}

// --- byte form decode + word-form-on-decode round-trip ---------------

func TestTallyDumpByte_DecodeRoundTrip(t *testing.T) {
	in := CrosspointTallyDumpByteParams{
		MatrixID: 5, LevelID: 3,
		FirstDestinationID: 100,
		SourceIDs:          []uint8{10, 20, 30, 40},
	}
	f := EncodeCrosspointTallyDumpByte(in)
	if f.ID != TxCrosspointTallyDumpByte {
		t.Fatalf("expected narrow byte form")
	}
	got, err := DecodeCrosspointTallyDumpByte(f)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !reflect.DeepEqual(got, in) {
		t.Errorf("round-trip: got %+v want %+v", got, in)
	}
}
