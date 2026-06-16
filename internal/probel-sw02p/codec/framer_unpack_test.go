package codec

import (
	"errors"
	"io"
	"testing"
)

// TestUnpackVariableLenNeedsMoreBytes covers the isVariableLenCommand
// true arm of Unpack: a known variable-length command (tx 100 Extended
// PROTECT TALLY DUMP) whose MESSAGE bytes have not yet fully arrived
// must return io.ErrUnexpectedEOF (keep reading), NOT ErrUnknownCommand.
//
// The Count byte declares 5 entries (1 + 4*5 = 21 MESSAGE bytes) but
// only the Count byte is buffered, so PayloadSize returns the full size
// and the want-length check trips io.ErrUnexpectedEOF.
func TestUnpackVariableLenNeedsMoreBytes(t *testing.T) {
	// SOM + cmd(100=0x64) + Count=5 + a stray byte → 4 bytes total, far
	// short of the 1+1+21+1 = 24 the dump needs.
	buf := []byte{SOM, 0x64, 0x05, 0x00}
	_, _, err := Unpack(buf)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("partial tx100: got %v; want io.ErrUnexpectedEOF", err)
	}
}

// TestUnpackVariableLenSizerNotReady covers the other isVariableLenCommand
// arm: a variable-length command whose size cannot even be computed yet
// because the length-determining header byte is missing. tx 100 with no
// Count byte buffered → tallyDumpPayloadSize returns (0,false) →
// isVariableLenCommand true → io.ErrUnexpectedEOF.
func TestUnpackVariableLenSizerNotReady(t *testing.T) {
	// Only SOM + cmd + one trailing byte; the Count peek is buf[2:] = 1
	// byte, which IS enough for tallyDumpPayloadSize. Use tx 076 instead,
	// whose sizer needs 4 header bytes — give it fewer.
	buf := []byte{SOM, 0x4C, 0x00, 0x00} // tx076, only 2 of 4 map bytes
	_, _, err := Unpack(buf)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("partial tx076 header: got %v; want io.ErrUnexpectedEOF", err)
	}
}

// TestUnpackBadChecksum covers the ErrBadChecksum arm of Unpack on a
// fully-buffered, known fixed-length frame whose trailing checksum byte
// has been corrupted.
func TestUnpackBadChecksum(t *testing.T) {
	wire := Pack(Frame{ID: RxConnectOnGo, Payload: []byte{0x00, 0x01, 0x02}})
	wire[len(wire)-1] ^= 0x01 // flip a checksum bit
	_, _, err := Unpack(wire)
	if !errors.Is(err, ErrBadChecksum) {
		t.Errorf("corrupt checksum: got %v; want ErrBadChecksum", err)
	}
}

// TestUnpackBadSOM covers the ErrBadSOM arm of Unpack (leading byte not
// 0xFF).
func TestUnpackBadSOM(t *testing.T) {
	_, _, err := Unpack([]byte{0x00, 0x06, 0x00, 0x7A})
	if !errors.Is(err, ErrBadSOM) {
		t.Errorf("bad SOM: got %v; want ErrBadSOM", err)
	}
}

// TestUnpackTooShort covers the < 3 byte minimum arm of Unpack.
func TestUnpackTooShort(t *testing.T) {
	for _, buf := range [][]byte{nil, {SOM}, {SOM, 0x06}} {
		if _, _, err := Unpack(buf); !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Errorf("Unpack(%X): got %v; want io.ErrUnexpectedEOF", buf, err)
		}
	}
}
