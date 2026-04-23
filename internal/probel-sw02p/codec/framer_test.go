package codec

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

// TestChecksum7 pins the 7-bit two's-complement checksum algorithm from
// §3.1. MSB must always be zero.
func TestChecksum7(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want byte
	}{
		{"empty", nil, 0x00},
		{"single 0x01", []byte{0x01}, 0x7F},
		{"three bytes 1-2-3", []byte{0x01, 0x02, 0x03}, 0x7A},
		{"sum wraps to 0", []byte{0x7F, 0x01}, 0x00},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := checksum7(tc.in)
			if got != tc.want {
				t.Errorf("checksum7(%X) = %#x; want %#x", tc.in, got, tc.want)
			}
			if got&0x80 != 0 {
				t.Errorf("checksum7(%X) MSB not zero: %#x", tc.in, got)
			}
		})
	}
}

// TestEncodeDecodeNoPayload round-trips a bare command (MESSAGE length 0).
func TestEncodeDecodeNoPayload(t *testing.T) {
	const cmd byte = 0x07
	raw := EncodeFrame(cmd, nil)

	// SOM + cmd + checksum == 3 bytes.
	if len(raw) != 3 {
		t.Fatalf("encoded len = %d; want 3; raw=%X", len(raw), raw)
	}
	if raw[0] != SOM {
		t.Errorf("SOM = %#x; want 0xFF", raw[0])
	}
	if raw[1] != cmd {
		t.Errorf("cmd = %#x; want %#x", raw[1], cmd)
	}
	// checksum7({0x07}) = (-7) & 0x7F = 0x79
	if raw[2] != 0x79 {
		t.Errorf("checksum = %#x; want 0x79", raw[2])
	}

	gotCmd, gotPayload, n, err := DecodeFrame(raw)
	if err != nil {
		t.Fatalf("DecodeFrame: %v", err)
	}
	if n != len(raw) {
		t.Errorf("consumed = %d; want %d", n, len(raw))
	}
	if gotCmd != cmd {
		t.Errorf("cmd = %#x; want %#x", gotCmd, cmd)
	}
	if len(gotPayload) != 0 {
		t.Errorf("payload len = %d; want 0", len(gotPayload))
	}
}

// TestEncodeDecodeWithPayload round-trips a command with a non-empty
// MESSAGE. Verifies checksum covers COMMAND + MESSAGE.
func TestEncodeDecodeWithPayload(t *testing.T) {
	const cmd byte = 0x02
	payload := []byte{0x00, 0x10, 0x20, 0xAA}
	raw := EncodeFrame(cmd, payload)

	want := []byte{
		SOM,
		cmd,
		0x00, 0x10, 0x20, 0xAA,
		// checksum7({0x02, 0x00, 0x10, 0x20, 0xAA}) = (-(0xDC)) & 0x7F
		// = 0x24 & 0x7F = 0x24
		0x24,
	}
	if !bytes.Equal(raw, want) {
		t.Fatalf("encoded mismatch\n got %X\nwant %X", raw, want)
	}

	gotCmd, gotPayload, n, err := DecodeFrame(raw)
	if err != nil {
		t.Fatalf("DecodeFrame: %v", err)
	}
	if n != len(raw) {
		t.Errorf("consumed = %d; want %d", n, len(raw))
	}
	if gotCmd != cmd {
		t.Errorf("cmd = %#x; want %#x", gotCmd, cmd)
	}
	if !bytes.Equal(gotPayload, payload) {
		t.Errorf("payload = %X; want %X", gotPayload, payload)
	}
}

// TestEncodeTransparentBytes verifies that SW-P-02 does NOT DLE-escape:
// a 0x10 or 0xFF inside MESSAGE rides the wire unchanged.
func TestEncodeTransparentBytes(t *testing.T) {
	raw := EncodeFrame(0x01, []byte{0x10, 0xFF, 0x00})
	// No byte doubling — MESSAGE appears verbatim between cmd and
	// checksum.
	want := []byte{
		SOM,
		0x01,
		0x10, 0xFF, 0x00,
		// checksum7({0x01, 0x10, 0xFF, 0x00}) = (-(0x110)) & 0x7F
		// = (-0x10) & 0x7F = 0x70 …  actually 0x110 & 0xFF = 0x10,
		// (-0x10) & 0x7F = 0x70.
		0x70,
	}
	if !bytes.Equal(raw, want) {
		t.Fatalf("transparent encoding mismatch\n got %X\nwant %X", raw, want)
	}
	// Round-trip.
	_, got, _, err := DecodeFrame(raw)
	if err != nil {
		t.Fatalf("DecodeFrame: %v", err)
	}
	if !bytes.Equal(got, []byte{0x10, 0xFF, 0x00}) {
		t.Errorf("payload round-trip = %X; want 10 FF 00", got)
	}
}

// TestDecodeRejectsBadSOM: any first byte other than 0xFF is rejected.
func TestDecodeRejectsBadSOM(t *testing.T) {
	_, _, _, err := DecodeFrame([]byte{0x00, 0x07, 0x79})
	if !errors.Is(err, ErrBadSOM) {
		t.Errorf("got %v; want ErrBadSOM", err)
	}
}

// TestDecodeRejectsBadChecksum: a tampered checksum byte is caught.
func TestDecodeRejectsBadChecksum(t *testing.T) {
	_, _, _, err := DecodeFrame([]byte{SOM, 0x07, 0x00}) // correct is 0x79
	if !errors.Is(err, ErrBadChecksum) {
		t.Errorf("got %v; want ErrBadChecksum", err)
	}
}

// TestDecodeTooShort: a buffer with fewer than 3 bytes (SOM + cmd +
// checksum) returns io.ErrUnexpectedEOF so stream readers know to keep
// accumulating.
func TestDecodeTooShort(t *testing.T) {
	for _, buf := range [][]byte{nil, {SOM}, {SOM, 0x07}} {
		_, _, _, err := DecodeFrame(buf)
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Errorf("DecodeFrame(%X) err = %v; want ErrUnexpectedEOF", buf, err)
		}
	}
}

// TestPackUnpackFrame verifies Pack + Unpack (Frame-level wrappers)
// agree with EncodeFrame + DecodeFrame.
func TestPackUnpackFrame(t *testing.T) {
	f := Frame{ID: CommandID(0x10), Payload: []byte{0x00, 0x01}}
	raw := Pack(f)
	got, n, err := Unpack(raw)
	if err != nil {
		t.Fatalf("Unpack: %v", err)
	}
	if n != len(raw) {
		t.Errorf("consumed = %d; want %d", n, len(raw))
	}
	if got.ID != f.ID {
		t.Errorf("ID = %#x; want %#x", got.ID, f.ID)
	}
	if !bytes.Equal(got.Payload, f.Payload) {
		t.Errorf("payload = %X; want %X", got.Payload, f.Payload)
	}
}
