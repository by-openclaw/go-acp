package codec

import (
	"bytes"
	"errors"
	"testing"
)

// TestEncodeExtendedProtectTallyDumpRequestByteLayout pins rx 105
// against §3.2.69 — Count + StartDest DIV 128 + MOD 128.
func TestEncodeExtendedProtectTallyDumpRequestByteLayout(t *testing.T) {
	cases := []struct {
		name   string
		params ExtendedProtectTallyDumpRequestParams
		want   []byte
	}{
		{
			"count=1 start=0",
			ExtendedProtectTallyDumpRequestParams{Count: 1, StartDestination: 0},
			// sum={69, 01, 00, 00} = 0x6A → -0x6A & 0xFF = 0x96 → 7-bit = 0x16.
			[]byte{SOM, 0x69, 0x01, 0x00, 0x00, 0x16},
		},
		{
			"count=32 start=5000",
			ExtendedProtectTallyDumpRequestParams{Count: 32, StartDestination: 5000},
			// 5000 = 39*128+8 → 0x27 0x08.
			// sum={69, 20, 27, 08}=0xB8 → -0xB8 & 0xFF = 0x48 → 7-bit = 0x48.
			[]byte{SOM, 0x69, 0x20, 0x27, 0x08, 0x48},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wire := Pack(EncodeExtendedProtectTallyDumpRequest(tc.params))
			if !bytes.Equal(wire, tc.want) {
				t.Fatalf("wire\n got %X\nwant %X", wire, tc.want)
			}
			parsed, _, err := Unpack(wire)
			if err != nil {
				t.Fatalf("Unpack: %v", err)
			}
			got, err := DecodeExtendedProtectTallyDumpRequest(parsed)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if got != tc.params {
				t.Errorf("decoded = %+v; want %+v", got, tc.params)
			}
		})
	}
}

// TestDecodeExtendedProtectTallyDumpRequestRejects verifies guards.
func TestDecodeExtendedProtectTallyDumpRequestRejects(t *testing.T) {
	if _, err := DecodeExtendedProtectTallyDumpRequest(Frame{ID: TxExtendedProtectTallyDump, Payload: make([]byte, 3)}); !errors.Is(err, ErrWrongCommand) {
		t.Errorf("wrong ID: got %v; want ErrWrongCommand", err)
	}
	if _, err := DecodeExtendedProtectTallyDumpRequest(Frame{ID: RxExtendedProtectTallyDumpRequest, Payload: make([]byte, 2)}); !errors.Is(err, ErrShortPayload) {
		t.Errorf("short payload: got %v; want ErrShortPayload", err)
	}
}
