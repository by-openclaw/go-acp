package probel

import (
	"bytes"
	"errors"
	"testing"
)

func TestCrosspointInterrogate_General(t *testing.T) {
	p := CrosspointInterrogateParams{MatrixID: 1, LevelID: 2, DestinationID: 500}
	f := EncodeCrosspointInterrogate(p)

	if f.ID != RxCrosspointInterrogate {
		t.Errorf("ID got %#x want %#x (general)", f.ID, RxCrosspointInterrogate)
	}
	// byte 1 = matrix<<4 | level = 0x12
	// dest 500 = 3*128 + 116, so multiplier byte = (3<<4)&0x70 = 0x30; src=0
	// dest%128 = 116 = 0x74
	want := []byte{0x12, 0x30, 0x74}
	if !bytes.Equal(f.Payload, want) {
		t.Errorf("Payload got %X want %X", f.Payload, want)
	}

	got, err := DecodeCrosspointInterrogate(f)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got != p {
		t.Errorf("round-trip: got %+v want %+v", got, p)
	}
}

func TestCrosspointInterrogate_Extended_ByDest(t *testing.T) {
	p := CrosspointInterrogateParams{MatrixID: 0, LevelID: 0, DestinationID: 1000}
	f := EncodeCrosspointInterrogate(p)
	if f.ID != RxCrosspointInterrogateExt {
		t.Errorf("ID got %#x want %#x", f.ID, RxCrosspointInterrogateExt)
	}
	// 1000 = 3*256 + 232
	want := []byte{0x00, 0x00, 0x03, 0xE8}
	if !bytes.Equal(f.Payload, want) {
		t.Errorf("Payload got %X want %X", f.Payload, want)
	}
	got, err := DecodeCrosspointInterrogate(f)
	if err != nil || got != p {
		t.Errorf("round-trip: got %+v err %v want %+v", got, err, p)
	}
}

func TestCrosspointInterrogate_ExtendedByMatrix(t *testing.T) {
	p := CrosspointInterrogateParams{MatrixID: 20, LevelID: 3, DestinationID: 5}
	f := EncodeCrosspointInterrogate(p)
	if f.ID != RxCrosspointInterrogateExt {
		t.Errorf("matrix>15 should trigger extended: got %#x", f.ID)
	}
	got, _ := DecodeCrosspointInterrogate(f)
	if got != p {
		t.Errorf("round-trip: got %+v want %+v", got, p)
	}
}

func TestCrosspointInterrogate_FramerRoundTrip(t *testing.T) {
	p := CrosspointInterrogateParams{MatrixID: 5, LevelID: 7, DestinationID: 200}
	f := EncodeCrosspointInterrogate(p)
	wire := Pack(f)
	// Decoded from the wire → Params round-trip via framer.
	back, n, err := Unpack(wire)
	if err != nil {
		t.Fatalf("Unpack: %v", err)
	}
	if n != len(wire) {
		t.Errorf("n=%d len(wire)=%d", n, len(wire))
	}
	got, err := DecodeCrosspointInterrogate(back)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got != p {
		t.Errorf("round-trip via framer: got %+v want %+v", got, p)
	}
}

func TestCrosspointInterrogate_DecodeErrors(t *testing.T) {
	_, err := DecodeCrosspointInterrogate(Frame{ID: TxCrosspointTally})
	if !errors.Is(err, ErrWrongCommand) {
		t.Errorf("wrong command: got %v want ErrWrongCommand", err)
	}
	_, err = DecodeCrosspointInterrogate(Frame{ID: RxCrosspointInterrogate, Payload: []byte{0x01}})
	if !errors.Is(err, ErrShortPayload) {
		t.Errorf("short general: got %v want ErrShortPayload", err)
	}
	_, err = DecodeCrosspointInterrogate(Frame{ID: RxCrosspointInterrogateExt, Payload: []byte{0x01, 0x02, 0x03}})
	if !errors.Is(err, ErrShortPayload) {
		t.Errorf("short extended: got %v want ErrShortPayload", err)
	}
}

func TestCrosspointTally_General(t *testing.T) {
	p := CrosspointTallyParams{MatrixID: 1, LevelID: 2, DestinationID: 500, SourceID: 600}
	f := EncodeCrosspointTally(p)
	if f.ID != TxCrosspointTally {
		t.Errorf("ID got %#x want general 0x03", f.ID)
	}
	// matrix/level = 0x12
	// multiplier: dest=500/128=3 → bits 4-6 = 0x30 ; src=600/128=4 → bits 0-2 = 0x04
	//            combined = 0x34
	// dest%128 = 116 = 0x74 ; src%128 = 600-4*128=88 = 0x58
	want := []byte{0x12, 0x34, 0x74, 0x58}
	if !bytes.Equal(f.Payload, want) {
		t.Errorf("Payload got %X want %X", f.Payload, want)
	}
	got, err := DecodeCrosspointTally(f)
	if err != nil || got != p {
		t.Errorf("round-trip general: got %+v err %v want %+v", got, err, p)
	}
}

func TestCrosspointTally_Extended(t *testing.T) {
	p := CrosspointTallyParams{
		MatrixID: 30, LevelID: 4, DestinationID: 1200, SourceID: 2000, Status: 0,
	}
	f := EncodeCrosspointTally(p)
	if f.ID != TxCrosspointTallyExt {
		t.Errorf("ID got %#x want extended 0x83", f.ID)
	}
	// 1200 = 4*256 + 176 → 04 B0
	// 2000 = 7*256 + 208 → 07 D0
	want := []byte{30, 4, 0x04, 0xB0, 0x07, 0xD0, 0x00}
	if !bytes.Equal(f.Payload, want) {
		t.Errorf("Payload got %X want %X", f.Payload, want)
	}
	got, err := DecodeCrosspointTally(f)
	if err != nil || got != p {
		t.Errorf("round-trip extended: got %+v err %v want %+v", got, err, p)
	}
}

func TestCrosspointTally_FramerRoundTrip(t *testing.T) {
	p := CrosspointTallyParams{MatrixID: 2, LevelID: 1, DestinationID: 42, SourceID: 7}
	wire := Pack(EncodeCrosspointTally(p))
	back, _, err := Unpack(wire)
	if err != nil {
		t.Fatalf("Unpack: %v", err)
	}
	got, err := DecodeCrosspointTally(back)
	if err != nil || got != p {
		t.Errorf("round-trip via framer: got %+v err %v want %+v", got, err, p)
	}
}
