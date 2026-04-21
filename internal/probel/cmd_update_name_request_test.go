package probel

import (
	"reflect"
	"testing"
)

func TestUpdateNameRequestRoundtrip(t *testing.T) {
	in := UpdateNameRequestParams{
		NameType:   UpdateNameSource,
		NameLength: NameLen4,
		MatrixID:   0,
		LevelID:    1,
		FirstID:    10,
		Names:      []string{"CAM1", "CAM2"},
	}
	f := EncodeUpdateNameRequest(in)
	if f.ID != RxUpdateNameRequest {
		t.Errorf("ID = %#x; want 0x75", f.ID)
	}
	back, err := DecodeUpdateNameRequest(f)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !reflect.DeepEqual(back, in) {
		t.Errorf("roundtrip: %+v != %+v", back, in)
	}
}

func TestUpdateNameRequest16Char(t *testing.T) {
	in := UpdateNameRequestParams{
		NameType:   UpdateNameUMDLabel,
		NameLength: NameLen16,
		MatrixID:   0,
		LevelID:    0,
		FirstID:    0,
		Names:      []string{"0123456789ABCDEF"},
	}
	f := EncodeUpdateNameRequest(in)
	back, err := DecodeUpdateNameRequest(f)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(back.Names) != 1 || back.Names[0] != "0123456789ABCDEF" {
		t.Errorf("Names = %v; want [0123456789ABCDEF]", back.Names)
	}
}

func TestUpdateNameRequestRejectsUnknownType(t *testing.T) {
	f := Frame{ID: RxUpdateNameRequest, Payload: []byte{0x99, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}}
	if _, err := DecodeUpdateNameRequest(f); err == nil {
		t.Error("want error on unknown NameType 0x99")
	}
}

func TestUpdateNameLenExt(t *testing.T) {
	// Base NameLength validation rejects 16; extended accepts.
	if err := validateNameLength(NameLen16); err == nil {
		t.Error("validateNameLength: want error on NameLen16")
	}
	if err := validateNameLengthExt(NameLen16); err != nil {
		t.Errorf("validateNameLengthExt: %v; want nil", err)
	}
}
