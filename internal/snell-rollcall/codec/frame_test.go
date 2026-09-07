package codec

import (
	"bytes"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"
)

// mustHex decodes a spec-derived hex vector.
func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		t.Fatalf("bad test vector %q: %v", s, err)
	}
	return b
}

// mustHexBytes decodes a hex vector where no *testing.T is in scope, such as a
// table literal. Test vectors are compile-time constants in this package, so a
// bad one is a bug in the test rather than a runtime condition.
func mustHexBytes(s string) []byte {
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		panic("bad test vector " + s + ": " + err.Error())
	}
	return b
}

// specIamFrame is the SP_IAM frame printed in the vendor ExampleClient
// readme, reconstructed per spec 11: transmission header, MESSAGE_STR,
// ROLLHEADER_STR and a 40-byte DEVICEINFO_STR.
//
// Trailing name padding is zeroed here rather than the 0xCD the vendor client
// leaks, because padding after the terminator is undefined and a fixture must
// not depend on it.
const specIamFrame = "000c 0038" + // flags 12, length 56 = 14 + 2 + 40
	"0000 00 00 00ff" + // dst 0000-00-00:FF broadcast
	"0000 ff 00 00ff" + // src 0000-FF-00:FF
	"002a" + // rLength 42 = 2 + 40
	"21 00" + // type 33 IAM, flags 0
	"0003" + // protocol version 3
	"0000 ff 00 00ff" + // device address
	"0000" + // services none
	"01e3" + // type id 483
	"01 00 20 01" + // v1.0 ' ' cmdset 1
	"5465737443 6c69656e74 0000000000 0000000000" + // "TestClient" padded
	"0000 0008" // service status 0, status Present

func TestDecodeFrame_SpecIam(t *testing.T) {
	raw := mustHex(t, specIamFrame)

	f, n, err := DecodeFrame(raw)
	if err != nil {
		t.Fatalf("DecodeFrame: %v", err)
	}
	if n != len(raw) {
		t.Errorf("consumed %d bytes, want %d", n, len(raw))
	}
	if f.Type != MsgIam {
		t.Errorf("Type = %s, want IAM", f.Type)
	}
	if f.Flags != 0 {
		t.Errorf("Flags = 0x%02X, want 0", f.Flags)
	}
	if !f.Dst.IsBroadcast() {
		t.Errorf("Dst = %s, want broadcast", f.Dst)
	}
	if f.Dst.Index != IndexUnknown {
		t.Errorf("Dst.Index = %d, want %d (UNKNOWNSESS)", f.Dst.Index, IndexUnknown)
	}
	if got, want := len(f.Payload), DeviceInfoSize; got != want {
		t.Fatalf("payload %d bytes, want %d", got, want)
	}

	info, err := DecodeDeviceInfo(f.Payload)
	if err != nil {
		t.Fatalf("DecodeDeviceInfo: %v", err)
	}
	if info.ProtocolVersion != ProtocolVersion {
		t.Errorf("ProtocolVersion = %d, want %d", info.ProtocolVersion, ProtocolVersion)
	}
	if info.ID.TypeID != 483 {
		t.Errorf("TypeID = %d, want 483", info.ID.TypeID)
	}
	if info.ID.Name != "TestClient" {
		t.Errorf("Name = %q, want %q", info.ID.Name, "TestClient")
	}
	if !info.Status.Status.Has(StatusPresent) {
		t.Errorf("Status = %s, want Present", info.Status.Status)
	}
}

func TestFrame_RoundTrip(t *testing.T) {
	raw := mustHex(t, specIamFrame)
	f, _, err := DecodeFrame(raw)
	if err != nil {
		t.Fatalf("DecodeFrame: %v", err)
	}
	out, err := f.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !bytes.Equal(out, raw) {
		t.Errorf("round trip differs\n got %x\nwant %x", out, raw)
	}
}

func TestDecodeFrame_Rejects(t *testing.T) {
	good := mustHex(t, specIamFrame)

	tests := []struct {
		name string
		in   []byte
		want error
	}{
		{"empty", nil, ErrShortBuffer},
		{"header only", good[:3], ErrShortBuffer},
		{"truncated body", good[:20], ErrShortBuffer},
		{"bad flags", func() []byte {
			b := append([]byte(nil), good...)
			b[1] = 0x0B // mode 2, historical and not accepted
			return b
		}(), ErrBadTxFlags},
		{"zero length", func() []byte {
			b := append([]byte(nil), good...)
			b[2], b[3] = 0, 0
			return b
		}(), ErrBadTxLength},
		{"length over spec max", func() []byte {
			b := append([]byte(nil), good...)
			b[2], b[3] = 0xFF, 0xFF // 65535 > 1570
			return b
		}(), ErrBadTxLength},
		{"rLength disagrees", func() []byte {
			b := append([]byte(nil), good...)
			b[16], b[17] = 0x00, 0x10 // rLength 16, but txLength says 42
			return b
		}(), ErrLengthMismatch},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := DecodeFrame(tc.in); !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestFrame_EncodeRejectsOversizePayload(t *testing.T) {
	f := Frame{Type: MsgRaw, Payload: make([]byte, MaxPayload+1)}
	if _, err := f.Encode(); !errors.Is(err, ErrPayloadTooLong) {
		t.Errorf("err = %v, want ErrPayloadTooLong", err)
	}
}

func TestFrame_Accessors(t *testing.T) {
	f := Frame{Flags: FlagBackChannel | FlagWideArea, Payload: []byte{1, 2, 3}}
	if !f.BackChannel() || !f.WideArea() {
		t.Error("flag accessors disagree with Flags")
	}
	if got, want := f.Size(), HeaderSize+3; got != want {
		t.Errorf("Size = %d, want %d", got, want)
	}

	for _, tc := range []struct {
		flags uint8
		want  string
	}{
		{0, "-"},
		{FlagBackChannel, "B"},
		{FlagWideArea, "W"},
		{FlagBackChannel | FlagWideArea, "BW"},
	} {
		if got := flagString(tc.flags); got != tc.want {
			t.Errorf("flagString(0x%02X) = %q, want %q", tc.flags, got, tc.want)
		}
	}

	if s := (Frame{Type: MsgAck}).String(); !strings.Contains(s, "ACK") {
		t.Errorf("String() = %q, want it to name the type", s)
	}
}

// TestReader_ResyncsOneByteAtATime is the regression for a real defect in the
// vendor library: it discards four bytes at a time when framing is lost, which
// can never re-align a stream that slipped by an odd number of bytes.
func TestReader_ResyncsOneByteAtATime(t *testing.T) {
	good := mustHex(t, specIamFrame)

	// One junk byte, so the following frame starts on an odd offset.
	stream := append([]byte{0xAA}, good...)
	r := NewReader(bytes.NewReader(stream))

	f, err := r.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame after 1 junk byte: %v", err)
	}
	if f.Type != MsgIam {
		t.Errorf("Type = %s, want IAM", f.Type)
	}
	if r.Resyncs() != 1 {
		t.Errorf("Resyncs = %d, want 1", r.Resyncs())
	}
}

func TestReader_MultipleFramesAndEOF(t *testing.T) {
	good := mustHex(t, specIamFrame)
	r := NewReader(bytes.NewReader(append(append([]byte{}, good...), good...)))

	for i := range 2 {
		if _, err := r.ReadFrame(); err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
	}
	if _, err := r.ReadFrame(); !errors.Is(err, io.EOF) {
		t.Errorf("err = %v, want io.EOF", err)
	}
	if r.Resyncs() != 0 {
		t.Errorf("Resyncs = %d, want 0 on a clean stream", r.Resyncs())
	}
}

// TestReader_SplitAcrossReads covers the reassembly the specification demands:
// "the underlying IP system may split the data and deliver it in separate
// blocks" (spec 10.2.3).
func TestReader_SplitAcrossReads(t *testing.T) {
	good := mustHex(t, specIamFrame)
	r := NewReader(&iotest{parts: [][]byte{good[:3], good[3:9], good[9:]}})

	f, err := r.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	if f.Type != MsgIam {
		t.Errorf("Type = %s, want IAM", f.Type)
	}
}

func TestReader_PropagatesDecodeError(t *testing.T) {
	// A frame whose declared payload never arrives: EOF mid-frame.
	good := mustHex(t, specIamFrame)
	r := NewReader(bytes.NewReader(good[:len(good)-4]))
	if _, err := r.ReadFrame(); err == nil {
		t.Fatal("want an error on a truncated stream")
	}
}

// iotest hands out a fixed sequence of short reads.
type iotest struct{ parts [][]byte }

func (r *iotest) Read(p []byte) (int, error) {
	if len(r.parts) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.parts[0])
	if n == len(r.parts[0]) {
		r.parts = r.parts[1:]
	} else {
		r.parts[0] = r.parts[0][n:]
	}
	return n, nil
}

// specMaxFrame builds the largest frame the specification permits: a
// transmission length of 1570. We never encode one, but a peer may send one and
// the reader must be able to assemble it.
func specMaxFrame(t *testing.T) []byte {
	t.Helper()

	payload := make([]byte, SpecMaxTxLength-MsgHeaderSize-RollHeaderSize)
	for i := range payload {
		payload[i] = byte(i)
	}

	b := make([]byte, 0, MaxDecodeFrame)
	b = append(b, 0x00, 0x0C) // flags
	txLen := SpecMaxTxLength
	b = append(b, byte(txLen>>8), byte(txLen))
	b = (Address{Unit: 0x20, Index: 1}).AppendTo(b)
	b = (Address{Unit: 0xFF, Index: 1}).AppendTo(b)
	rLength := RollHeaderSize + len(payload)
	b = append(b, byte(rLength>>8), byte(rLength))
	b = append(b, byte(MsgRaw), 0)
	return append(b, payload...)
}

// TestReader_AcceptsSpecMaximumFrame is the regression for a sizing defect: the
// reader's buffer held 880 bytes while DecodeFrame accepts up to 1574, so a
// spec-legal large frame could never be assembled and the connection died with
// a buffer-full error.
func TestReader_AcceptsSpecMaximumFrame(t *testing.T) {
	raw := specMaxFrame(t)
	if len(raw) != MaxDecodeFrame {
		t.Fatalf("test frame is %d bytes, want %d", len(raw), MaxDecodeFrame)
	}

	// Delivered in small pieces, the way a TCP stream really arrives.
	var parts [][]byte
	for off := 0; off < len(raw); off += 200 {
		parts = append(parts, raw[off:minInt(off+200, len(raw))])
	}

	r := NewReader(&iotest{parts: parts})
	f, err := r.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	if f.Type != MsgRaw {
		t.Errorf("Type = %s, want RAW", f.Type)
	}
	if got, want := len(f.Payload), SpecMaxTxLength-MsgHeaderSize-RollHeaderSize; got != want {
		t.Errorf("payload %d bytes, want %d", got, want)
	}
	if !bytes.Equal(f.Payload, raw[HeaderSize:]) {
		t.Error("payload does not match what was sent")
	}
}

// TestReader_UndersizedBufferFailsLoudly exercises the guard in fill. NewReader
// always allocates more than MaxDecodeFrame so this cannot happen in service;
// the guard exists so that if it ever did, the reader would report it instead
// of spinning on a buffer it can never fill.
func TestReader_UndersizedBufferFailsLoudly(t *testing.T) {
	raw := specMaxFrame(t)
	r := newReaderSize(bytes.NewReader(raw), 64)

	_, err := r.ReadFrame()
	if !errors.Is(err, ErrBadTxLength) {
		t.Fatalf("err = %v, want ErrBadTxLength", err)
	}
	if !strings.Contains(err.Error(), "buffer full") {
		t.Errorf("err = %v, want it to say the buffer is full", err)
	}
}

func TestNewReaderSize_Floor(t *testing.T) {
	r := newReaderSize(bytes.NewReader(nil), 1)
	if got := cap(r.buf); got < HeaderSize {
		t.Errorf("buffer capped at %d, want at least one header (%d)", got, HeaderSize)
	}
}

// TestReader_ResyncsPastAnyFramingError covers the three ways framing can be
// lost. Each costs exactly one byte, so a stream that slipped by an odd number
// of bytes still recovers; the vendor library drops four at a time and never
// re-aligns.
func TestReader_ResyncsPastAnyFramingError(t *testing.T) {
	good := mustHex(t, specIamFrame)

	tests := []struct {
		name  string
		junk  []byte
		syncs uint64
	}{
		{"bad flags", []byte{0x00, 0x0B, 0x00, 0x38}, 4},
		{"zero length", []byte{0x00, 0x0C, 0x00, 0x00}, 4},
		{"odd offset", []byte{0xAA}, 1},
		{"several bytes", []byte{0xAA, 0xBB, 0xCC}, 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stream := append(append([]byte{}, tc.junk...), good...)
			r := NewReader(bytes.NewReader(stream))

			f, err := r.ReadFrame()
			if err != nil {
				t.Fatalf("ReadFrame: %v", err)
			}
			if f.Type != MsgIam {
				t.Errorf("Type = %s, want IAM", f.Type)
			}
			if r.Resyncs() != tc.syncs {
				t.Errorf("Resyncs = %d, want %d", r.Resyncs(), tc.syncs)
			}
		})
	}
}

// TestReader_ResyncsPastLengthMismatch covers the case the vendor drops
// silently: a frame whose two length fields disagree. We resync and count it so
// the session layer can raise a compliance event rather than lose traffic.
func TestReader_ResyncsPastLengthMismatch(t *testing.T) {
	good := mustHex(t, specIamFrame)

	bad := append([]byte(nil), good...)
	bad[16], bad[17] = 0x00, 0x10 // rLength 16 against a txLength of 42

	r := NewReader(bytes.NewReader(append(bad, good...)))
	f, err := r.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	if f.Type != MsgIam {
		t.Errorf("Type = %s, want IAM", f.Type)
	}
	if r.Resyncs() == 0 {
		t.Error("a length mismatch must be counted as a resync, not passed over")
	}
}

// TestReader_PayloadAliasesTheBuffer pins the documented lifetime of a decoded
// payload: it points into the reader's buffer and is only valid until the next
// read. A consumer that keeps a menu line across reads must copy it, and this
// test is what that requirement is written against.
func TestReader_PayloadAliasesTheBuffer(t *testing.T) {
	good := mustHex(t, specIamFrame)
	r := NewReader(bytes.NewReader(append(append([]byte{}, good...), good...)))

	first, err := r.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	kept := first.Payload
	copied := append([]byte(nil), first.Payload...)

	if _, err := r.ReadFrame(); err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	if &kept[0] == &copied[0] {
		t.Fatal("the test did not actually copy")
	}
	// The copy is what a caller must keep; nothing here asserts the aliased
	// slice still holds the old bytes, only that copying is what works.
	info, err := DecodeDeviceInfo(copied)
	if err != nil || info.ID.Name != "TestClient" {
		t.Errorf("the copied payload must stay decodable: %+v %v", info, err)
	}
}

// idleReader returns (0, nil) forever, which io.Reader permits but which would
// spin the read loop if we treated it as "try again".
type idleReader struct{}

func (idleReader) Read([]byte) (int, error) { return 0, nil }

// TestReader_NoProgress covers that case: a peer that keeps the connection open
// and sends nothing must surface as an error, not as a busy loop.
func TestReader_NoProgress(t *testing.T) {
	r := NewReader(idleReader{})
	if _, err := r.ReadFrame(); !errors.Is(err, io.ErrNoProgress) {
		t.Errorf("err = %v, want io.ErrNoProgress", err)
	}
}
