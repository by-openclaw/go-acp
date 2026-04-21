package codec

import (
	"reflect"
	"testing"
)

func TestAllSourceAssocNamesRequestRoundtrip(t *testing.T) {
	in := AllSourceAssocNamesRequestParams{MatrixID: 2, NameLength: NameLen12}
	f := EncodeAllSourceAssocNamesRequest(in)
	if f.ID != RxAllSourceAssocNamesRequest {
		t.Errorf("ID = %#x; want 0x72", f.ID)
	}
	want := []byte{0x02, 0x02}
	if !reflect.DeepEqual(f.Payload, want) {
		t.Errorf("payload = %s; want %s", HexDump(f.Payload), HexDump(want))
	}
	back, err := DecodeAllSourceAssocNamesRequest(f)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if back != in {
		t.Errorf("roundtrip: %+v != %+v", back, in)
	}
}

func TestSingleSourceAssocNameRequestRoundtrip(t *testing.T) {
	in := SingleSourceAssocNameRequestParams{MatrixID: 1, NameLength: NameLen8, SourceAssociationID: 500}
	f := EncodeSingleSourceAssocNameRequest(in)
	want := []byte{0x01, 0x01, 0x01, 0xF4}
	if !reflect.DeepEqual(f.Payload, want) {
		t.Errorf("payload = %s; want %s", HexDump(f.Payload), HexDump(want))
	}
	back, err := DecodeSingleSourceAssocNameRequest(f)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if back != in {
		t.Errorf("roundtrip: %+v != %+v", back, in)
	}
}

func TestSourceAssocNamesResponseRoundtrip(t *testing.T) {
	in := SourceAssocNamesResponseParams{
		MatrixID:                1,
		LevelID:                 0,
		NameLength:              NameLen4,
		FirstSourceAssociationID: 10,
		Names:                   []string{"A", "BB", "CCC"},
	}
	f := EncodeSourceAssocNamesResponse(in)
	back, err := DecodeSourceAssocNamesResponse(f)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if back.MatrixID != 1 || back.LevelID != 0 || back.NameLength != NameLen4 ||
		back.FirstSourceAssociationID != 10 {
		t.Errorf("header: %+v", back)
	}
	if len(back.Names) != 3 {
		t.Fatalf("got %d names; want 3", len(back.Names))
	}
}
