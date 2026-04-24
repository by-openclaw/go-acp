package codec

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

// -----------------------------------------------------------------------------
// Length-prefix framing
// -----------------------------------------------------------------------------

func TestLenPrefix_RoundTrip(t *testing.T) {
	packets := [][]byte{
		[]byte("AA"),
		[]byte("hello world"),
		{},
	}
	var stream []byte
	for _, p := range packets {
		stream = append(stream, EncodeLenPrefix(p)...)
	}
	rd := NewLenPrefixReader(bytes.NewReader(stream), 0)
	for i, want := range packets {
		got, err := rd.ReadPacket()
		if err != nil {
			t.Fatalf("packet %d: %v", i, err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("packet %d: got %q, want %q", i, got, want)
		}
	}
	// Stream should be cleanly at EOF.
	if _, err := rd.ReadPacket(); err != io.EOF {
		t.Errorf("expected EOF, got %v", err)
	}
}

func TestLenPrefix_TruncatedMidPacket(t *testing.T) {
	// 4-byte header claims size=10, only 3 body bytes follow.
	stream := []byte{0x00, 0x00, 0x00, 0x0A, 0x01, 0x02, 0x03}
	rd := NewLenPrefixReader(bytes.NewReader(stream), 0)
	_, err := rd.ReadPacket()
	if err != io.ErrUnexpectedEOF {
		t.Errorf("expected io.ErrUnexpectedEOF, got %v", err)
	}
}

func TestLenPrefix_TooLargeRejected(t *testing.T) {
	stream := EncodeLenPrefix(make([]byte, 100))
	rd := NewLenPrefixReader(bytes.NewReader(stream), 50)
	_, err := rd.ReadPacket()
	if !errors.Is(err, ErrLenPrefixTooLarge) {
		t.Errorf("expected ErrLenPrefixTooLarge, got %v", err)
	}
}

// -----------------------------------------------------------------------------
// SLIP framing
// -----------------------------------------------------------------------------

func TestSLIP_RoundTrip_Plain(t *testing.T) {
	in := []byte("hello")
	wire := EncodeSLIP(in)
	// Wire = END + 'h','e','l','l','o' + END = 7 bytes
	if wire[0] != SLIPEnd || wire[len(wire)-1] != SLIPEnd {
		t.Errorf("missing END delimiters: % x", wire)
	}
	got, err := NewSLIPReader(bytes.NewReader(wire), 0).ReadPacket()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, in) {
		t.Errorf("got %q, want %q", got, in)
	}
}

func TestSLIP_EscapeEND(t *testing.T) {
	// Body contains a literal 0xC0 byte — must be escape-stuffed.
	in := []byte{0x01, SLIPEnd, 0x02}
	wire := EncodeSLIP(in)
	// Expect END + 01 ESC ESC_END 02 END = 7 bytes.
	want := []byte{SLIPEnd, 0x01, SLIPEsc, SLIPEscEnd, 0x02, SLIPEnd}
	if !bytes.Equal(wire, want) {
		t.Errorf("wire=% x, want=% x", wire, want)
	}
	got, err := NewSLIPReader(bytes.NewReader(wire), 0).ReadPacket()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, in) {
		t.Errorf("got % x, want % x", got, in)
	}
}

func TestSLIP_EscapeESC(t *testing.T) {
	in := []byte{0x10, SLIPEsc, 0x20}
	wire := EncodeSLIP(in)
	want := []byte{SLIPEnd, 0x10, SLIPEsc, SLIPEscEsc, 0x20, SLIPEnd}
	if !bytes.Equal(wire, want) {
		t.Errorf("wire=% x, want=% x", wire, want)
	}
	got, _ := NewSLIPReader(bytes.NewReader(wire), 0).ReadPacket()
	if !bytes.Equal(got, in) {
		t.Errorf("got % x, want % x", got, in)
	}
}

func TestSLIP_DoubleEND_BetweenPackets(t *testing.T) {
	// OSC 1.1 mandates END both BEFORE and AFTER each packet — so
	// back-to-back packets look like END pkt1 END END pkt2 END.
	p1 := []byte("AA")
	p2 := []byte("BB")
	stream := append(EncodeSLIP(p1), EncodeSLIP(p2)...)
	rd := NewSLIPReader(bytes.NewReader(stream), 0)

	g1, err := rd.ReadPacket()
	if err != nil {
		t.Fatalf("read 1: %v", err)
	}
	if !bytes.Equal(g1, p1) {
		t.Errorf("p1 got %q", g1)
	}
	g2, err := rd.ReadPacket()
	if err != nil {
		t.Fatalf("read 2: %v", err)
	}
	if !bytes.Equal(g2, p2) {
		t.Errorf("p2 got %q", g2)
	}
	if _, err := rd.ReadPacket(); err != io.EOF {
		t.Errorf("expected EOF, got %v", err)
	}
}

func TestSLIP_MalformedEscape(t *testing.T) {
	// ESC followed by neither ESC_END nor ESC_ESC.
	stream := []byte{SLIPEnd, 0x01, SLIPEsc, 0x99, SLIPEnd}
	_, err := NewSLIPReader(bytes.NewReader(stream), 0).ReadPacket()
	if !errors.Is(err, ErrSLIPBadEscape) {
		t.Errorf("expected ErrSLIPBadEscape, got %v", err)
	}
}

func TestSLIP_TruncatedMidPacket(t *testing.T) {
	stream := []byte{SLIPEnd, 0x01, 0x02} // no trailing END
	_, err := NewSLIPReader(bytes.NewReader(stream), 0).ReadPacket()
	if err == nil || err == io.EOF {
		t.Errorf("expected unexpected-EOF, got %v", err)
	}
}

func TestSLIP_RealOSCMessageRoundTrip(t *testing.T) {
	m := Message{
		Address: "/mixer/ch/1/fader",
		Args:    []Arg{Float32(0.5), True(), String("PGM")},
	}
	wire, err := m.Encode()
	if err != nil {
		t.Fatalf("msg encode: %v", err)
	}
	wrapped := EncodeSLIP(wire)
	unwrapped, err := NewSLIPReader(bytes.NewReader(wrapped), 0).ReadPacket()
	if err != nil {
		t.Fatalf("slip read: %v", err)
	}
	if !bytes.Equal(unwrapped, wire) {
		t.Errorf("unwrapped mismatch")
	}
	back, err := DecodeMessage(unwrapped)
	if err != nil {
		t.Fatalf("msg decode: %v", err)
	}
	if back.Address != m.Address || len(back.Args) != 3 {
		t.Errorf("round-trip: %+v", back)
	}
	if back.Args[1].Tag != TagTrue {
		t.Errorf("boolean T lost in round-trip")
	}
}
