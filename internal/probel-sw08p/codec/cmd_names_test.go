package codec

import (
	"reflect"
	"testing"
)

func TestAllSourceNamesRequestRoundtrip(t *testing.T) {
	in := AllSourceNamesRequestParams{MatrixID: 2, LevelID: 1, NameLength: NameLen8}
	f := EncodeAllSourceNamesRequest(in)
	if f.ID != RxAllSourceNamesRequest {
		t.Errorf("ID = %#x; want 0x64", f.ID)
	}
	if len(f.Payload) != 2 {
		t.Fatalf("payload len = %d; want 2", len(f.Payload))
	}
	// Matrix=2 Level=1 → 0x21; NameLen8 → 0x01
	if f.Payload[0] != 0x21 || f.Payload[1] != 0x01 {
		t.Errorf("payload = %s; want 21 01", HexDump(f.Payload))
	}
	back, err := DecodeAllSourceNamesRequest(f)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if back != in {
		t.Errorf("roundtrip mismatch: %+v != %+v", back, in)
	}
}

func TestSingleSourceNameRequestRoundtrip(t *testing.T) {
	in := SingleSourceNameRequestParams{MatrixID: 0, LevelID: 0, NameLength: NameLen12, SourceID: 300}
	f := EncodeSingleSourceNameRequest(in)
	if f.ID != RxSingleSourceNameRequest {
		t.Errorf("ID = %#x", f.ID)
	}
	// Payload: 00 02 01 2C (300 = 0x012C)
	want := []byte{0x00, 0x02, 0x01, 0x2C}
	if !reflect.DeepEqual(f.Payload, want) {
		t.Errorf("payload = %s; want %s", HexDump(f.Payload), HexDump(want))
	}
	back, err := DecodeSingleSourceNameRequest(f)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if back != in {
		t.Errorf("roundtrip: %+v != %+v", back, in)
	}
}

func TestSourceNamesResponseRoundtrip(t *testing.T) {
	in := SourceNamesResponseParams{
		MatrixID:      0,
		LevelID:       0,
		NameLength:    NameLen8,
		FirstSourceID: 0,
		Names:         []string{"CAM 1", "CAM 2", "MIC"},
	}
	f := EncodeSourceNamesResponse(in)
	back, err := DecodeSourceNamesResponse(f)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if back.MatrixID != in.MatrixID || back.LevelID != in.LevelID ||
		back.NameLength != in.NameLength || back.FirstSourceID != in.FirstSourceID {
		t.Errorf("header mismatch: %+v", back)
	}
	if len(back.Names) != 3 {
		t.Fatalf("got %d names; want 3", len(back.Names))
	}
	for i, want := range []string{"CAM 1", "CAM 2", "MIC"} {
		if back.Names[i] != want {
			t.Errorf("Names[%d] = %q; want %q", i, back.Names[i], want)
		}
	}
}

func TestSourceNamesResponseCapsAtMaxPerFrame(t *testing.T) {
	// 8-char names -> max 16 per frame.
	names := make([]string, 20)
	for i := range names {
		names[i] = "X"
	}
	f := EncodeSourceNamesResponse(SourceNamesResponseParams{
		NameLength: NameLen8,
		Names:      names,
	})
	// Count byte is at payload offset 4; must be 16 (max), not 20.
	if f.Payload[4] != 16 {
		t.Errorf("num-names byte = %d; want 16", f.Payload[4])
	}
}

func TestAllDestAssocNamesRequestRoundtrip(t *testing.T) {
	in := AllDestAssocNamesRequestParams{MatrixID: 3, NameLength: NameLen4}
	f := EncodeAllDestAssocNamesRequest(in)
	if f.ID != RxAllDestNamesRequest {
		t.Errorf("ID = %#x", f.ID)
	}
	if f.Payload[0] != 0x03 || f.Payload[1] != 0x00 {
		t.Errorf("payload = %s; want 03 00", HexDump(f.Payload))
	}
	back, err := DecodeAllDestAssocNamesRequest(f)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if back != in {
		t.Errorf("roundtrip: %+v != %+v", back, in)
	}
}

func TestSingleDestAssocNameRequestRoundtrip(t *testing.T) {
	in := SingleDestAssocNameRequestParams{MatrixID: 1, NameLength: NameLen4, DestAssociationID: 42}
	f := EncodeSingleDestAssocNameRequest(in)
	want := []byte{0x01, 0x00, 0x00, 0x2A}
	if !reflect.DeepEqual(f.Payload, want) {
		t.Errorf("payload = %s; want %s", HexDump(f.Payload), HexDump(want))
	}
	back, err := DecodeSingleDestAssocNameRequest(f)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if back != in {
		t.Errorf("roundtrip: %+v != %+v", back, in)
	}
}

func TestDestAssocNamesResponseRoundtrip(t *testing.T) {
	in := DestAssocNamesResponseParams{
		MatrixID:              1,
		LevelID:               0,
		NameLength:            NameLen4,
		FirstDestAssociationID: 0,
		Names:                 []string{"OUT1", "OUT2"},
	}
	f := EncodeDestAssocNamesResponse(in)
	back, err := DecodeDestAssocNamesResponse(f)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if back.MatrixID != in.MatrixID || back.LevelID != in.LevelID ||
		back.NameLength != in.NameLength || back.FirstDestAssociationID != in.FirstDestAssociationID {
		t.Errorf("header: %+v", back)
	}
	if len(back.Names) != 2 || back.Names[0] != "OUT1" || back.Names[1] != "OUT2" {
		t.Errorf("Names = %v; want [OUT1 OUT2]", back.Names)
	}
}

func TestPackNameTruncatesAndPads(t *testing.T) {
	got := packName("HELLO", 4)
	if string(got) != "HELL" {
		t.Errorf("packName(\"HELLO\", 4) = %q; want HELL", got)
	}
	got = packName("A", 4)
	if string(got) != "A   " {
		t.Errorf("packName(\"A\", 4) = %q; want \"A   \"", got)
	}
	got = packName("", 4)
	if string(got) != "    " {
		t.Errorf("packName(\"\", 4) = %q; want 4 spaces", got)
	}
}

func TestValidateNameLengthRejectsUnknown(t *testing.T) {
	if err := validateNameLength(NameLength(5)); err == nil {
		t.Error("validateNameLength(5): want error")
	}
}
