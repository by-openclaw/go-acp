package codec

import (
	"errors"
	"io"
	"testing"
)

// TestUnpackMoreErrors covers the Unpack error arms not exercised by
// TestUnpackErrors in framer_test.go: the too-short buffer, a lone
// trailing DLE, a stray "DLE X" (X neither DLE nor ETX), and a frame
// whose unescaped body is shorter than ID+BTC+CHK.
func TestUnpackMoreErrors(t *testing.T) {
	t.Run("buffer shorter than SOM", func(t *testing.T) {
		_, _, err := Unpack([]byte{0x10})
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Errorf("got %v; want io.ErrUnexpectedEOF", err)
		}
	})

	t.Run("lone trailing DLE", func(t *testing.T) {
		// DLE STX 0x01 DLE  — the DLE at the end has no following byte,
		// so the escape scanner reports an incomplete frame.
		_, _, err := Unpack([]byte{0x10, 0x02, 0x01, 0x10})
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Errorf("got %v; want io.ErrUnexpectedEOF", err)
		}
	})

	t.Run("stray DLE X", func(t *testing.T) {
		// DLE STX 0x01 DLE 0xAA — 0xAA after a DLE is neither a doubled
		// DLE nor ETX, so framing is corrupt → ErrTruncated.
		_, _, err := Unpack([]byte{0x10, 0x02, 0x01, 0x10, 0xAA})
		if !errors.Is(err, ErrTruncated) {
			t.Errorf("got %v; want ErrTruncated", err)
		}
	})

	t.Run("body shorter than ID+BTC+CHK", func(t *testing.T) {
		// DLE STX 0x01 DLE ETX — body unescapes to a single byte, fewer
		// than the 3 (ID+BTC+CHK) required → ErrEmptyFrame.
		_, _, err := Unpack([]byte{0x10, 0x02, 0x01, 0x10, 0x03})
		if !errors.Is(err, ErrEmptyFrame) {
			t.Errorf("got %v; want ErrEmptyFrame", err)
		}
	})
}

// TestUnpackEmptyDataGuard drives the defensive len(data)==0 arm in
// Unpack via the transparent unpackDataHook seam. That arm is shadowed
// in production by the len(body)<3 check (the only zero-DATA body shape
// is rejected earlier), so the seam empties DATA after the body-length
// and BTC/checksum checks pass. The crafted frame carries btc=0 and
// chk=0 so the (now empty) data still satisfies the BTC and checksum
// guards, isolating the ErrEmptyFrame arm.
func TestUnpackEmptyDataGuard(t *testing.T) {
	orig := unpackDataHook
	unpackDataHook = func([]byte) []byte { return nil }
	defer func() { unpackDataHook = orig }()

	// body = {0xAA, 0x00, 0x00}: data=[0xAA], btc=0x00, chk=0x00.
	// Checksum8([0x00]) == 0x00, so the checksum check passes once the
	// hook has emptied data (btc 0 == len(empty data) 0).
	frame := []byte{0x10, 0x02, 0xAA, 0x00, 0x00, 0x10, 0x03}
	_, _, err := Unpack(frame)
	if !errors.Is(err, ErrEmptyFrame) {
		t.Errorf("got %v; want ErrEmptyFrame", err)
	}
}
