package codec

import (
	"bytes"
	"errors"
	"testing"
)

// TestEncodeInterrogateByteLayout pins the wire layout of rx 01 against
// SW-P-02 §3.2.3. Exercises narrow addressing (dst < 128) and the
// extended-addressing path (dst in the 128-1023 range) so the
// Multiplier bit-packing is covered on both halves. The Source DIV 128
// bits (§3.2.3: "always 0 for this command") stay clear.
func TestEncodeInterrogateByteLayout(t *testing.T) {
	cases := []struct {
		name     string
		params   InterrogateParams
		wantWire []byte
	}{
		{
			name:   "dst=1 narrow",
			params: InterrogateParams{Destination: 1},
			// Multiplier=0x00, DestMod128=0x01.
			// checksum7({01, 00, 01}) = (-(0x02)) & 0x7F = 0x7E.
			wantWire: []byte{SOM, 0x01, 0x00, 0x01, 0x7E},
		},
		{
			name:   "dst=130 extended",
			params: InterrogateParams{Destination: 130},
			// DestDIV128=1 → Multiplier bit4-6 = 0b001 → 0x10.
			// DestMod128=2.
			// checksum7({01, 10, 02}) = (-(0x13)) & 0x7F = 0x6D.
			wantWire: []byte{SOM, 0x01, 0x10, 0x02, 0x6D},
		},
		{
			name:   "dst=0 bad source flag",
			params: InterrogateParams{Destination: 0, BadSource: true},
			// Multiplier=0x08 (bit 3). DestMod128=0.
			// checksum7({01, 08, 00}) = (-(0x09)) & 0x7F = 0x77.
			wantWire: []byte{SOM, 0x01, 0x08, 0x00, 0x77},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := EncodeInterrogate(tc.params)
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
			got, err := DecodeInterrogate(parsed)
			if err != nil {
				t.Fatalf("DecodeInterrogate: %v", err)
			}
			if got != tc.params {
				t.Errorf("decoded = %+v; want %+v", got, tc.params)
			}
		})
	}
}

// TestDecodeInterrogateRejects confirms the per-command decoder refuses
// a mismatched command ID or a short payload.
func TestDecodeInterrogateRejects(t *testing.T) {
	if _, err := DecodeInterrogate(Frame{ID: TxTally, Payload: []byte{0, 1}}); !errors.Is(err, ErrWrongCommand) {
		t.Errorf("wrong ID: got %v; want ErrWrongCommand", err)
	}
	if _, err := DecodeInterrogate(Frame{ID: RxInterrogate, Payload: []byte{0}}); !errors.Is(err, ErrShortPayload) {
		t.Errorf("short payload: got %v; want ErrShortPayload", err)
	}
}

// TestEncodeTallyByteLayout pins the wire layout of tx 03 against
// SW-P-02 §3.2.5 + §3.2.3. Exercises the §3.2.5 reserved Source=1023
// "destination out of range" sentinel.
func TestEncodeTallyByteLayout(t *testing.T) {
	cases := []struct {
		name     string
		params   TallyParams
		wantWire []byte
	}{
		{
			name:   "dst=1 src=2",
			params: TallyParams{Destination: 1, Source: 2},
			// Multiplier=0x00 DestMod128=1 SrcMod128=2.
			// checksum7({03, 00, 01, 02}) = (-(0x06)) & 0x7F = 0x7A.
			wantWire: []byte{SOM, 0x03, 0x00, 0x01, 0x02, 0x7A},
		},
		{
			name:   "dst=130 src=260",
			params: TallyParams{Destination: 130, Source: 260},
			// Multiplier=0x12 DestMod128=2 SrcMod128=4.
			// checksum7({03, 12, 02, 04}) = (-(0x1B)) & 0x7F = 0x65.
			wantWire: []byte{SOM, 0x03, 0x12, 0x02, 0x04, 0x65},
		},
		{
			name:   "dst=0 src=1023 out-of-range sentinel",
			params: TallyParams{Destination: 0, Source: DestOutOfRangeSource},
			// 1023 = 7*128 + 127. SrcDIV128=7 → Multiplier bit0-2=0b111 → 0x07.
			// DestMod128=0 SrcMod128=127.
			// checksum7({03, 07, 00, 7F}) = (-(0x89)) & 0x7F = 0x77.
			wantWire: []byte{SOM, 0x03, 0x07, 0x00, 0x7F, 0x77},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := EncodeTally(tc.params)
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
			got, err := DecodeTally(parsed)
			if err != nil {
				t.Fatalf("DecodeTally: %v", err)
			}
			if got != tc.params {
				t.Errorf("decoded = %+v; want %+v", got, tc.params)
			}
		})
	}
}

// TestDecodeTallyRejects confirms the per-command decoder refuses a
// mismatched command ID or a short payload.
func TestDecodeTallyRejects(t *testing.T) {
	if _, err := DecodeTally(Frame{ID: RxInterrogate, Payload: []byte{0, 1, 2}}); !errors.Is(err, ErrWrongCommand) {
		t.Errorf("wrong ID: got %v; want ErrWrongCommand", err)
	}
	if _, err := DecodeTally(Frame{ID: TxTally, Payload: []byte{0, 1}}); !errors.Is(err, ErrShortPayload) {
		t.Errorf("short payload: got %v; want ErrShortPayload", err)
	}
}
