package codec

import (
	"bytes"
	"reflect"
	"testing"
)

// Boundary tests for the tie-line pair (rx 112 / tx 113). Per SW-P-08
// Issue 30 §3.2.28 + §3.3.23 the tie-line family has NO extended pair
// in the spec — both rx 112 (cmd 0x70) and tx 113 (cmd 0x71) already
// use full-byte matrix and 16-bit IDs in their narrow form. These
// tests pin that the wire form correctly carries the full type range
// (matrix 0-255, dst_assoc 0-65535, per-source matrix/level 0-255,
// src 0-65535) and round-trips faithfully.

// --- rx 112 ----------------------------------------------------------

func TestTieLineInterrogate_Min(t *testing.T) {
	in := TieLineInterrogateParams{MatrixID: 0, DestAssociationID: 0}
	f := EncodeTieLineInterrogate(in)
	if f.ID != RxCrosspointTieLineInterrogate {
		t.Errorf("ID = %#x; want 0x70", f.ID)
	}
	want := []byte{0x00, 0x00, 0x00}
	if !bytes.Equal(f.Payload, want) {
		t.Errorf("payload = %X; want %X", f.Payload, want)
	}
}

func TestTieLineInterrogate_FullByteMatrix(t *testing.T) {
	// The spec quotes a device range of 0-19 but the wire byte is full
	// 8-bit, so the codec must accept and emit any uint8 value verbatim.
	in := TieLineInterrogateParams{MatrixID: 200, DestAssociationID: 0}
	f := EncodeTieLineInterrogate(in)
	if f.ID != RxCrosspointTieLineInterrogate {
		t.Errorf("matrix=200 ID = %#x; want 0x70 (no extended pair)", f.ID)
	}
	if f.Payload[0] != 200 {
		t.Errorf("matrix byte = %#x; want 200", f.Payload[0])
	}
	back, err := DecodeTieLineInterrogate(f)
	if err != nil || back != in {
		t.Errorf("round-trip: got %+v err %v", back, err)
	}
}

func TestTieLineInterrogate_Max(t *testing.T) {
	in := TieLineInterrogateParams{MatrixID: 255, DestAssociationID: 65535}
	f := EncodeTieLineInterrogate(in)
	if f.ID != RxCrosspointTieLineInterrogate {
		t.Fatalf("ID = %#x; want 0x70", f.ID)
	}
	want := []byte{0xFF, 0xFF, 0xFF}
	if !bytes.Equal(f.Payload, want) {
		t.Errorf("payload = %X; want %X", f.Payload, want)
	}
	back, err := DecodeTieLineInterrogate(f)
	if err != nil || back != in {
		t.Errorf("round-trip: got %+v err %v", back, err)
	}
}

// --- tx 113 ----------------------------------------------------------

func TestTieLineTally_NoSources(t *testing.T) {
	// Spec §3.3.23: the per-source quad is repeated `Num Srcs` times
	// (byte 4). An empty source list is legal — header is 4 bytes,
	// payload total is 4 bytes.
	in := TieLineTallyParams{DestMatrixID: 0, DestAssociationID: 0, Sources: nil}
	f := EncodeTieLineTally(in)
	if f.ID != TxCrosspointTieLineTally {
		t.Errorf("ID = %#x; want 0x71", f.ID)
	}
	want := []byte{0x00, 0x00, 0x00, 0x00}
	if !bytes.Equal(f.Payload, want) {
		t.Errorf("payload = %X; want %X", f.Payload, want)
	}
	back, err := DecodeTieLineTally(f)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if back.DestMatrixID != 0 || back.DestAssociationID != 0 || len(back.Sources) != 0 {
		t.Errorf("round-trip mismatch: %+v", back)
	}
}

func TestTieLineTally_FullByteMatrix(t *testing.T) {
	// All matrix-id slots are full 8-bit bytes — DestMatrixID and the
	// per-source MatrixID. Pin that the codec accepts values past the
	// spec device limit of 19 verbatim.
	in := TieLineTallyParams{
		DestMatrixID:      200,
		DestAssociationID: 1234,
		Sources: []TieLineSource{
			{MatrixID: 200, LevelID: 0, SourceID: 50},
			{MatrixID: 200, LevelID: 1, SourceID: 51},
		},
	}
	f := EncodeTieLineTally(in)
	if f.ID != TxCrosspointTieLineTally {
		t.Errorf("ID = %#x; want 0x71", f.ID)
	}
	want := []byte{
		200, 0x04, 0xD2, 0x02, // dest matrix=200, dst_assoc=1234, num srcs=2
		200, 0, 0x00, 50, // src 0
		200, 1, 0x00, 51, // src 1
	}
	if !bytes.Equal(f.Payload, want) {
		t.Errorf("payload = %X; want %X", f.Payload, want)
	}
	back, err := DecodeTieLineTally(f)
	if err != nil || !reflect.DeepEqual(back, in) {
		t.Errorf("round-trip: got %+v err %v", back, err)
	}
}

func TestTieLineTally_Max(t *testing.T) {
	// Hammer every numeric field at the type maximum the wire form
	// supports. Matrix 0xFF, dst_assoc 0xFFFF, per-source matrix/level
	// 0xFF, src 0xFFFF.
	in := TieLineTallyParams{
		DestMatrixID:      255,
		DestAssociationID: 65535,
		Sources: []TieLineSource{
			{MatrixID: 255, LevelID: 255, SourceID: 65535},
		},
	}
	f := EncodeTieLineTally(in)
	if f.ID != TxCrosspointTieLineTally {
		t.Fatalf("ID = %#x; want 0x71", f.ID)
	}
	want := []byte{0xFF, 0xFF, 0xFF, 0x01, 0xFF, 0xFF, 0xFF, 0xFF}
	if !bytes.Equal(f.Payload, want) {
		t.Errorf("payload = %X; want %X", f.Payload, want)
	}
	back, err := DecodeTieLineTally(f)
	if err != nil || !reflect.DeepEqual(back, in) {
		t.Errorf("round-trip: got %+v err %v", back, err)
	}
}

func TestTieLineTally_DecodeShortPayload(t *testing.T) {
	// Header claims 1 source, but only 2 bytes follow — short.
	short := Frame{ID: TxCrosspointTieLineTally, Payload: []byte{0x01, 0x00, 0x00, 0x01, 0x00, 0x00}}
	if _, err := DecodeTieLineTally(short); err == nil {
		t.Error("expected error on short payload")
	}
}
