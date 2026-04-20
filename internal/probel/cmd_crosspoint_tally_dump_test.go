package probel

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
)

func TestTallyDumpRequest_General(t *testing.T) {
	p := CrosspointTallyDumpRequestParams{MatrixID: 1, LevelID: 2}
	f := EncodeCrosspointTallyDumpRequest(p)
	if f.ID != RxCrosspointTallyDumpRequest {
		t.Errorf("ID got %#x", f.ID)
	}
	if !bytes.Equal(f.Payload, []byte{0x12}) {
		t.Errorf("Payload got %X", f.Payload)
	}
	got, err := DecodeCrosspointTallyDumpRequest(f)
	if err != nil || got != p {
		t.Errorf("round-trip: got %+v err %v", got, err)
	}
}

func TestTallyDumpRequest_Extended(t *testing.T) {
	p := CrosspointTallyDumpRequestParams{MatrixID: 200, LevelID: 30}
	f := EncodeCrosspointTallyDumpRequest(p)
	if f.ID != RxCrosspointTallyDumpRequestExt {
		t.Errorf("ID got %#x", f.ID)
	}
	if !bytes.Equal(f.Payload, []byte{200, 30}) {
		t.Errorf("Payload got %X", f.Payload)
	}
	got, err := DecodeCrosspointTallyDumpRequest(f)
	if err != nil || got != p {
		t.Errorf("round-trip: got %+v err %v", got, err)
	}
}

func TestTallyDumpByte_RoundTrip(t *testing.T) {
	p := CrosspointTallyDumpByteParams{
		MatrixID:           2,
		LevelID:            3,
		FirstDestinationID: 10,
		SourceIDs:          []uint8{100, 101, 102, 103},
	}
	f := EncodeCrosspointTallyDumpByte(p)
	if f.ID != TxCrosspointTallyDumpByte {
		t.Errorf("ID got %#x", f.ID)
	}
	// matrix/level = 0x23, tallies=4, firstDest=10, then 100,101,102,103
	want := []byte{0x23, 0x04, 0x0A, 100, 101, 102, 103}
	if !bytes.Equal(f.Payload, want) {
		t.Errorf("Payload got %X want %X", f.Payload, want)
	}
	got, err := DecodeCrosspointTallyDumpByte(f)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.MatrixID != p.MatrixID || got.LevelID != p.LevelID ||
		got.FirstDestinationID != p.FirstDestinationID ||
		!reflect.DeepEqual(got.SourceIDs, p.SourceIDs) {
		t.Errorf("round-trip: got %+v want %+v", got, p)
	}
}

func TestTallyDumpByte_Framer(t *testing.T) {
	p := CrosspointTallyDumpByteParams{
		MatrixID: 0, LevelID: 0, FirstDestinationID: 0,
		SourceIDs: []uint8{1, 2, 3, 4, 5},
	}
	wire := Pack(EncodeCrosspointTallyDumpByte(p))
	back, _, err := Unpack(wire)
	if err != nil {
		t.Fatalf("Unpack: %v", err)
	}
	got, err := DecodeCrosspointTallyDumpByte(back)
	if err != nil || !reflect.DeepEqual(got.SourceIDs, p.SourceIDs) {
		t.Errorf("framer round-trip got %+v err %v", got, err)
	}
}

func TestTallyDumpWord_General(t *testing.T) {
	p := CrosspointTallyDumpWordParams{
		MatrixID:           1,
		LevelID:            2,
		FirstDestinationID: 500,
		SourceIDs:          []uint16{1000, 1500},
	}
	f := EncodeCrosspointTallyDumpWord(p)
	if f.ID != TxCrosspointTallyDumpWord {
		t.Errorf("ID got %#x", f.ID)
	}
	// 500 = 01F4 (01 F4); 1000 = 03 E8; 1500 = 05 DC
	want := []byte{0x12, 0x02, 0x01, 0xF4, 0x03, 0xE8, 0x05, 0xDC}
	if !bytes.Equal(f.Payload, want) {
		t.Errorf("Payload got %X want %X", f.Payload, want)
	}
	got, err := DecodeCrosspointTallyDumpWord(f)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.MatrixID != p.MatrixID || got.FirstDestinationID != p.FirstDestinationID ||
		!reflect.DeepEqual(got.SourceIDs, p.SourceIDs) {
		t.Errorf("round-trip: got %+v want %+v", got, p)
	}
}

func TestTallyDumpWord_Extended(t *testing.T) {
	p := CrosspointTallyDumpWordParams{
		MatrixID:           30,
		LevelID:            4,
		FirstDestinationID: 1000,
		SourceIDs:          []uint16{50000, 65535},
	}
	f := EncodeCrosspointTallyDumpWord(p)
	if f.ID != TxCrosspointTallyDumpWordExt {
		t.Errorf("ID got %#x", f.ID)
	}
	// matrix=30, level=4, tallies=2, dest=03 E8, src0=C3 50, src1=FF FF
	want := []byte{30, 4, 2, 0x03, 0xE8, 0xC3, 0x50, 0xFF, 0xFF}
	if !bytes.Equal(f.Payload, want) {
		t.Errorf("Payload got %X want %X", f.Payload, want)
	}
	got, err := DecodeCrosspointTallyDumpWord(f)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.MatrixID != p.MatrixID || !reflect.DeepEqual(got.SourceIDs, p.SourceIDs) {
		t.Errorf("round-trip: got %+v want %+v", got, p)
	}
}

func TestTallyDump_DecodeErrors(t *testing.T) {
	if _, err := DecodeCrosspointTallyDumpByte(Frame{ID: TxCrosspointTally}); !errors.Is(err, ErrWrongCommand) {
		t.Errorf("byte wrong: %v", err)
	}
	// Tallies claims 3 but only 2 bytes follow
	short := Frame{ID: TxCrosspointTallyDumpByte, Payload: []byte{0x23, 0x03, 0x00, 1, 2}}
	if _, err := DecodeCrosspointTallyDumpByte(short); !errors.Is(err, ErrShortPayload) {
		t.Errorf("byte short: %v", err)
	}
	if _, err := DecodeCrosspointTallyDumpWord(Frame{ID: TxCrosspointTally}); !errors.Is(err, ErrWrongCommand) {
		t.Errorf("word wrong: %v", err)
	}
}
