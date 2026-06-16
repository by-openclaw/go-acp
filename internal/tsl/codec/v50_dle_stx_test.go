package codec

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestDLEFrame_RoundTrip_NoStuffing(t *testing.T) {
	// PBC=2 LE + 2 body bytes = 4-byte "packet" with valid internal PBC.
	payload := []byte{0x02, 0x00, 0xAA, 0xBB}
	wrapped := EncodeDLEFrame(payload)
	want := []byte{DLE, STX, 0x02, 0x00, 0xAA, 0xBB}
	if !bytes.Equal(wrapped, want) {
		t.Fatalf("wrapped=% x, want=% x", wrapped, want)
	}
	dec := NewDLEStreamDecoder(bytes.NewReader(wrapped), 0)
	got, err := dec.ReadFrame()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("decoded=% x, want=% x", got, payload)
	}
}

func TestDLEFrame_ByteStuffing_SingleDLEInBody(t *testing.T) {
	// Body contains one 0xFE byte — must be stuffed to 0xFE 0xFE.
	// Build a v5-ish body where PBC = 4 (after the 2 PBC bytes) so the
	// decoder reads the correct total length.
	body := []byte{0x04, 0x00, 0x00, 0x00, DLE, 0xAA}
	//             ^^^^^^^^^^^  pbc=4 LE     ^^^^^^^^^^ 4 body bytes (VER/FLAGS zero, then DLE,0xAA)
	wrapped := EncodeDLEFrame(body)
	// Wrapped length should be 2 (DLE/STX) + 6 (body) + 1 (stuff for the DLE)
	if len(wrapped) != 2+6+1 {
		t.Errorf("stuffed wrapper len = %d, want %d", len(wrapped), 2+6+1)
	}
	// Decoder should recover the original body exactly.
	got, err := NewDLEStreamDecoder(bytes.NewReader(wrapped), 0).ReadFrame()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Errorf("decoded=% x, want=% x", got, body)
	}
}

func TestDLEFrame_ByteStuffing_MultipleFrames(t *testing.T) {
	// Two successive frames in one stream.
	a := []byte{0x02, 0x00, 0xAA, 0xBB}     // PBC=2, two data bytes
	b := []byte{0x03, 0x00, DLE, 0xCC, DLE} // PBC=3, includes 2 DLEs that get stuffed
	stream := append(EncodeDLEFrame(a), EncodeDLEFrame(b)...)

	dec := NewDLEStreamDecoder(bytes.NewReader(stream), 0)

	got1, err := dec.ReadFrame()
	if err != nil {
		t.Fatalf("frame 1 read: %v", err)
	}
	if !bytes.Equal(got1, a) {
		t.Errorf("frame1=% x, want=% x", got1, a)
	}
	got2, err := dec.ReadFrame()
	if err != nil {
		t.Fatalf("frame 2 read: %v", err)
	}
	if !bytes.Equal(got2, b) {
		t.Errorf("frame2=% x, want=% x", got2, b)
	}
	// Third read at EOF.
	if _, err := dec.ReadFrame(); err != io.EOF {
		t.Errorf("third read err = %v, want EOF", err)
	}
}

func TestDLEFrame_MalformedEscape(t *testing.T) {
	// Construct a stream where a DLE inside the body is NOT followed by
	// another DLE — this is a framing error. The sequence DLE 0xAA in
	// the body (without stuffing) should trigger ErrDLEMalformedEscape.
	stream := []byte{
		DLE, STX, // start
		0x02, 0x00, // PBC=2
		DLE, 0xAA, // malformed — DLE not stuffed
	}
	_, err := NewDLEStreamDecoder(bytes.NewReader(stream), 0).ReadFrame()
	if !errors.Is(err, ErrDLEMalformedEscape) {
		t.Errorf("want ErrDLEMalformedEscape, got %v", err)
	}
}

func TestDLEFrame_MissingStart(t *testing.T) {
	// Stream doesn't start with DLE/STX.
	stream := []byte{0x00, 0x01, 0x02}
	_, err := NewDLEStreamDecoder(bytes.NewReader(stream), 0).ReadFrame()
	if !errors.Is(err, ErrDLEMissingStart) {
		t.Errorf("want ErrDLEMissingStart, got %v", err)
	}
}

func TestDLEFrame_TruncatedMidFrame(t *testing.T) {
	// Stream ends mid-body.
	stream := []byte{DLE, STX, 0x05, 0x00, 0x01} // claims PBC=5, only 1 body byte
	_, err := NewDLEStreamDecoder(bytes.NewReader(stream), 0).ReadFrame()
	if err == nil || err == io.EOF {
		t.Errorf("want unexpected-EOF, got %v", err)
	}
}

// errReader returns data once, then a fixed non-EOF error — used to drive
// the ensureBytes reader-error arm that io.EOF can't reach.
type errReader struct {
	data []byte
	err  error
	done bool
}

func (r *errReader) Read(p []byte) (int, error) {
	if !r.done && len(r.data) > 0 {
		n := copy(p, r.data)
		r.data = r.data[n:]
		if len(r.data) == 0 {
			r.done = true
		}
		return n, nil
	}
	return 0, r.err
}

// dataEOFReader returns its whole buffer plus io.EOF in a SINGLE Read call
// (legal per io.Reader), then io.EOF forever after — exercising the
// ensureBytes arm where k>0 and err==io.EOF arrive together with bytes
// still buffered beyond what was requested.
type dataEOFReader struct {
	data []byte
	done bool
}

func (r *dataEOFReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, io.EOF
	}
	n := copy(p, r.data)
	r.done = true
	return n, io.EOF
}

func TestDLEFrame_DataPlusEOFSameRead_UnexpectedEOF(t *testing.T) {
	// A Read that returns body bytes together with io.EOF (legal per the
	// io.Reader contract) and leaves bytes still buffered exercises the
	// ensureBytes "EOF with leftover" arm, which yields ErrUnexpectedEOF
	// rather than a clean io.EOF.
	r := &dataEOFReader{data: []byte{DLE, STX, 0x05}}
	_, err := NewDLEStreamDecoder(r, 0).ReadFrame()
	if err != io.ErrUnexpectedEOF {
		t.Errorf("want ErrUnexpectedEOF, got %v", err)
	}
}

func TestDLEFrame_ReaderError_Propagated(t *testing.T) {
	// A non-EOF reader error mid-frame must propagate verbatim (not be
	// remapped to ErrUnexpectedEOF).
	sentinel := errors.New("network down")
	r := &errReader{data: []byte{DLE, STX}, err: sentinel}
	_, err := NewDLEStreamDecoder(r, 0).ReadFrame()
	if !errors.Is(err, sentinel) {
		t.Errorf("want sentinel reader error, got %v", err)
	}
}

func TestDLEFrame_EOFAfterStartByte_UnexpectedEOF(t *testing.T) {
	// Stream is exactly one DLE then EOF. The first byte (DLE) succeeds,
	// the strict read of STX hits EOF → ErrUnexpectedEOF.
	r := &errReader{data: []byte{DLE}, err: io.EOF}
	_, err := NewDLEStreamDecoder(r, 0).ReadFrame()
	if err != io.ErrUnexpectedEOF {
		t.Errorf("want ErrUnexpectedEOF, got %v", err)
	}
}

func TestDLEFrame_DLENotFollowedBySTX(t *testing.T) {
	// DLE followed by a non-STX byte at start is a missing-start error.
	stream := []byte{DLE, 0x03, 0x00, 0x00}
	_, err := NewDLEStreamDecoder(bytes.NewReader(stream), 0).ReadFrame()
	if !errors.Is(err, ErrDLEMissingStart) {
		t.Errorf("want ErrDLEMissingStart, got %v", err)
	}
}

func TestDLEFrame_TruncatedBeforePBC(t *testing.T) {
	// DLE/STX then immediate EOF — can't read the 2-byte PBC.
	stream := []byte{DLE, STX}
	_, err := NewDLEStreamDecoder(bytes.NewReader(stream), 0).ReadFrame()
	if err != io.ErrUnexpectedEOF {
		t.Errorf("want ErrUnexpectedEOF reading PBC, got %v", err)
	}
}

func TestDLEFrame_PBCExceedsMax(t *testing.T) {
	// PBC larger than maxPktSize-2 must reject with ErrV50PacketTooLarge.
	// maxPktSize=8 → max body PBC = 6; PBC=10 overflows.
	stream := []byte{DLE, STX, 0x0A, 0x00} // PBC=10 LE
	_, err := NewDLEStreamDecoder(bytes.NewReader(stream), 8).ReadFrame()
	if !errors.Is(err, ErrV50PacketTooLarge) {
		t.Errorf("want ErrV50PacketTooLarge, got %v", err)
	}
}

func TestDLEFrame_TruncatedInsideEscape(t *testing.T) {
	// A DLE in the body followed by EOF (no second escape byte) is a
	// mid-frame truncation.
	stream := []byte{DLE, STX, 0x02, 0x00, DLE} // PBC=2, then lone DLE, EOF
	_, err := NewDLEStreamDecoder(bytes.NewReader(stream), 0).ReadFrame()
	if err != io.ErrUnexpectedEOF {
		t.Errorf("want ErrUnexpectedEOF inside escape, got %v", err)
	}
}

func TestDLEFrame_RealV50Packet_WithStuffing(t *testing.T) {
	// Build a real v5.0 packet that happens to contain 0xFE in the body
	// (e.g. SCREEN = 0xFEFE), then wrap + unwrap via DLE stream.
	p := V50Packet{
		Screen: 0xFEFE, // two stuffings required
		DMSGs: []DMSG{
			{Index: 0xFE01, TextTally: TallyRed, Text: "X"},
		},
	}
	packet, err := p.Encode()
	if err != nil {
		t.Fatalf("encode v5: %v", err)
	}
	// Count 0xFE in the packet.
	dleCount := 0
	for _, b := range packet {
		if b == DLE {
			dleCount++
		}
	}
	if dleCount == 0 {
		t.Skip("packet happened to contain no DLE bytes — stuffing not exercised")
	}

	wrapped := EncodeDLEFrame(packet)
	if len(wrapped) != 2+len(packet)+dleCount {
		t.Errorf("wrapped len = %d, want %d (2 DLE/STX + %d body + %d stuffing)",
			len(wrapped), 2+len(packet)+dleCount, len(packet), dleCount)
	}
	got, err := NewDLEStreamDecoder(bytes.NewReader(wrapped), 0).ReadFrame()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, packet) {
		t.Errorf("unwrapped != original\n got:  % x\n want: % x", got, packet)
	}
	// End-to-end: decode the unwrapped packet back to the semantic struct.
	back, err := DecodeV50(got)
	if err != nil {
		t.Fatalf("DecodeV50: %v", err)
	}
	if back.Screen != 0xFEFE || back.DMSGs[0].Index != 0xFE01 {
		t.Errorf("semantic round-trip failed: %+v", back)
	}
}
