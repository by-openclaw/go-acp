package s101

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"dhs/internal/errcode"
)

// TestS101_Sentinels_ShapeAndClass pins every code's wire shape and
// exit class — the contract scripts grep on.
func TestS101_Sentinels_ShapeAndClass(t *testing.T) {
	cases := []struct {
		code *errcode.Code
		want string
	}{
		{ErrBadFrame, "s101:bad-frame"},
		{ErrBadCRC, "s101:crc-mismatch"},
		{ErrTruncated, "s101:truncated"},
		{ErrFrameTooLarge, "s101:frame-too-large"},
		{ErrReadFailed, "s101:read-failed"},
		{ErrWriteFailed, "s101:write-failed"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			if got := tc.code.Error(); got != tc.want {
				t.Errorf("Error() = %q, want %q", got, tc.want)
			}
			if tc.code.Layer != errcode.LayerS101 {
				t.Errorf("Layer = %q, want %q", tc.code.Layer, errcode.LayerS101)
			}
			if tc.code.Class != errcode.ClassRuntime {
				t.Errorf("Class = %d, want ClassRuntime (1)", tc.code.Class)
			}
			if got := errcode.Exit(tc.code); got != 1 {
				t.Errorf("Exit() = %d, want 1", got)
			}
		})
	}
}

// TestDecode_BadFrame_ReturnsTypedError pins that Decode wraps the typed
// ErrBadFrame sentinel so callers can errors.Is against it.
func TestDecode_BadFrame_ReturnsTypedError(t *testing.T) {
	// Frame missing BOF.
	_, err := Decode([]byte{0x00, 0x0E, 0x00, 0x01, EOF})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrBadFrame) {
		t.Errorf("err = %v, want errors.Is(err, ErrBadFrame)", err)
	}
}

// TestDecode_BadCRC_ReturnsTypedError pins ErrBadCRC.
func TestDecode_BadCRC_ReturnsTypedError(t *testing.T) {
	// Encode a valid frame then corrupt the CRC.
	good := Encode(&Frame{
		Slot: SlotDefault, MsgType: MsgKeepAlive, Command: CmdKeepAliveReq, Version: VersionS101,
	})
	// good = BOF + escaped[slot,msgType,cmd,ver,crc_lo,crc_hi] + EOF.
	// Flip a CRC byte (second-to-last before EOF in the unescaped stream).
	bad := append([]byte{}, good...)
	// Find a content byte and flip it — flip byte at index 2 (msgType)
	// to break the CRC while keeping framing valid.
	bad[2] ^= 0x01
	_, err := Decode(bad)
	if err == nil {
		t.Fatal("expected CRC error, got nil")
	}
	if !errors.Is(err, ErrBadCRC) {
		t.Errorf("err = %v, want errors.Is(err, ErrBadCRC)", err)
	}
}

// TestDecode_Truncated_ReturnsTypedError pins ErrTruncated.
func TestDecode_Truncated_ReturnsTypedError(t *testing.T) {
	// BOF + EOF with no content between → too short to be a frame.
	_, err := Decode([]byte{BOF, EOF})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrTruncated) {
		t.Errorf("err = %v, want errors.Is(err, ErrTruncated)", err)
	}
}

// TestReader_WriteFailure_TypedError pins ErrWriteFailed wrapping.
func TestWriter_WriteFailure_TypedError(t *testing.T) {
	w := NewWriter(&failingWriter{})
	err := w.WriteFrame(&Frame{
		Slot: SlotDefault, MsgType: MsgKeepAlive, Command: CmdKeepAliveReq, Version: VersionS101,
	})
	if err == nil {
		t.Fatal("expected write to fail, got nil")
	}
	if !errors.Is(err, ErrWriteFailed) {
		t.Errorf("err = %v, want errors.Is(err, ErrWriteFailed)", err)
	}
	if !strings.HasPrefix(err.Error(), "s101:write-failed:") {
		t.Errorf("err string = %q, want s101:write-failed: prefix", err.Error())
	}
}

// TestReader_ReadFailure_TypedError pins ErrReadFailed wrapping.
func TestReader_ReadFailure_TypedError(t *testing.T) {
	// A reader that errors on first read.
	r := NewReader(&failingReader{})
	_, err := r.ReadFrame()
	if err == nil {
		t.Fatal("expected read to fail, got nil")
	}
	if !errors.Is(err, ErrReadFailed) {
		t.Errorf("err = %v, want errors.Is(err, ErrReadFailed)", err)
	}
	if !strings.HasPrefix(err.Error(), "s101:read-failed:") {
		t.Errorf("err string = %q, want s101:read-failed: prefix", err.Error())
	}
}

// TestReader_FrameTooLarge_TypedError pins ErrFrameTooLarge by feeding
// a >64 KiB stream of BOF + filler with no EOF.
func TestReader_FrameTooLarge_TypedError(t *testing.T) {
	// 65538 bytes: BOF + filler. The filler avoids BOF/EOF so the reader
	// keeps collecting and never resyncs.
	junk := make([]byte, 65538)
	junk[0] = BOF
	for i := 1; i < len(junk); i++ {
		junk[i] = 0x42 // not BOF (0xFE) and not EOF (0xFF)
	}
	r := NewReader(bytes.NewReader(junk))
	_, err := r.ReadFrame()
	if err == nil {
		t.Fatal("expected frame-too-large error, got nil")
	}
	if !errors.Is(err, ErrFrameTooLarge) {
		t.Errorf("err = %v, want errors.Is(err, ErrFrameTooLarge)", err)
	}
}

// failingWriter always returns an IO error on Write.
type failingWriter struct{}

func (failingWriter) Write(p []byte) (int, error) {
	return 0, errors.New("simulated write error")
}

// failingReader returns an IO error on the first ReadByte.
type failingReader struct{}

func (failingReader) Read(p []byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}
