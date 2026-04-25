package codec

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

// TestEncodeSourceLockStatusRequestWireLayout pins rx 014 against
// §3.2.16 — single controller-selector byte.
func TestEncodeSourceLockStatusRequestWireLayout(t *testing.T) {
	cases := []struct {
		name   string
		params SourceLockStatusRequestParams
		want   []byte
	}{
		{
			"LH controller",
			SourceLockStatusRequestParams{Controller: ControllerLH},
			// checksum7({0E, 00}) = (-(0x0E)) & 0x7F = 0x72.
			[]byte{SOM, 0x0E, 0x00, 0x72},
		},
		{
			"RH controller",
			SourceLockStatusRequestParams{Controller: ControllerRH},
			// checksum7({0E, 01}) = (-(0x0F)) & 0x7F = 0x71.
			[]byte{SOM, 0x0E, 0x01, 0x71},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wire := Pack(EncodeSourceLockStatusRequest(tc.params))
			if !bytes.Equal(wire, tc.want) {
				t.Fatalf("wire\n got %X\nwant %X", wire, tc.want)
			}
			parsed, _, err := Unpack(wire)
			if err != nil {
				t.Fatalf("Unpack: %v", err)
			}
			got, err := DecodeSourceLockStatusRequest(parsed)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if got != tc.params {
				t.Errorf("decoded = %+v; want %+v", got, tc.params)
			}
		})
	}
}

// TestDecodeSourceLockStatusRequestRejects verifies guards.
func TestDecodeSourceLockStatusRequestRejects(t *testing.T) {
	if _, err := DecodeSourceLockStatusRequest(Frame{ID: TxSourceLockStatusResponse, Payload: []byte{0}}); !errors.Is(err, ErrWrongCommand) {
		t.Errorf("wrong ID: got %v; want ErrWrongCommand", err)
	}
	if _, err := DecodeSourceLockStatusRequest(Frame{ID: RxSourceLockStatusRequest, Payload: nil}); !errors.Is(err, ErrShortPayload) {
		t.Errorf("short payload: got %v; want ErrShortPayload", err)
	}
}

// TestEncodeSourceLockStatusResponseRoundTrip pins tx 015 against
// §3.2.17 — 2-byte self-declared length + 4-source-per-byte bitmap.
// Exercises 8 sources (exactly 2 bytes), 10 sources (3 bytes with
// bits 6-7 of last byte forced 0), and the all-ones pattern.
func TestEncodeSourceLockStatusResponseRoundTrip(t *testing.T) {
	cases := []struct {
		name   string
		locked []bool
	}{
		{"8 sources all locked", make8(true)},
		{"10 sources mixed", []bool{true, false, true, false, true, true, false, true, true, true}},
		{"16 sources every other", alternating(16)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := SourceLockStatusResponseParams{Locked: tc.locked}
			wire := Pack(EncodeSourceLockStatusResponse(p))
			// Self-declared length byte pair must match.
			msgLen := len(wire) - 3 // SOM + CMD + ... + CHK
			hi := byte(msgLen / 128)
			lo := byte(msgLen % 128)
			if wire[2] != hi || wire[3] != lo {
				t.Errorf("header = %02X %02X; want %02X %02X", wire[2], wire[3], hi, lo)
			}
			parsed, n, err := Unpack(wire)
			if err != nil || n != len(wire) {
				t.Fatalf("Unpack: n=%d err=%v", n, err)
			}
			got, err := DecodeSourceLockStatusResponse(parsed)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			// Decoded Locked slice rounds up to a multiple of 4 —
			// compare only the declared portion.
			for i, want := range tc.locked {
				if got.Locked[i] != want {
					t.Errorf("locked[%d] = %v; want %v", i, got.Locked[i], want)
				}
			}
		})
	}
}

// TestEncodeSourceLockStatusResponseBitPacking locks §3.2.17 byte 3
// layout: bit 0 = source 0, bit 1 = source 1, bit 2 = source 2, bit
// 3 = source 3, bits 4-7 = always 0.
func TestEncodeSourceLockStatusResponseBitPacking(t *testing.T) {
	// Sources 0, 2 locked → byte 3 = 0b0000_0101 = 0x05.
	p := SourceLockStatusResponseParams{Locked: []bool{true, false, true, false}}
	f := EncodeSourceLockStatusResponse(p)
	if len(f.Payload) != 3 {
		t.Fatalf("payload len = %d; want 3", len(f.Payload))
	}
	if f.Payload[2] != 0x05 {
		t.Errorf("byte 3 = %02X; want 05", f.Payload[2])
	}
	if f.Payload[2]&0xF0 != 0 {
		t.Errorf("byte 3 high nibble = %02X; §3.2.17 requires 0", f.Payload[2]&0xF0)
	}
}

// TestUnpackSourceLockStatusResponseVariableLen verifies the scanner
// reads the 2-byte header before peeling the full frame. Truncated
// buffers return io.ErrUnexpectedEOF; full buffer decodes cleanly.
func TestUnpackSourceLockStatusResponseVariableLen(t *testing.T) {
	p := SourceLockStatusResponseParams{Locked: []bool{true, true, true, true, true, true, true, true}}
	wire := Pack(EncodeSourceLockStatusResponse(p))
	// Truncate: only 1 header byte buffered after CMD.
	_, _, err := Unpack(wire[:3])
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("1-header byte: got %v; want io.ErrUnexpectedEOF", err)
	}
	// 2 header bytes but still truncated.
	_, _, err = Unpack(wire[:4])
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("2-header byte: got %v; want io.ErrUnexpectedEOF", err)
	}
	// Full frame: clean decode.
	if _, n, err := Unpack(wire); err != nil || n != len(wire) {
		t.Errorf("full: n=%d err=%v; want n=%d", n, err, len(wire))
	}
}

// TestDecodeSourceLockStatusResponseHeaderMismatch confirms the
// decoder rejects a frame whose self-declared length disagrees with
// the actual MESSAGE size.
func TestDecodeSourceLockStatusResponseHeaderMismatch(t *testing.T) {
	// Build a valid 2-source wire then lie about the declared length.
	f := EncodeSourceLockStatusResponse(SourceLockStatusResponseParams{Locked: []bool{true, false}})
	// Override declared length to something shorter.
	f.Payload[1] = 99
	if _, err := DecodeSourceLockStatusResponse(f); !errors.Is(err, ErrShortPayload) {
		t.Errorf("mismatch: got %v; want ErrShortPayload", err)
	}
}

// helpers
func make8(v bool) []bool {
	out := make([]bool, 8)
	for i := range out {
		out[i] = v
	}
	return out
}

func alternating(n int) []bool {
	out := make([]bool, n)
	for i := range out {
		out[i] = i%2 == 0
	}
	return out
}
