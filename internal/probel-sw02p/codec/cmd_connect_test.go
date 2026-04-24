package codec

import (
	"bytes"
	"errors"
	"testing"
)

// TestEncodeConnectByteLayout pins the wire layout of rx 02 against
// SW-P-02 §3.2.4 + §3.2.3.
func TestEncodeConnectByteLayout(t *testing.T) {
	cases := []struct {
		name     string
		params   ConnectParams
		wantWire []byte
	}{
		{
			name:   "dst=1 src=2 narrow",
			params: ConnectParams{Destination: 1, Source: 2},
			// Multiplier=0x00 DestMod128=0x01 SrcMod128=0x02.
			// checksum7({02, 00, 01, 02}) = (-(0x05)) & 0x7F = 0x7B.
			wantWire: []byte{SOM, 0x02, 0x00, 0x01, 0x02, 0x7B},
		},
		{
			name:   "dst=130 src=260 extended",
			params: ConnectParams{Destination: 130, Source: 260},
			// Multiplier=0x12 DestMod128=2 SrcMod128=4.
			// checksum7({02, 12, 02, 04}) = (-(0x1A)) & 0x7F = 0x66.
			wantWire: []byte{SOM, 0x02, 0x12, 0x02, 0x04, 0x66},
		},
		{
			name:   "bad source flag",
			params: ConnectParams{Destination: 0, Source: 0, BadSource: true},
			// Multiplier=0x08 (bit 3). DestMod128=0 SrcMod128=0.
			// checksum7({02, 08, 00, 00}) = (-(0x0A)) & 0x7F = 0x76.
			wantWire: []byte{SOM, 0x02, 0x08, 0x00, 0x00, 0x76},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := EncodeConnect(tc.params)
			wire := Pack(f)
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
			got, err := DecodeConnect(parsed)
			if err != nil {
				t.Fatalf("DecodeConnect: %v", err)
			}
			if got != tc.params {
				t.Errorf("decoded = %+v; want %+v", got, tc.params)
			}
		})
	}
}

// TestDecodeConnectRejects verifies per-command decoder guards.
func TestDecodeConnectRejects(t *testing.T) {
	if _, err := DecodeConnect(Frame{ID: RxInterrogate, Payload: []byte{0, 1, 2}}); !errors.Is(err, ErrWrongCommand) {
		t.Errorf("wrong ID: got %v; want ErrWrongCommand", err)
	}
	if _, err := DecodeConnect(Frame{ID: RxConnect, Payload: []byte{0, 1}}); !errors.Is(err, ErrShortPayload) {
		t.Errorf("short payload: got %v; want ErrShortPayload", err)
	}
}
