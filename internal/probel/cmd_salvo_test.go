package probel

import (
	"reflect"
	"testing"
)

func TestSalvoConnectOnGoRoundtrip(t *testing.T) {
	in := SalvoConnectOnGoParams{MatrixID: 1, LevelID: 0, DestinationID: 5, SourceID: 12, SalvoID: 3}
	f := EncodeSalvoConnectOnGo(in)
	if f.ID != RxCrosspointConnectOnGoSalvo {
		t.Errorf("ID = %#x; want 0x78", f.ID)
	}
	back, err := DecodeSalvoConnectOnGo(f)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !reflect.DeepEqual(back, in) {
		t.Errorf("roundtrip: %+v != %+v", back, in)
	}
}

func TestSalvoGoRoundtrip(t *testing.T) {
	in := SalvoGoParams{Op: SalvoOpSet, SalvoID: 7}
	f := EncodeSalvoGo(in)
	if f.Payload[0] != 0x00 || f.Payload[1] != 0x07 {
		t.Errorf("payload = %s; want 00 07", HexDump(f.Payload))
	}
	back, err := DecodeSalvoGo(f)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if back != in {
		t.Errorf("roundtrip: %+v != %+v", back, in)
	}
}

func TestSalvoGoRejectsUnknownOp(t *testing.T) {
	f := Frame{ID: RxCrosspointGoSalvo, Payload: []byte{0x99, 0x00}}
	if _, err := DecodeSalvoGo(f); err == nil {
		t.Error("want error on unknown op")
	}
}

func TestSalvoGroupInterrogateRoundtrip(t *testing.T) {
	in := SalvoGroupInterrogateParams{SalvoID: 2, ConnectIndex: 4}
	f := EncodeSalvoGroupInterrogate(in)
	back, err := DecodeSalvoGroupInterrogate(f)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if back != in {
		t.Errorf("roundtrip: %+v != %+v", back, in)
	}
}

func TestSalvoGoDoneAckRoundtrip(t *testing.T) {
	for _, st := range []SalvoGoDoneStatus{SalvoDoneSet, SalvoDoneCleared, SalvoDoneNone} {
		f := EncodeSalvoGoDoneAck(SalvoGoDoneAckParams{Status: st, SalvoID: 1})
		back, err := DecodeSalvoGoDoneAck(f)
		if err != nil {
			t.Fatalf("status=%d: decode: %v", st, err)
		}
		if back.Status != st || back.SalvoID != 1 {
			t.Errorf("status=%d: got %+v", st, back)
		}
	}
}

func TestSalvoGroupTallyRoundtrip(t *testing.T) {
	in := SalvoGroupTallyParams{
		MatrixID: 0, LevelID: 0, DestinationID: 5, SourceID: 12,
		SalvoID: 3, ConnectIndex: 0, Validity: SalvoTallyValidMore,
	}
	f := EncodeSalvoGroupTally(in)
	back, err := DecodeSalvoGroupTally(f)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !reflect.DeepEqual(back, in) {
		t.Errorf("roundtrip: %+v != %+v", back, in)
	}
}
