package codec

import (
	"bytes"
	"errors"
	"testing"
)

// TestExtendedProtectTripleByteLayout pins the wire layout of tx 96 /
// 97 / 98 against §3.2.60 / §3.2.61 / §3.2.62 — all three share the
// same 5-byte MESSAGE shape so a single table drives all three command
// bytes.
func TestExtendedProtectTripleByteLayout(t *testing.T) {
	cases := []struct {
		name    string
		protect ProtectState
		dst     uint16
		device  uint16
	}{
		{"not protected narrow", ProtectNone, 0, 0},
		{"pro-bel narrow", ProtectProBel, 1, 2},
		{"override extended", ProtectProBelOverride, 5000, 9000},
		{"oem extended", ProtectOEM, 16000, 16383},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// tx 96 pair
			f96 := EncodeExtendedProtectTally(ExtendedProtectTallyParams{
				Protect: tc.protect, Destination: tc.dst, Device: tc.device,
			})
			w96 := Pack(f96)
			parsed, n, err := Unpack(w96)
			if err != nil {
				t.Fatalf("Unpack tx 96: %v", err)
			}
			if n != len(w96) {
				t.Errorf("tx 96 consumed = %d; want %d", n, len(w96))
			}
			got96, err := DecodeExtendedProtectTally(parsed)
			if err != nil {
				t.Fatalf("DecodeExtendedProtectTally: %v", err)
			}
			if got96.Protect != tc.protect || got96.Destination != tc.dst || got96.Device != tc.device {
				t.Errorf("tx 96 decoded = %+v; want protect=%d dst=%d device=%d",
					got96, tc.protect, tc.dst, tc.device)
			}

			// tx 97 same shape, different CMD byte
			f97 := EncodeExtendedProtectConnected(ExtendedProtectConnectedParams{
				Protect: tc.protect, Destination: tc.dst, Device: tc.device,
			})
			w97 := Pack(f97)
			// Payload should be identical to tx 96's.
			if !bytes.Equal(f97.Payload, f96.Payload) {
				t.Errorf("tx 97 payload %X != tx 96 payload %X", f97.Payload, f96.Payload)
			}
			if w97[1] != 0x61 {
				t.Errorf("tx 97 CMD = %#x; want 0x61", w97[1])
			}
			parsed97, _, err := Unpack(w97)
			if err != nil {
				t.Fatalf("Unpack tx 97: %v", err)
			}
			got97, err := DecodeExtendedProtectConnected(parsed97)
			if err != nil {
				t.Fatalf("DecodeExtendedProtectConnected: %v", err)
			}
			if got97.Protect != tc.protect || got97.Destination != tc.dst || got97.Device != tc.device {
				t.Errorf("tx 97 decoded = %+v", got97)
			}

			// tx 98 same shape, different CMD byte
			f98 := EncodeExtendedProtectDisconnected(ExtendedProtectDisconnectedParams{
				Protect: tc.protect, Destination: tc.dst, Device: tc.device,
			})
			w98 := Pack(f98)
			if !bytes.Equal(f98.Payload, f96.Payload) {
				t.Errorf("tx 98 payload %X != tx 96 payload %X", f98.Payload, f96.Payload)
			}
			if w98[1] != 0x62 {
				t.Errorf("tx 98 CMD = %#x; want 0x62", w98[1])
			}
			parsed98, _, err := Unpack(w98)
			if err != nil {
				t.Fatalf("Unpack tx 98: %v", err)
			}
			got98, err := DecodeExtendedProtectDisconnected(parsed98)
			if err != nil {
				t.Fatalf("DecodeExtendedProtectDisconnected: %v", err)
			}
			if got98.Protect != tc.protect || got98.Destination != tc.dst || got98.Device != tc.device {
				t.Errorf("tx 98 decoded = %+v", got98)
			}
		})
	}
}

// TestExtendedProtectDecodersReject verifies per-command decoder
// guards for all three protect commands.
func TestExtendedProtectDecodersReject(t *testing.T) {
	if _, err := DecodeExtendedProtectTally(Frame{ID: TxExtendedProtectConnected, Payload: make([]byte, 5)}); !errors.Is(err, ErrWrongCommand) {
		t.Errorf("tx 96 wrong ID: got %v; want ErrWrongCommand", err)
	}
	if _, err := DecodeExtendedProtectTally(Frame{ID: TxExtendedProtectTally, Payload: make([]byte, 4)}); !errors.Is(err, ErrShortPayload) {
		t.Errorf("tx 96 short payload: got %v; want ErrShortPayload", err)
	}
	if _, err := DecodeExtendedProtectConnected(Frame{ID: TxExtendedProtectTally, Payload: make([]byte, 5)}); !errors.Is(err, ErrWrongCommand) {
		t.Errorf("tx 97 wrong ID: got %v; want ErrWrongCommand", err)
	}
	if _, err := DecodeExtendedProtectDisconnected(Frame{ID: TxExtendedProtectConnected, Payload: make([]byte, 5)}); !errors.Is(err, ErrWrongCommand) {
		t.Errorf("tx 98 wrong ID: got %v; want ErrWrongCommand", err)
	}
}

// TestExtendedProtectStateMask confirms bits[2-7] of the Protect byte
// are forced to 0 on encode and ignored on decode.
func TestExtendedProtectStateMask(t *testing.T) {
	// Inject garbage bits in bits 2-7 and confirm decode keeps the low
	// 2 bits only.
	f := Frame{ID: TxExtendedProtectTally, Payload: []byte{0xFE, 0x00, 0x01, 0x00, 0x02}}
	got, err := DecodeExtendedProtectTally(f)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Protect != ProtectProBelOverride { // 0xFE & 0x03 = 0x02
		t.Errorf("got protect=%d; want %d", got.Protect, ProtectProBelOverride)
	}
}
