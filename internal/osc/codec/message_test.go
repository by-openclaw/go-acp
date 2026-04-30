package codec

import (
	"bytes"
	"errors"
	"testing"
)

func TestEncodeString_Alignment(t *testing.T) {
	cases := []struct {
		in   string
		want []byte
	}{
		{"", []byte{0x00, 0x00, 0x00, 0x00}},
		{"abc", []byte{'a', 'b', 'c', 0x00}},
		{"data", []byte{'d', 'a', 't', 'a', 0x00, 0x00, 0x00, 0x00}},
		{"#bundle", []byte{'#', 'b', 'u', 'n', 'd', 'l', 'e', 0x00}},
		{"/foo", []byte{'/', 'f', 'o', 'o', 0x00, 0x00, 0x00, 0x00}},
	}
	for _, c := range cases {
		got := encodeString(nil, c.in)
		if !bytes.Equal(got, c.want) {
			t.Errorf("encodeString(%q) = % x, want % x", c.in, got, c.want)
		}
	}
}

func TestDecodeString_AlignmentAndConsumed(t *testing.T) {
	// "abc" encoded = 'a','b','c',0x00 → 4 bytes consumed
	s, n, err := decodeString([]byte{'a', 'b', 'c', 0x00}, 0)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if s != "abc" || n != 4 {
		t.Errorf("got %q n=%d, want \"abc\" n=4", s, n)
	}
	// "data" encoded = 4 bytes text + 4 bytes NUL pad = 8 total
	s, n, err = decodeString([]byte{'d', 'a', 't', 'a', 0x00, 0x00, 0x00, 0x00}, 0)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if s != "data" || n != 8 {
		t.Errorf("got %q n=%d, want \"data\" n=8", s, n)
	}
}

func TestDecodeString_NoTerminator(t *testing.T) {
	_, _, err := decodeString([]byte{'a', 'b', 'c'}, 0)
	if !errors.Is(err, ErrStringNotTerminated) {
		t.Errorf("want ErrStringNotTerminated, got %v", err)
	}
}

func TestEncodeBlob_PadAndSize(t *testing.T) {
	got := encodeBlob(nil, []byte{0x11, 0x22, 0x33})
	// int32 BE size=3 → 0x00 0x00 0x00 0x03, then 3 bytes, then 1 NUL pad.
	want := []byte{0x00, 0x00, 0x00, 0x03, 0x11, 0x22, 0x33, 0x00}
	if !bytes.Equal(got, want) {
		t.Errorf("got % x, want % x", got, want)
	}
}

func TestDecodeBlob_RoundTrip(t *testing.T) {
	in := []byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE}
	wire := encodeBlob(nil, in)
	got, n, err := decodeBlob(wire, 0)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !bytes.Equal(got, in) {
		t.Errorf("round-trip = % x, want % x", got, in)
	}
	// 4 size + 5 data + 3 pad = 12
	if n != 12 {
		t.Errorf("consumed=%d, want 12", n)
	}
}

func TestMessage_Encode_SpecExample(t *testing.T) {
	// From OSC 1.0 examples: /oscillator/4/frequency ,f 440.0
	m := Message{
		Address: "/oscillator/4/frequency",
		Args:    []Arg{Float32(440.0)},
	}
	wire, err := m.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	// Decode back and verify structural round-trip.
	got, err := DecodeMessage(wire)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Address != m.Address {
		t.Errorf("Address=%q, want %q", got.Address, m.Address)
	}
	if len(got.Args) != 1 || got.Args[0].Tag != 'f' || got.Args[0].Float32 != 440.0 {
		t.Errorf("Args=%+v", got.Args)
	}
}

func TestMessage_RoundTrip_AllTags(t *testing.T) {
	m := Message{
		Address: "/mixer/ch/1/set",
		Args: []Arg{
			Int32(42),
			Float32(3.14),
			String("hello"),
			Blob([]byte{0x01, 0x02, 0x03}),
			Int64(-9223372036854775807),
			Float64(2.718281828),
			Symbol("SYM"),
			Char(int32('Z')),
			RGBA([]byte{0xFF, 0xA0, 0x00, 0xFF}),
			MIDI([]byte{0x90, 0x3C, 0x64, 0x00}),
			Timetag(0x0000000100000000),
			True(),
			False(),
			Nil(),
			Infinitum(),
		},
	}
	wire, err := m.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := DecodeMessage(wire)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Address != m.Address {
		t.Errorf("Address=%q", got.Address)
	}
	if len(got.Args) != len(m.Args) {
		t.Fatalf("args len=%d, want %d", len(got.Args), len(m.Args))
	}
	for i := range m.Args {
		if got.Args[i].Tag != m.Args[i].Tag {
			t.Errorf("arg[%d] tag=%c, want %c", i, got.Args[i].Tag, m.Args[i].Tag)
		}
	}
	// Spot-check typed fields
	if got.Args[0].Int32 != 42 {
		t.Errorf("int32: %d", got.Args[0].Int32)
	}
	if got.Args[4].Int64 != -9223372036854775807 {
		t.Errorf("int64: %d", got.Args[4].Int64)
	}
	if got.Args[5].Float64 != 2.718281828 {
		t.Errorf("float64: %v", got.Args[5].Float64)
	}
	if got.Args[2].String != "hello" {
		t.Errorf("string: %q", got.Args[2].String)
	}
	if !bytes.Equal(got.Args[3].Blob, []byte{0x01, 0x02, 0x03}) {
		t.Errorf("blob: % x", got.Args[3].Blob)
	}
	if got.Args[10].Uint64 != 0x0000000100000000 {
		t.Errorf("timetag: 0x%x", got.Args[10].Uint64)
	}
}

func TestMessage_EmptyAddressRejected(t *testing.T) {
	_, err := Message{Address: ""}.Encode()
	if err == nil {
		t.Errorf("empty address should error")
	}
}

func TestMessage_BadTypeTag_MissingComma(t *testing.T) {
	// Build raw bytes: address "/x" + tag string without leading comma.
	wire := encodeString(nil, "/x")
	wire = encodeString(wire, "i") // no leading comma
	wire = encodeInt32(wire, 7)
	_, err := DecodeMessage(wire)
	if !errors.Is(err, ErrCommaMissing) {
		t.Errorf("want ErrCommaMissing, got %v", err)
	}
}

func TestMessage_UnknownTag_FiresNote(t *testing.T) {
	// Build raw bytes: address "/x" + tag ",Z" (Z not a recognised tag)
	wire := encodeString(nil, "/x")
	wire = encodeString(wire, ",Z")
	m, err := DecodeMessage(wire)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	found := false
	for _, n := range m.Notes {
		if n.Kind == "osc_type_tag_unknown" {
			found = true
		}
	}
	if !found {
		t.Errorf("want osc_type_tag_unknown note, got %+v", m.Notes)
	}
}

func TestMessage_TruncatedBody(t *testing.T) {
	// Tag claims ,i but no int bytes follow.
	wire := encodeString(nil, "/x")
	wire = encodeString(wire, ",i")
	// no 4-byte int32 follows
	_, err := DecodeMessage(wire)
	if !errors.Is(err, ErrTruncated) {
		t.Errorf("want ErrTruncated, got %v", err)
	}
}

func TestMessage_StringArgPaddingAlignment(t *testing.T) {
	// Arg string "abcd" (4 bytes) needs 4 NUL bytes to align → 8 total.
	m := Message{Address: "/p", Args: []Arg{String("abcd")}}
	wire, _ := m.Encode()
	got, err := DecodeMessage(wire)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Args[0].String != "abcd" {
		t.Errorf("string pad round-trip failed: %q", got.Args[0].String)
	}
}
