package codec

import (
	"bytes"
	"errors"
	"testing"
)

// TestEncodeExtendedProtectConnectByteLayout pins rx 102 wire layout
// against §3.2.66 — Dst + Device extended addressing.
func TestEncodeExtendedProtectConnectByteLayout(t *testing.T) {
	cases := []struct {
		name   string
		params ExtendedProtectConnectParams
		want   []byte
	}{
		{
			"dst=1 device=2 narrow",
			ExtendedProtectConnectParams{Destination: 1, Device: 2},
			// DestMult=0x00 DestMod=0x01 DeviceMult=0x00 DeviceMod=0x02.
			// checksum7({66, 00, 01, 00, 02}) = (-(0x69)) & 0x7F = 0x17.
			[]byte{SOM, 0x66, 0x00, 0x01, 0x00, 0x02, 0x17},
		},
		{
			"dst=5000 device=9000 extended",
			ExtendedProtectConnectParams{Destination: 5000, Device: 9000},
			// 5000 = 39*128+8 → DestMult=0x27 DestMod=0x08.
			// 9000 = 70*128+40 → DeviceMult=0x46 DeviceMod=0x28.
			// sum=0x66+0x27+0x08+0x46+0x28=0x103 → -0x03 & 0xFF = 0xFD → 7-bit = 0x7D.
			[]byte{SOM, 0x66, 0x27, 0x08, 0x46, 0x28, 0x7D},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wire := Pack(EncodeExtendedProtectConnect(tc.params))
			if !bytes.Equal(wire, tc.want) {
				t.Fatalf("wire\n got %X\nwant %X", wire, tc.want)
			}
			parsed, _, err := Unpack(wire)
			if err != nil {
				t.Fatalf("Unpack: %v", err)
			}
			got, err := DecodeExtendedProtectConnect(parsed)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if got != tc.params {
				t.Errorf("decoded = %+v; want %+v", got, tc.params)
			}
		})
	}
}

// TestDecodeExtendedProtectConnectRejects verifies guards.
func TestDecodeExtendedProtectConnectRejects(t *testing.T) {
	if _, err := DecodeExtendedProtectConnect(Frame{ID: RxExtendedProtectInterrogate, Payload: make([]byte, 4)}); !errors.Is(err, ErrWrongCommand) {
		t.Errorf("wrong ID: got %v; want ErrWrongCommand", err)
	}
	if _, err := DecodeExtendedProtectConnect(Frame{ID: RxExtendedProtectConnect, Payload: make([]byte, 3)}); !errors.Is(err, ErrShortPayload) {
		t.Errorf("short payload: got %v; want ErrShortPayload", err)
	}
}
