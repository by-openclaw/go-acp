package codec

import (
	"bytes"
	"errors"
	"testing"
)

// TestEncodeExtendedProtectInterrogateByteLayout pins rx 101 wire
// layout against §3.2.65 — 2-byte Destination field only.
func TestEncodeExtendedProtectInterrogateByteLayout(t *testing.T) {
	cases := []struct {
		name   string
		params ExtendedProtectInterrogateParams
		want   []byte
	}{
		{
			"dst=1 narrow",
			ExtendedProtectInterrogateParams{Destination: 1},
			// DestMult=0x00 DestMod=0x01.
			// checksum7({65, 00, 01}) = (-(0x66)) & 0x7F = 0x1A.
			[]byte{SOM, 0x65, 0x00, 0x01, 0x1A},
		},
		{
			"dst=5000 extended",
			ExtendedProtectInterrogateParams{Destination: 5000},
			// 5000 = 39*128+8 → DestMult=0x27 DestMod=0x08.
			// checksum7({65, 27, 08}) = (-(0x94)) & 0x7F = 0x6C.
			[]byte{SOM, 0x65, 0x27, 0x08, 0x6C},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wire := Pack(EncodeExtendedProtectInterrogate(tc.params))
			if !bytes.Equal(wire, tc.want) {
				t.Fatalf("wire\n got %X\nwant %X", wire, tc.want)
			}
			parsed, _, err := Unpack(wire)
			if err != nil {
				t.Fatalf("Unpack: %v", err)
			}
			got, err := DecodeExtendedProtectInterrogate(parsed)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if got != tc.params {
				t.Errorf("decoded = %+v; want %+v", got, tc.params)
			}
		})
	}
}

// TestDecodeExtendedProtectInterrogateRejects verifies guards.
func TestDecodeExtendedProtectInterrogateRejects(t *testing.T) {
	if _, err := DecodeExtendedProtectInterrogate(Frame{ID: TxExtendedProtectTally, Payload: make([]byte, 2)}); !errors.Is(err, ErrWrongCommand) {
		t.Errorf("wrong ID: got %v; want ErrWrongCommand", err)
	}
	if _, err := DecodeExtendedProtectInterrogate(Frame{ID: RxExtendedProtectInterrogate, Payload: []byte{0}}); !errors.Is(err, ErrShortPayload) {
		t.Errorf("short payload: got %v; want ErrShortPayload", err)
	}
}
