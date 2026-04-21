package codec

import (
	"reflect"
	"testing"
)

func TestTieLineInterrogateRoundtrip(t *testing.T) {
	in := TieLineInterrogateParams{MatrixID: 2, DestAssociationID: 300}
	f := EncodeTieLineInterrogate(in)
	if f.ID != RxCrosspointTieLineInterrogate {
		t.Errorf("ID = %#x", f.ID)
	}
	want := []byte{0x02, 0x01, 0x2C}
	if !reflect.DeepEqual(f.Payload, want) {
		t.Errorf("payload = %s; want %s", HexDump(f.Payload), HexDump(want))
	}
	back, err := DecodeTieLineInterrogate(f)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if back != in {
		t.Errorf("roundtrip: %+v != %+v", back, in)
	}
}

func TestTieLineTallyRoundtrip(t *testing.T) {
	in := TieLineTallyParams{
		DestMatrixID:      1,
		DestAssociationID: 5,
		Sources: []TieLineSource{
			{MatrixID: 0, LevelID: 0, SourceID: 10},
			{MatrixID: 0, LevelID: 1, SourceID: 11},
		},
	}
	f := EncodeTieLineTally(in)
	if f.ID != TxCrosspointTieLineTally {
		t.Errorf("ID = %#x", f.ID)
	}
	if len(f.Payload) != 4+len(in.Sources)*4 {
		t.Errorf("payload len = %d; want %d", len(f.Payload), 4+len(in.Sources)*4)
	}
	back, err := DecodeTieLineTally(f)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !reflect.DeepEqual(back, in) {
		t.Errorf("roundtrip: %+v != %+v", back, in)
	}
}
