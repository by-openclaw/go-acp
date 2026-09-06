package s101

import (
	"bytes"
	"errors"
	"testing"
)

// frameFromContent wraps raw header+payload content with a valid CRC and
// BOF/EOF, applying the same 0xFD escaping as Encode. Used to craft
// frames with arbitrary (non-default) header shapes for decode tests.
func frameFromContent(content []byte) []byte {
	raw := append([]byte(nil), content...)
	crc := crcCCITT16(content)
	raw = append(raw, byte(crc&0xFF), byte(crc>>8))
	out := []byte{BOF}
	for _, b := range raw {
		if b >= 0xF8 {
			out = append(out, ESC, b^0x20)
		} else {
			out = append(out, b)
		}
	}
	return append(out, EOF)
}

// errReaderWriter is a deliberately failing io.Reader/io.Writer used to
// exercise the ErrReadFailed / ErrWriteFailed paths.
type errReaderWriter struct{ err error }

func (e errReaderWriter) Read(p []byte) (int, error)  { return 0, e.err }
func (e errReaderWriter) Write(p []byte) (int, error) { return 0, e.err }

// TestIsEmBER pins the MsgType discriminator used by the session read
// loop to route Glow payloads vs keep-alive frames.
func TestIsEmBER(t *testing.T) {
	if !NewEmBERFrame([]byte{0x01}).IsEmBER() {
		t.Error("EmBER frame should report IsEmBER()=true")
	}
	// A frame with a non-EmBER message type must report false.
	if (&Frame{MsgType: 0x00}).IsEmBER() {
		t.Error("non-EmBER msgType should report IsEmBER()=false")
	}
}

// TestDecode_BadFrame covers the missing-BOF/EOF rejection and the
// too-short input guard.
func TestDecode_BadFrame(t *testing.T) {
	cases := map[string][]byte{
		"empty":  {},
		"single": {BOF},
		"no-bof": {0x00, 0x01, EOF},
		"no-eof": {BOF, 0x01, 0x02},
	}
	for name, in := range cases {
		if _, err := Decode(in); !errors.Is(err, ErrBadFrame) {
			t.Errorf("%s: got %v, want ErrBadFrame", name, err)
		}
	}
}

// TestDecode_Truncated covers both truncation guards: content shorter
// than the 4-byte keep-alive header, and an EmBER frame whose content is
// at least 4 but fewer than 5 bytes (no flags byte).
func TestDecode_Truncated(t *testing.T) {
	// raw must be < 4 bytes to hit the first guard: content 1 byte + CRC 2
	// bytes = raw 3.
	if _, err := Decode(frameFromContent([]byte{0xAA})); !errors.Is(err, ErrTruncated) {
		t.Errorf("short content: got %v, want ErrTruncated", err)
	}

	// EmBER (command 0x00) with exactly 4 content bytes -> len(content) 4 < 5
	// after the flags check triggers the second ErrTruncated guard.
	content := []byte{SlotDefault, MsgEmBER, CmdEmBER, VersionS101}
	if _, err := Decode(frameFromContent(content)); !errors.Is(err, ErrTruncated) {
		t.Errorf("missing flags byte: got %v, want ErrTruncated", err)
	}
}

// TestDecode_ShortHeaderFallback covers the 5-byte-header read path: a
// provider following the strict spec reading emits no DTD app-bytes, so
// offset 5 is treated as start-of-payload (hasFullHeader == false).
func TestDecode_ShortHeaderFallback(t *testing.T) {
	// 5-byte header (slot,msgType,cmd,ver,flags) + payload, no DTD bytes.
	payload := []byte{0x60, 0x01, 0x00}
	content := append([]byte{SlotDefault, MsgEmBER, CmdEmBER, VersionS101, FlagSingle}, payload...)
	got, err := Decode(frameFromContent(content))
	if err != nil {
		t.Fatalf("Decode short header: %v", err)
	}
	if got.DTD != 0 || got.DTDMinor != 0 || got.DTDMajor != 0 {
		t.Errorf("short header should leave DTD fields zero, got DTD=0x%02X %d.%d",
			got.DTD, got.DTDMajor, got.DTDMinor)
	}
	if !bytes.Equal(got.Payload, payload) {
		t.Errorf("payload: got %X, want %X", got.Payload, payload)
	}

	// Exactly-5-byte content (flags present, no payload) — fallback with
	// no trailing payload bytes.
	content5 := []byte{SlotDefault, MsgEmBER, CmdEmBER, VersionS101, FlagSingle}
	got5, err := Decode(frameFromContent(content5))
	if err != nil {
		t.Fatalf("Decode 5-byte content: %v", err)
	}
	if len(got5.Payload) != 0 {
		t.Errorf("5-byte content should yield empty payload, got %X", got5.Payload)
	}
}

// TestDecode_FullHeaderNoPayload covers the full-header path where the
// frame ends right after the 9-byte header (len(content) == 9).
func TestDecode_FullHeaderNoPayload(t *testing.T) {
	content := []byte{SlotDefault, MsgEmBER, CmdEmBER, VersionS101, FlagSingle,
		DTDGlow, AppBytesLen, DTDMinorVersion, DTDMajorVersion}
	got, err := Decode(frameFromContent(content))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(got.Payload) != 0 {
		t.Errorf("expected empty payload, got %X", got.Payload)
	}
	if got.DTDMinor != DTDMinorVersion {
		t.Errorf("DTDMinor not read: 0x%02X", got.DTDMinor)
	}
}

// TestEncode_CustomDTDOverride exercises the non-default minor/major
// branches in Encode (caller-supplied 0-values fall back; non-zero
// pass through).
func TestEncode_CustomDTDOverride(t *testing.T) {
	f := NewEmBERFrame([]byte{0x60, 0x00})
	f.DTDMinor = 0x0A
	f.DTDMajor = 0x02
	got, err := Decode(Encode(f))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.DTDMinor != 0x0A || got.DTDMajor != 0x02 {
		t.Errorf("custom DTD not honoured: %d.%d", got.DTDMajor, got.DTDMinor)
	}

	// Zero-valued DTD bytes must fall back to the canonical 2.60 defaults
	// (Encode minor==0 / major==0 branches). A raw Frame built without
	// NewEmBERFrame leaves them zero.
	zero := &Frame{MsgType: MsgEmBER, Command: CmdEmBER, Flags: FlagSingle, Payload: []byte{0x60, 0x00}}
	gotZero, err := Decode(Encode(zero))
	if err != nil {
		t.Fatalf("Decode zero-DTD: %v", err)
	}
	if gotZero.DTDMinor != DTDMinorVersion || gotZero.DTDMajor != DTDMajorVersion {
		t.Errorf("zero DTD should default to %d.%d, got %d.%d",
			DTDMajorVersion, DTDMinorVersion, gotZero.DTDMajor, gotZero.DTDMinor)
	}
}

// TestReader_Resync covers the second-BOF resync branch: a junk preamble
// beginning with a raw 0xFE before the real frame is dropped, and the
// real frame decodes cleanly.
func TestReader_Resync(t *testing.T) {
	payload := []byte{0x30, 0x03, 0x02, 0x01, 0x2A}
	wire := Encode(NewEmBERFrame(payload))

	var buf bytes.Buffer
	// Junk that STARTS with BOF (mid-frame), forcing a resync when the
	// reader hits the real frame's BOF.
	buf.Write([]byte{BOF, 0xD9, 0x5C, 0x80, 0x30})
	buf.Write(wire)

	got, err := NewReader(&buf).ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	if !bytes.Equal(got.Payload, payload) {
		t.Errorf("payload: got %X, want %X", got.Payload, payload)
	}
}

// TestReader_Tap covers SetTap + the tap-fired branch in ReadFrame.
func TestReader_Tap(t *testing.T) {
	wire := Encode(NewEmBERFrame([]byte{0x42}))
	r := NewReader(bytes.NewReader(wire))
	var captured []byte
	r.SetTap(func(b []byte) { captured = append([]byte(nil), b...) })
	if _, err := r.ReadFrame(); err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	if !bytes.Equal(captured, wire) {
		t.Errorf("tap captured %X, want %X", captured, wire)
	}
}

// TestReader_ReadByteError covers both ReadByte error paths: failure
// while scanning for BOF, and failure mid-content (BOF seen, stream dies
// before EOF).
func TestReader_ReadByteError(t *testing.T) {
	sentinel := errors.New("boom")
	// Fails before any BOF.
	if _, err := NewReader(errReaderWriter{err: sentinel}).ReadFrame(); !errors.Is(err, ErrReadFailed) {
		t.Errorf("scan error: got %v, want ErrReadFailed", err)
	}
	// BOF then EOF-on-stream before frame EOF: a reader yielding one BOF
	// then erroring.
	r := NewReader(&oneByteThenErr{b: BOF, err: sentinel})
	if _, err := r.ReadFrame(); !errors.Is(err, ErrReadFailed) {
		t.Errorf("content error: got %v, want ErrReadFailed", err)
	}
}

// oneByteThenErr returns one byte then a persistent error.
type oneByteThenErr struct {
	b    byte
	err  error
	done bool
}

func (o *oneByteThenErr) Read(p []byte) (int, error) {
	if o.done {
		return 0, o.err
	}
	o.done = true
	p[0] = o.b
	return 1, nil
}

// TestReader_FrameTooLarge covers the 64 KiB cap guard: a BOF followed by
// a flood of non-EOF bytes overflows the buffer.
func TestReader_FrameTooLarge(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteByte(BOF)
	// 70000 filler bytes, none of which are BOF/EOF.
	buf.Write(bytes.Repeat([]byte{0x10}, 70000))
	if _, err := NewReader(&buf).ReadFrame(); !errors.Is(err, ErrFrameTooLarge) {
		t.Errorf("got %v, want ErrFrameTooLarge", err)
	}
}

// TestReader_DecodeError covers both Decode-error wrapping branches:
// a short bad frame (<=64 bytes, includes hex) and a long bad frame
// (>64 bytes, truncated hex).
func TestReader_DecodeError(t *testing.T) {
	// Short bad frame: BOF, a few bytes with a wrong CRC, EOF.
	shortBad := []byte{BOF, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0x00, 0x00, EOF}
	if _, err := NewReader(bytes.NewReader(shortBad)).ReadFrame(); !errors.Is(err, ErrBadCRC) {
		t.Errorf("short bad frame: got %v, want ErrBadCRC", err)
	}

	// Long bad frame: >64 content bytes with a wrong CRC trailer.
	body := bytes.Repeat([]byte{0x10}, 100)
	long := append([]byte{BOF}, body...)
	long = append(long, 0x00, 0x00, EOF) // bogus CRC
	if _, err := NewReader(bytes.NewReader(long)).ReadFrame(); !errors.Is(err, ErrBadCRC) {
		t.Errorf("long bad frame: got %v, want ErrBadCRC", err)
	}
}

// TestWriter_Tap covers SetTap + the tap-fired branch in WriteFrame.
func TestWriter_Tap(t *testing.T) {
	var sink bytes.Buffer
	w := NewWriter(&sink)
	var captured []byte
	w.SetTap(func(b []byte) { captured = append([]byte(nil), b...) })
	if err := w.WriteFrame(NewEmBERFrame([]byte{0x42})); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}
	if !bytes.Equal(captured, sink.Bytes()) {
		t.Errorf("tap captured %X, want %X", captured, sink.Bytes())
	}
}

// TestWriter_WriteError covers the io.Writer error path.
func TestWriter_WriteError(t *testing.T) {
	w := NewWriter(errReaderWriter{err: errors.New("disk full")})
	if err := w.WriteFrame(NewEmBERFrame([]byte{0x01})); !errors.Is(err, ErrWriteFailed) {
		t.Errorf("got %v, want ErrWriteFailed", err)
	}
}
