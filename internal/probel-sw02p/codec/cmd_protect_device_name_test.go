package codec

import (
	"bytes"
	"errors"
	"testing"
)

// TestEncodeProtectDeviceNameRequestByteLayout pins rx 103 against
// §3.2.67 — 2-byte Device Number only.
func TestEncodeProtectDeviceNameRequestByteLayout(t *testing.T) {
	cases := []struct {
		name   string
		params ProtectDeviceNameRequestParams
		want   []byte
	}{
		{
			"device=1 narrow",
			ProtectDeviceNameRequestParams{Device: 1},
			// Multiplier=0x00 DeviceMod=0x01.
			// sum=0x67+0x00+0x01=0x68 → -0x68 & 0xFF = 0x98 → 7-bit = 0x18.
			[]byte{SOM, 0x67, 0x00, 0x01, 0x18},
		},
		{
			"device=500 mid-range",
			ProtectDeviceNameRequestParams{Device: 500},
			// 500 = 3*128+116 → Multiplier=0x03 DeviceMod=0x74.
			// sum=0x67+0x03+0x74=0xDE → -0xDE & 0xFF = 0x22 → 7-bit = 0x22.
			[]byte{SOM, 0x67, 0x03, 0x74, 0x22},
		},
		{
			"device=1023 max narrow",
			ProtectDeviceNameRequestParams{Device: 1023},
			// 1023 = 7*128+127 → Multiplier=0x07 DeviceMod=0x7F.
			// sum=0x67+0x07+0x7F=0xED → -0xED & 0xFF = 0x13 → 7-bit = 0x13.
			[]byte{SOM, 0x67, 0x07, 0x7F, 0x13},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wire := Pack(EncodeProtectDeviceNameRequest(tc.params))
			if !bytes.Equal(wire, tc.want) {
				t.Fatalf("wire\n got %X\nwant %X", wire, tc.want)
			}
			parsed, _, err := Unpack(wire)
			if err != nil {
				t.Fatalf("Unpack: %v", err)
			}
			got, err := DecodeProtectDeviceNameRequest(parsed)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if got != tc.params {
				t.Errorf("decoded = %+v; want %+v", got, tc.params)
			}
		})
	}
}

// TestDecodeProtectDeviceNameRequestRejects verifies guards.
func TestDecodeProtectDeviceNameRequestRejects(t *testing.T) {
	if _, err := DecodeProtectDeviceNameRequest(Frame{ID: TxProtectDeviceNameResponse, Payload: make([]byte, 2)}); !errors.Is(err, ErrWrongCommand) {
		t.Errorf("wrong ID: got %v; want ErrWrongCommand", err)
	}
	if _, err := DecodeProtectDeviceNameRequest(Frame{ID: RxProtectDeviceNameRequest, Payload: []byte{0}}); !errors.Is(err, ErrShortPayload) {
		t.Errorf("short payload: got %v; want ErrShortPayload", err)
	}
}

// TestEncodeProtectDeviceNameResponseSpacePadding confirms names
// shorter than 8 chars are space-padded on the wire per §3.2.63.
func TestEncodeProtectDeviceNameResponseSpacePadding(t *testing.T) {
	p := ProtectDeviceNameResponseParams{Device: 42, Name: "AB"}
	f := EncodeProtectDeviceNameResponse(p)
	if len(f.Payload) != PayloadLenProtectDeviceNameResponse {
		t.Fatalf("payload len = %d; want %d", len(f.Payload), PayloadLenProtectDeviceNameResponse)
	}
	// Bytes 2-3 carry "AB", bytes 4-9 should all be 0x20.
	if f.Payload[2] != 'A' || f.Payload[3] != 'B' {
		t.Errorf("bytes 2-3 = %02X %02X; want 'A' 'B'", f.Payload[2], f.Payload[3])
	}
	for i := 4; i < PayloadLenProtectDeviceNameResponse; i++ {
		if f.Payload[i] != 0x20 {
			t.Errorf("byte %d = %02X; want space padding (0x20)", i, f.Payload[i])
		}
	}
	// Round-trip trims padding on decode.
	got, err := DecodeProtectDeviceNameResponse(f)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Device != 42 || got.Name != "AB" {
		t.Errorf("decoded = %+v; want {Device:42 Name:AB}", got)
	}
}

// TestEncodeProtectDeviceNameResponseTruncates confirms names longer
// than 8 chars are truncated to the fixed wire width.
func TestEncodeProtectDeviceNameResponseTruncates(t *testing.T) {
	p := ProtectDeviceNameResponseParams{Device: 0, Name: "ABCDEFGHIJKL"}
	f := EncodeProtectDeviceNameResponse(p)
	if string(f.Payload[2:PayloadLenProtectDeviceNameResponse]) != "ABCDEFGH" {
		t.Errorf("name on wire = %q; want %q", string(f.Payload[2:PayloadLenProtectDeviceNameResponse]), "ABCDEFGH")
	}
}

// TestEncodeProtectDeviceNameResponseRoundTripFullName exercises the
// full-width case — exactly 8 chars.
func TestEncodeProtectDeviceNameResponseRoundTripFullName(t *testing.T) {
	p := ProtectDeviceNameResponseParams{Device: 500, Name: "ROUTER01"}
	wire := Pack(EncodeProtectDeviceNameResponse(p))
	parsed, _, err := Unpack(wire)
	if err != nil {
		t.Fatalf("Unpack: %v", err)
	}
	got, err := DecodeProtectDeviceNameResponse(parsed)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got != p {
		t.Errorf("decoded = %+v; want %+v", got, p)
	}
}

// TestDecodeProtectDeviceNameResponseRejects verifies guards.
func TestDecodeProtectDeviceNameResponseRejects(t *testing.T) {
	if _, err := DecodeProtectDeviceNameResponse(Frame{ID: RxProtectDeviceNameRequest, Payload: make([]byte, 10)}); !errors.Is(err, ErrWrongCommand) {
		t.Errorf("wrong ID: got %v; want ErrWrongCommand", err)
	}
	if _, err := DecodeProtectDeviceNameResponse(Frame{ID: TxProtectDeviceNameResponse, Payload: make([]byte, 9)}); !errors.Is(err, ErrShortPayload) {
		t.Errorf("short payload: got %v; want ErrShortPayload", err)
	}
}
