package codec

import (
	"bytes"
	"errors"
	"testing"
)

// TestEncodeExtendedInterrogateByteLayout pins rx 65 wire layout
// against §3.2.47. Exercises narrow addressing (dst < 128), the DIV
// 128 boundary, and the upper extended-range (dst > 1023).
func TestEncodeExtendedInterrogateByteLayout(t *testing.T) {
	cases := []struct {
		name     string
		params   ExtendedInterrogateParams
		wantWire []byte
	}{
		{
			name:   "dst=1 narrow",
			params: ExtendedInterrogateParams{Destination: 1},
			// DestMult=0x00 DestMod=0x01.
			// checksum7({41, 00, 01}) = (-(0x42)) & 0x7F = 0x3E.
			wantWire: []byte{SOM, 0x41, 0x00, 0x01, 0x3E},
		},
		{
			name:   "dst=130 extended low",
			params: ExtendedInterrogateParams{Destination: 130},
			// DestDIV128=1, DestMult=0x01, DestMod=2.
			// checksum7({41, 01, 02}) = (-(0x44)) & 0x7F = 0x3C.
			wantWire: []byte{SOM, 0x41, 0x01, 0x02, 0x3C},
		},
		{
			name:   "dst=5000 extended",
			params: ExtendedInterrogateParams{Destination: 5000},
			// 5000 = 39*128 + 8. DestMult=0x27 DestMod=0x08.
			// checksum7({41, 27, 08}) = (-(0x70)) & 0x7F = 0x10.
			wantWire: []byte{SOM, 0x41, 0x27, 0x08, 0x10},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wire := Pack(EncodeExtendedInterrogate(tc.params))
			if !bytes.Equal(wire, tc.wantWire) {
				t.Fatalf("wire\n got %X\nwant %X", wire, tc.wantWire)
			}
			parsed, n, err := Unpack(wire)
			if err != nil {
				t.Fatalf("Unpack: %v", err)
			}
			if n != len(wire) {
				t.Errorf("consumed = %d; want %d", n, len(wire))
			}
			got, err := DecodeExtendedInterrogate(parsed)
			if err != nil {
				t.Fatalf("DecodeExtendedInterrogate: %v", err)
			}
			if got != tc.params {
				t.Errorf("decoded = %+v; want %+v", got, tc.params)
			}
		})
	}
}

// TestDecodeExtendedInterrogateRejects verifies per-command decoder
// guards.
func TestDecodeExtendedInterrogateRejects(t *testing.T) {
	if _, err := DecodeExtendedInterrogate(Frame{ID: TxExtendedTally, Payload: []byte{0, 1}}); !errors.Is(err, ErrWrongCommand) {
		t.Errorf("wrong ID: got %v; want ErrWrongCommand", err)
	}
	if _, err := DecodeExtendedInterrogate(Frame{ID: RxExtendedInterrogate, Payload: []byte{0}}); !errors.Is(err, ErrShortPayload) {
		t.Errorf("short payload: got %v; want ErrShortPayload", err)
	}
}

// TestEncodeExtendedTallyByteLayout pins tx 67 wire layout against
// §3.2.49. Covers the status-byte flags.
func TestEncodeExtendedTallyByteLayout(t *testing.T) {
	cases := []struct {
		name     string
		params   ExtendedTallyParams
		wantWire []byte
	}{
		{
			name:   "dst=5000 src=10000 no flags",
			params: ExtendedTallyParams{Destination: 5000, Source: 10000},
			// dst 5000 = 39*128+8 → DestMult=0x27 DestMod=0x08
			// src 10000 = 78*128+16 → SrcMult=0x4E SrcMod=0x10
			// status = 0x00.
			// checksum7({43, 27, 08, 4E, 10, 00}) = (-(0xD0)) & 0x7F = 0x30.
			wantWire: []byte{SOM, 0x43, 0x27, 0x08, 0x4E, 0x10, 0x00, 0x30},
		},
		{
			name:   "bad-source + update-off flags",
			params: ExtendedTallyParams{Destination: 0, Source: 0, UpdateOff: true, BadSource: true},
			// status = 0x03.
			// checksum7({43, 00, 00, 00, 00, 03}) = (-(0x46)) & 0x7F = 0x3A.
			wantWire: []byte{SOM, 0x43, 0x00, 0x00, 0x00, 0x00, 0x03, 0x3A},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wire := Pack(EncodeExtendedTally(tc.params))
			if !bytes.Equal(wire, tc.wantWire) {
				t.Fatalf("wire\n got %X\nwant %X", wire, tc.wantWire)
			}
			parsed, n, err := Unpack(wire)
			if err != nil {
				t.Fatalf("Unpack: %v", err)
			}
			if n != len(wire) {
				t.Errorf("consumed = %d; want %d", n, len(wire))
			}
			got, err := DecodeExtendedTally(parsed)
			if err != nil {
				t.Fatalf("DecodeExtendedTally: %v", err)
			}
			if got != tc.params {
				t.Errorf("decoded = %+v; want %+v", got, tc.params)
			}
		})
	}
}

// TestDecodeExtendedTallyRejects verifies per-command decoder guards.
func TestDecodeExtendedTallyRejects(t *testing.T) {
	if _, err := DecodeExtendedTally(Frame{ID: RxExtendedInterrogate, Payload: make([]byte, 5)}); !errors.Is(err, ErrWrongCommand) {
		t.Errorf("wrong ID: got %v; want ErrWrongCommand", err)
	}
	if _, err := DecodeExtendedTally(Frame{ID: TxExtendedTally, Payload: make([]byte, 4)}); !errors.Is(err, ErrShortPayload) {
		t.Errorf("short payload: got %v; want ErrShortPayload", err)
	}
}

// TestEncodeExtendedConnectByteLayout pins rx 66 wire layout against
// §3.2.48.
func TestEncodeExtendedConnectByteLayout(t *testing.T) {
	p := ExtendedConnectParams{Destination: 5000, Source: 10000}
	wire := Pack(EncodeExtendedConnect(p))
	// DestMult=0x27 DestMod=0x08 SrcMult=0x4E SrcMod=0x10.
	// checksum7({42, 27, 08, 4E, 10}) = (-(0xCF)) & 0x7F = 0x31.
	want := []byte{SOM, 0x42, 0x27, 0x08, 0x4E, 0x10, 0x31}
	if !bytes.Equal(wire, want) {
		t.Fatalf("wire\n got %X\nwant %X", wire, want)
	}
	parsed, n, err := Unpack(wire)
	if err != nil {
		t.Fatalf("Unpack: %v", err)
	}
	if n != len(wire) {
		t.Errorf("consumed = %d; want %d", n, len(wire))
	}
	got, err := DecodeExtendedConnect(parsed)
	if err != nil {
		t.Fatalf("DecodeExtendedConnect: %v", err)
	}
	if got != p {
		t.Errorf("decoded = %+v; want %+v", got, p)
	}
}

// TestDecodeExtendedConnectRejects verifies per-command decoder guards.
func TestDecodeExtendedConnectRejects(t *testing.T) {
	if _, err := DecodeExtendedConnect(Frame{ID: RxExtendedInterrogate, Payload: make([]byte, 4)}); !errors.Is(err, ErrWrongCommand) {
		t.Errorf("wrong ID: got %v; want ErrWrongCommand", err)
	}
	if _, err := DecodeExtendedConnect(Frame{ID: RxExtendedConnect, Payload: make([]byte, 3)}); !errors.Is(err, ErrShortPayload) {
		t.Errorf("short payload: got %v; want ErrShortPayload", err)
	}
}

// TestEncodeExtendedConnectedByteLayout pins tx 68 wire layout against
// §3.2.50 — identical shape to tx 67 with a different command byte.
func TestEncodeExtendedConnectedByteLayout(t *testing.T) {
	p := ExtendedConnectedParams{Destination: 5000, Source: 10000}
	wire := Pack(EncodeExtendedConnected(p))
	// checksum7({44, 27, 08, 4E, 10, 00}) = (-(0xD1)) & 0x7F = 0x2F.
	want := []byte{SOM, 0x44, 0x27, 0x08, 0x4E, 0x10, 0x00, 0x2F}
	if !bytes.Equal(wire, want) {
		t.Fatalf("wire\n got %X\nwant %X", wire, want)
	}
	parsed, _, err := Unpack(wire)
	if err != nil {
		t.Fatalf("Unpack: %v", err)
	}
	got, err := DecodeExtendedConnected(parsed)
	if err != nil {
		t.Fatalf("DecodeExtendedConnected: %v", err)
	}
	if got != p {
		t.Errorf("decoded = %+v; want %+v", got, p)
	}
}

// TestDecodeExtendedConnectedRejects verifies per-command decoder
// guards.
func TestDecodeExtendedConnectedRejects(t *testing.T) {
	if _, err := DecodeExtendedConnected(Frame{ID: TxExtendedTally, Payload: make([]byte, 5)}); !errors.Is(err, ErrWrongCommand) {
		t.Errorf("wrong ID: got %v; want ErrWrongCommand", err)
	}
	if _, err := DecodeExtendedConnected(Frame{ID: TxExtendedConnected, Payload: make([]byte, 4)}); !errors.Is(err, ErrShortPayload) {
		t.Errorf("short payload: got %v; want ErrShortPayload", err)
	}
}
