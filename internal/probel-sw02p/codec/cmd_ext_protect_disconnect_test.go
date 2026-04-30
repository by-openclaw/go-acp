package codec

import (
	"bytes"
	"errors"
	"testing"
)

// TestEncodeExtendedProtectDisconnectByteLayout pins rx 104 against
// §3.2.68 — same 4-byte Dst + Device layout as rx 102.
func TestEncodeExtendedProtectDisconnectByteLayout(t *testing.T) {
	p := ExtendedProtectDisconnectParams{Destination: 1, Device: 2}
	wire := Pack(EncodeExtendedProtectDisconnect(p))
	// sum={68, 00, 01, 00, 02}=0x6B → -0x6B & 0xFF = 0x95 → 7-bit = 0x15.
	want := []byte{SOM, 0x68, 0x00, 0x01, 0x00, 0x02, 0x15}
	if !bytes.Equal(wire, want) {
		t.Fatalf("wire\n got %X\nwant %X", wire, want)
	}
	parsed, _, err := Unpack(wire)
	if err != nil {
		t.Fatalf("Unpack: %v", err)
	}
	got, err := DecodeExtendedProtectDisconnect(parsed)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got != p {
		t.Errorf("decoded = %+v; want %+v", got, p)
	}
}

// TestDecodeExtendedProtectDisconnectRejects verifies guards.
func TestDecodeExtendedProtectDisconnectRejects(t *testing.T) {
	if _, err := DecodeExtendedProtectDisconnect(Frame{ID: RxExtendedProtectConnect, Payload: make([]byte, 4)}); !errors.Is(err, ErrWrongCommand) {
		t.Errorf("wrong ID: got %v; want ErrWrongCommand", err)
	}
	if _, err := DecodeExtendedProtectDisconnect(Frame{ID: RxExtendedProtectDisconnect, Payload: make([]byte, 3)}); !errors.Is(err, ErrShortPayload) {
		t.Errorf("short payload: got %v; want ErrShortPayload", err)
	}
}
