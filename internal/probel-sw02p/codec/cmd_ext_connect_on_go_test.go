package codec

import (
	"bytes"
	"errors"
	"testing"
)

// TestEncodeExtendedConnectOnGoByteLayout pins rx 069 wire layout
// against §3.2.51 + §3.2.47 + §3.2.48.
func TestEncodeExtendedConnectOnGoByteLayout(t *testing.T) {
	cases := []struct {
		name   string
		params ExtendedConnectOnGoParams
		want   []byte
	}{
		{
			"dst=1 src=2 narrow",
			ExtendedConnectOnGoParams{Destination: 1, Source: 2},
			// DestMult=0x00 DestMod=0x01 SrcMult=0x00 SrcMod=0x02.
			// checksum7({45, 00, 01, 00, 02}) = (-(0x48)) & 0x7F = 0x38.
			[]byte{SOM, 0x45, 0x00, 0x01, 0x00, 0x02, 0x38},
		},
		{
			"dst=5000 src=10000 extended",
			ExtendedConnectOnGoParams{Destination: 5000, Source: 10000},
			// 5000 = 39*128+8 → DestMult=0x27 DestMod=0x08.
			// 10000 = 78*128+16 → SrcMult=0x4E SrcMod=0x10.
			// checksum7({45, 27, 08, 4E, 10}) = (-(0xD2)) & 0x7F = 0x2E.
			[]byte{SOM, 0x45, 0x27, 0x08, 0x4E, 0x10, 0x2E},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wire := Pack(EncodeExtendedConnectOnGo(tc.params))
			if !bytes.Equal(wire, tc.want) {
				t.Fatalf("wire\n got %X\nwant %X", wire, tc.want)
			}
			parsed, n, err := Unpack(wire)
			if err != nil {
				t.Fatalf("Unpack: %v", err)
			}
			if n != len(wire) {
				t.Errorf("consumed = %d; want %d", n, len(wire))
			}
			got, err := DecodeExtendedConnectOnGo(parsed)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if got != tc.params {
				t.Errorf("decoded = %+v; want %+v", got, tc.params)
			}
		})
	}
}

// TestDecodeExtendedConnectOnGoRejects verifies guards.
func TestDecodeExtendedConnectOnGoRejects(t *testing.T) {
	if _, err := DecodeExtendedConnectOnGo(Frame{ID: TxExtendedConnectOnGoAck, Payload: make([]byte, 4)}); !errors.Is(err, ErrWrongCommand) {
		t.Errorf("wrong ID: got %v; want ErrWrongCommand", err)
	}
	if _, err := DecodeExtendedConnectOnGo(Frame{ID: RxExtendedConnectOnGo, Payload: make([]byte, 3)}); !errors.Is(err, ErrShortPayload) {
		t.Errorf("short payload: got %v; want ErrShortPayload", err)
	}
}

// TestEncodeExtendedConnectOnGoAckByteLayout pins tx 070 wire layout
// against §3.2.52 — same 4-byte shape as rx 069 with cmd=0x46.
func TestEncodeExtendedConnectOnGoAckByteLayout(t *testing.T) {
	p := ExtendedConnectOnGoAckParams{Destination: 5000, Source: 10000}
	wire := Pack(EncodeExtendedConnectOnGoAck(p))
	// checksum7({46, 27, 08, 4E, 10}) = (-(0xD3)) & 0x7F = 0x2D.
	want := []byte{SOM, 0x46, 0x27, 0x08, 0x4E, 0x10, 0x2D}
	if !bytes.Equal(wire, want) {
		t.Fatalf("wire\n got %X\nwant %X", wire, want)
	}
	parsed, _, err := Unpack(wire)
	if err != nil {
		t.Fatalf("Unpack: %v", err)
	}
	got, err := DecodeExtendedConnectOnGoAck(parsed)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got != p {
		t.Errorf("decoded = %+v; want %+v", got, p)
	}
}

// TestDecodeExtendedConnectOnGoAckRejects verifies guards.
func TestDecodeExtendedConnectOnGoAckRejects(t *testing.T) {
	if _, err := DecodeExtendedConnectOnGoAck(Frame{ID: RxExtendedConnectOnGo, Payload: make([]byte, 4)}); !errors.Is(err, ErrWrongCommand) {
		t.Errorf("wrong ID: got %v; want ErrWrongCommand", err)
	}
	if _, err := DecodeExtendedConnectOnGoAck(Frame{ID: TxExtendedConnectOnGoAck, Payload: make([]byte, 3)}); !errors.Is(err, ErrShortPayload) {
		t.Errorf("short payload: got %v; want ErrShortPayload", err)
	}
}
