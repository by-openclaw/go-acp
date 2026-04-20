package probel

import (
	"bytes"
	"errors"
	"testing"
)

func TestCrosspointConnect_General(t *testing.T) {
	p := CrosspointConnectParams{MatrixID: 1, LevelID: 2, DestinationID: 500, SourceID: 600}
	f := EncodeCrosspointConnect(p)
	if f.ID != RxCrosspointConnect {
		t.Errorf("ID got %#x want 0x02", f.ID)
	}
	// Same layout as tx 003: multiplier 0x34, dest%128=0x74, src%128=0x58
	want := []byte{0x12, 0x34, 0x74, 0x58}
	if !bytes.Equal(f.Payload, want) {
		t.Errorf("Payload got %X want %X", f.Payload, want)
	}
	got, err := DecodeCrosspointConnect(f)
	if err != nil || got != p {
		t.Errorf("round-trip: got %+v err %v want %+v", got, err, p)
	}
}

func TestCrosspointConnect_Extended(t *testing.T) {
	p := CrosspointConnectParams{MatrixID: 16, LevelID: 2, DestinationID: 1000, SourceID: 2000}
	f := EncodeCrosspointConnect(p)
	if f.ID != RxCrosspointConnectExt {
		t.Errorf("ID got %#x want 0x82", f.ID)
	}
	want := []byte{16, 2, 0x03, 0xE8, 0x07, 0xD0}
	if !bytes.Equal(f.Payload, want) {
		t.Errorf("Payload got %X want %X", f.Payload, want)
	}
	got, err := DecodeCrosspointConnect(f)
	if err != nil || got != p {
		t.Errorf("round-trip extended: got %+v err %v want %+v", got, err, p)
	}
}

func TestCrosspointConnect_FramerRoundTrip(t *testing.T) {
	p := CrosspointConnectParams{MatrixID: 3, LevelID: 4, DestinationID: 10, SourceID: 20}
	wire := Pack(EncodeCrosspointConnect(p))
	back, _, err := Unpack(wire)
	if err != nil {
		t.Fatalf("Unpack: %v", err)
	}
	got, err := DecodeCrosspointConnect(back)
	if err != nil || got != p {
		t.Errorf("got %+v err %v want %+v", got, err, p)
	}
}

func TestCrosspointConnected_General(t *testing.T) {
	// Matches tx 003 layout on wire; only ID differs.
	p := CrosspointConnectedParams{MatrixID: 1, LevelID: 2, DestinationID: 500, SourceID: 600}
	f := EncodeCrosspointConnected(p)
	if f.ID != TxCrosspointConnected {
		t.Errorf("ID got %#x want 0x04", f.ID)
	}
	want := []byte{0x12, 0x34, 0x74, 0x58}
	if !bytes.Equal(f.Payload, want) {
		t.Errorf("Payload got %X want %X", f.Payload, want)
	}
	got, err := DecodeCrosspointConnected(f)
	if err != nil || got != p {
		t.Errorf("round-trip: got %+v err %v want %+v", got, err, p)
	}
}

func TestCrosspointConnected_Extended(t *testing.T) {
	p := CrosspointConnectedParams{
		MatrixID: 30, LevelID: 4, DestinationID: 1200, SourceID: 2000,
	}
	f := EncodeCrosspointConnected(p)
	if f.ID != TxCrosspointConnectedExt {
		t.Errorf("ID got %#x want 0x84", f.ID)
	}
	want := []byte{30, 4, 0x04, 0xB0, 0x07, 0xD0, 0x00}
	if !bytes.Equal(f.Payload, want) {
		t.Errorf("Payload got %X want %X", f.Payload, want)
	}
	got, err := DecodeCrosspointConnected(f)
	if err != nil || got != p {
		t.Errorf("round-trip extended: got %+v err %v want %+v", got, err, p)
	}
}

func TestCrosspointConnect_DecodeErrors(t *testing.T) {
	if _, err := DecodeCrosspointConnect(Frame{ID: RxCrosspointConnect, Payload: []byte{0x01, 0x02}}); !errors.Is(err, ErrShortPayload) {
		t.Errorf("short: got %v", err)
	}
	if _, err := DecodeCrosspointConnected(Frame{ID: RxCrosspointConnect}); !errors.Is(err, ErrWrongCommand) {
		t.Errorf("wrong: got %v", err)
	}
}
