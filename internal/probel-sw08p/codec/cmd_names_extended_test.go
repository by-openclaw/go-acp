package codec

import (
	"bytes"
	"reflect"
	"testing"
)

// Boundary tests for the names family: rx 100/101/102/103 + tx 106/107
// + their extended counterparts (rx 228/229/230/231 + tx 234/235).
//
// Form-selection rule (per SW-P-08 §3.1.2):
//   - rx 100 / rx 101 / tx 106 / tx 107  : matrix > 15 OR level > 15 → ext
//   - rx 102 / rx 103                    : matrix > 15               → ext
//
// SourceID (rx 101 / tx 106) and DestAssociationID (rx 103 / tx 107)
// are 16-bit in BOTH narrow and extended forms, so they do not promote
// on their own.

// --- rx 100 / rx 228 -------------------------------------------------

func TestAllSourceNamesRequest_NarrowMin(t *testing.T) {
	in := AllSourceNamesRequestParams{MatrixID: 0, LevelID: 0, NameLength: NameLen4}
	f := EncodeAllSourceNamesRequest(in)
	if f.ID != RxAllSourceNamesRequest {
		t.Errorf("min-narrow ID = %#x; want 0x64 (general)", f.ID)
	}
	want := []byte{0x00, 0x00}
	if !bytes.Equal(f.Payload, want) {
		t.Errorf("payload = %X; want %X", f.Payload, want)
	}
	roundtripAllSrcNames(t, f, in, "narrow-min")
}

func TestAllSourceNamesRequest_NarrowMax(t *testing.T) {
	in := AllSourceNamesRequestParams{MatrixID: 15, LevelID: 15, NameLength: NameLen12}
	f := EncodeAllSourceNamesRequest(in)
	if f.ID != RxAllSourceNamesRequest {
		t.Errorf("max-narrow ID = %#x; want 0x64 (general, no promotion)", f.ID)
	}
	want := []byte{0xFF, 0x02} // matrix(15)<<4|level(15) = 0xFF; NameLen12 = 0x02
	if !bytes.Equal(f.Payload, want) {
		t.Errorf("payload = %X; want %X", f.Payload, want)
	}
	roundtripAllSrcNames(t, f, in, "narrow-max")
}

func TestAllSourceNamesRequest_PromoteOnMatrix(t *testing.T) {
	in := AllSourceNamesRequestParams{MatrixID: 16, LevelID: 0, NameLength: NameLen8}
	f := EncodeAllSourceNamesRequest(in)
	if f.ID != RxAllSourceNamesRequestExt {
		t.Errorf("matrix=16 ID = %#x; want 0xE4 (extended)", f.ID)
	}
	want := []byte{16, 0, 0x01}
	if !bytes.Equal(f.Payload, want) {
		t.Errorf("payload = %X; want %X", f.Payload, want)
	}
	roundtripAllSrcNames(t, f, in, "promote-matrix")
}

func TestAllSourceNamesRequest_PromoteOnLevel(t *testing.T) {
	in := AllSourceNamesRequestParams{MatrixID: 0, LevelID: 16, NameLength: NameLen4}
	f := EncodeAllSourceNamesRequest(in)
	if f.ID != RxAllSourceNamesRequestExt {
		t.Errorf("level=16 ID = %#x; want 0xE4", f.ID)
	}
	roundtripAllSrcNames(t, f, in, "promote-level")
}

func TestAllSourceNamesRequest_ExtendedMax(t *testing.T) {
	in := AllSourceNamesRequestParams{MatrixID: 255, LevelID: 255, NameLength: NameLen12}
	f := EncodeAllSourceNamesRequest(in)
	if f.ID != RxAllSourceNamesRequestExt {
		t.Errorf("max-ext ID = %#x; want 0xE4", f.ID)
	}
	want := []byte{0xFF, 0xFF, 0x02}
	if !bytes.Equal(f.Payload, want) {
		t.Errorf("payload = %X; want %X", f.Payload, want)
	}
	roundtripAllSrcNames(t, f, in, "extended-max")
}

// --- rx 101 / rx 229 -------------------------------------------------

func TestSingleSourceNameRequest_NarrowMaxStillNarrow(t *testing.T) {
	// SourceID is 16-bit in narrow form already — never triggers ext.
	in := SingleSourceNameRequestParams{MatrixID: 15, LevelID: 15, NameLength: NameLen8, SourceID: 65535}
	f := EncodeSingleSourceNameRequest(in)
	if f.ID != RxSingleSourceNameRequest {
		t.Errorf("ID = %#x; want 0x65 (narrow accepts SourceID up to 65535)", f.ID)
	}
	want := []byte{0xFF, 0x01, 0xFF, 0xFF}
	if !bytes.Equal(f.Payload, want) {
		t.Errorf("payload = %X; want %X", f.Payload, want)
	}
}

func TestSingleSourceNameRequest_PromoteOnMatrix(t *testing.T) {
	in := SingleSourceNameRequestParams{MatrixID: 16, LevelID: 0, NameLength: NameLen4, SourceID: 100}
	f := EncodeSingleSourceNameRequest(in)
	if f.ID != RxSingleSourceNameRequestExt {
		t.Errorf("matrix=16 ID = %#x; want 0xE5", f.ID)
	}
	want := []byte{16, 0, 0x00, 0x00, 100}
	if !bytes.Equal(f.Payload, want) {
		t.Errorf("payload = %X; want %X", f.Payload, want)
	}
	back, err := DecodeSingleSourceNameRequest(f)
	if err != nil || !reflect.DeepEqual(back, in) {
		t.Errorf("round-trip: got %+v err %v want %+v", back, err, in)
	}
}

func TestSingleSourceNameRequest_ExtendedMax(t *testing.T) {
	in := SingleSourceNameRequestParams{MatrixID: 255, LevelID: 255, NameLength: NameLen12, SourceID: 65535}
	f := EncodeSingleSourceNameRequest(in)
	if f.ID != RxSingleSourceNameRequestExt {
		t.Fatalf("ID = %#x; want 0xE5", f.ID)
	}
	want := []byte{0xFF, 0xFF, 0x02, 0xFF, 0xFF}
	if !bytes.Equal(f.Payload, want) {
		t.Errorf("payload = %X; want %X", f.Payload, want)
	}
}

// --- rx 102 / rx 230 -------------------------------------------------

func TestAllDestAssocNamesRequest_NarrowMaxStillNarrow(t *testing.T) {
	in := AllDestAssocNamesRequestParams{MatrixID: 15, NameLength: NameLen12}
	f := EncodeAllDestAssocNamesRequest(in)
	if f.ID != RxAllDestNamesRequest {
		t.Errorf("matrix=15 ID = %#x; want 0x66", f.ID)
	}
	want := []byte{0x0F, 0x02} // matrix is bits 0-3 only in narrow; bits 4-7 unused
	if !bytes.Equal(f.Payload, want) {
		t.Errorf("payload = %X; want %X", f.Payload, want)
	}
}

func TestAllDestAssocNamesRequest_PromoteOnMatrix(t *testing.T) {
	in := AllDestAssocNamesRequestParams{MatrixID: 16, NameLength: NameLen4}
	f := EncodeAllDestAssocNamesRequest(in)
	if f.ID != RxAllDestNamesRequestExt {
		t.Errorf("matrix=16 ID = %#x; want 0xE6", f.ID)
	}
	want := []byte{16, 0x00}
	if !bytes.Equal(f.Payload, want) {
		t.Errorf("payload = %X; want %X", f.Payload, want)
	}
	back, err := DecodeAllDestAssocNamesRequest(f)
	if err != nil || back != in {
		t.Errorf("round-trip: got %+v err %v", back, err)
	}
}

// --- rx 103 / rx 231 -------------------------------------------------

func TestSingleDestAssocNameRequest_NarrowMaxStillNarrow(t *testing.T) {
	// AssocID 16-bit in narrow — never triggers ext.
	in := SingleDestAssocNameRequestParams{MatrixID: 15, NameLength: NameLen8, DestAssociationID: 65535}
	f := EncodeSingleDestAssocNameRequest(in)
	if f.ID != RxSingleDestNameRequest {
		t.Errorf("ID = %#x; want 0x67 (narrow accepts AssocID up to 65535)", f.ID)
	}
	want := []byte{0x0F, 0x01, 0xFF, 0xFF}
	if !bytes.Equal(f.Payload, want) {
		t.Errorf("payload = %X; want %X", f.Payload, want)
	}
}

func TestSingleDestAssocNameRequest_PromoteOnMatrix(t *testing.T) {
	in := SingleDestAssocNameRequestParams{MatrixID: 16, NameLength: NameLen4, DestAssociationID: 1234}
	f := EncodeSingleDestAssocNameRequest(in)
	if f.ID != RxSingleDestNameRequestExt {
		t.Errorf("matrix=16 ID = %#x; want 0xE7", f.ID)
	}
	want := []byte{16, 0x00, 0x04, 0xD2} // 1234 = 4*256 + 210 (0xD2)
	if !bytes.Equal(f.Payload, want) {
		t.Errorf("payload = %X; want %X", f.Payload, want)
	}
	back, err := DecodeSingleDestAssocNameRequest(f)
	if err != nil || back != in {
		t.Errorf("round-trip: got %+v err %v", back, err)
	}
}

// --- tx 106 / tx 234 -------------------------------------------------

func TestSourceNamesResponse_PromoteOnMatrix(t *testing.T) {
	in := SourceNamesResponseParams{
		MatrixID: 16, LevelID: 0, NameLength: NameLen4,
		FirstSourceID: 0,
		Names:         []string{"A", "B"},
	}
	f := EncodeSourceNamesResponse(in)
	if f.ID != TxSourceNamesResponseExt {
		t.Errorf("matrix=16 ID = %#x; want 0xEA", f.ID)
	}
	// Payload prefix: matrix(16), level(0), namelen(0=4-char), firstsrc/256, firstsrc%256, count=2,
	// then 2 × 4-char names "A   " "B   ".
	if len(f.Payload) != 6+2*4 {
		t.Fatalf("ext payload len = %d; want %d", len(f.Payload), 6+2*4)
	}
	if f.Payload[0] != 16 || f.Payload[1] != 0 {
		t.Errorf("matrix/level prefix = %X / %X; want 10 / 00", f.Payload[0], f.Payload[1])
	}
	back, err := DecodeSourceNamesResponse(f)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if back.MatrixID != 16 || back.LevelID != 0 || len(back.Names) != 2 {
		t.Errorf("round-trip mismatch: %+v", back)
	}
}

func TestSourceNamesResponse_NarrowMaxStillNarrow(t *testing.T) {
	in := SourceNamesResponseParams{MatrixID: 15, LevelID: 15, NameLength: NameLen8, FirstSourceID: 0, Names: []string{"X"}}
	f := EncodeSourceNamesResponse(in)
	if f.ID != TxSourceNamesResponse {
		t.Errorf("ID = %#x; want 0x6A", f.ID)
	}
}

// --- tx 107 / tx 235 -------------------------------------------------

func TestDestAssocNamesResponse_PromoteOnMatrix(t *testing.T) {
	in := DestAssocNamesResponseParams{
		MatrixID: 16, LevelID: 0, NameLength: NameLen4,
		FirstDestAssociationID: 0,
		Names:                  []string{"D"},
	}
	f := EncodeDestAssocNamesResponse(in)
	if f.ID != TxDestAssocNamesResponseExt {
		t.Errorf("matrix=16 ID = %#x; want 0xEB", f.ID)
	}
	back, err := DecodeDestAssocNamesResponse(f)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if back.MatrixID != 16 || back.LevelID != 0 || len(back.Names) != 1 {
		t.Errorf("round-trip mismatch: %+v", back)
	}
}

// --- helper ----------------------------------------------------------

func roundtripAllSrcNames(t *testing.T, f Frame, want AllSourceNamesRequestParams, label string) {
	t.Helper()
	got, err := DecodeAllSourceNamesRequest(f)
	if err != nil {
		t.Errorf("%s: decode: %v", label, err)
		return
	}
	if got != want {
		t.Errorf("%s: round-trip got %+v want %+v", label, got, want)
	}
}
