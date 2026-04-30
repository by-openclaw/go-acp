package codec

import (
	"bytes"
	"errors"
	"math"
	"testing"
)

// Per-tag round-trip + edge case sweep. Each tag tested independently
// with extreme values where applicable so a regression in encode/decode
// of one tag doesn't hide behind a successful round-trip of another.

func TestPerTag_Int32_RoundTrip(t *testing.T) {
	for _, v := range []int32{0, 1, -1, math.MaxInt32, math.MinInt32, 42, -42} {
		m := Message{Address: "/i", Args: []Arg{Int32(v)}}
		gotMsg := encodeDecode(t, m)
		if gotMsg.Args[0].Int32 != v {
			t.Errorf("int32 round-trip: got %d, want %d", gotMsg.Args[0].Int32, v)
		}
	}
}

func TestPerTag_Float32_RoundTrip(t *testing.T) {
	values := []float32{0, 1, -1, 3.14, math.MaxFloat32, math.SmallestNonzeroFloat32}
	for _, v := range values {
		m := Message{Address: "/f", Args: []Arg{Float32(v)}}
		got := encodeDecode(t, m)
		if got.Args[0].Float32 != v {
			t.Errorf("float32 round-trip: got %v, want %v", got.Args[0].Float32, v)
		}
	}
}

func TestPerTag_Int64_RoundTrip(t *testing.T) {
	for _, v := range []int64{0, 1, -1, math.MaxInt64, math.MinInt64} {
		m := Message{Address: "/h", Args: []Arg{Int64(v)}}
		got := encodeDecode(t, m)
		if got.Args[0].Int64 != v {
			t.Errorf("int64 round-trip: got %d, want %d", got.Args[0].Int64, v)
		}
	}
}

func TestPerTag_Float64_RoundTrip(t *testing.T) {
	values := []float64{0, 1, -1, math.Pi, math.MaxFloat64, math.SmallestNonzeroFloat64}
	for _, v := range values {
		m := Message{Address: "/d", Args: []Arg{Float64(v)}}
		got := encodeDecode(t, m)
		if got.Args[0].Float64 != v {
			t.Errorf("float64 round-trip: got %v, want %v", got.Args[0].Float64, v)
		}
	}
}

func TestPerTag_String_VariousLengths(t *testing.T) {
	values := []string{"", "a", "hi", "hello", "exactly12bytes!", "a string of length unknown but more than four bytes"}
	for _, v := range values {
		m := Message{Address: "/s", Args: []Arg{String(v)}}
		got := encodeDecode(t, m)
		if got.Args[0].String != v {
			t.Errorf("string: got %q, want %q", got.Args[0].String, v)
		}
	}
}

func TestPerTag_Symbol_RoundTrip(t *testing.T) {
	m := Message{Address: "/S", Args: []Arg{Symbol("MUTE")}}
	got := encodeDecode(t, m)
	if got.Args[0].Tag != TagSymbol || got.Args[0].String != "MUTE" {
		t.Errorf("symbol: got tag=%c value=%q", got.Args[0].Tag, got.Args[0].String)
	}
}

func TestPerTag_Blob_VariousSizes(t *testing.T) {
	for _, size := range []int{0, 1, 4, 7, 8, 1024} {
		blob := make([]byte, size)
		for i := range blob {
			blob[i] = byte(i)
		}
		m := Message{Address: "/b", Args: []Arg{Blob(blob)}}
		got := encodeDecode(t, m)
		if !bytes.Equal(got.Args[0].Blob, blob) {
			t.Errorf("blob size %d: got %d bytes, want %d", size, len(got.Args[0].Blob), size)
		}
	}
}

func TestPerTag_Char_RoundTrip(t *testing.T) {
	for _, c := range []rune{'A', 'z', '0', ' ', '!', 0x7f} {
		m := Message{Address: "/c", Args: []Arg{Char(int32(c))}}
		got := encodeDecode(t, m)
		if rune(got.Args[0].Int32) != c {
			t.Errorf("char: got %q, want %q", rune(got.Args[0].Int32), c)
		}
	}
}

func TestPerTag_RGBA_StrictFourBytes(t *testing.T) {
	good := []byte{0xFF, 0xA0, 0x00, 0xFF}
	m := Message{Address: "/r", Args: []Arg{RGBA(good)}}
	got := encodeDecode(t, m)
	if !bytes.Equal(got.Args[0].Blob, good) {
		t.Errorf("rgba: %x", got.Args[0].Blob)
	}
	// 3-byte payload must reject at encode time.
	bad := Message{Address: "/r", Args: []Arg{{Tag: TagRGBA32, Blob: []byte{1, 2, 3}}}}
	if _, err := bad.Encode(); err == nil {
		t.Errorf("3-byte RGBA should error")
	}
	// 5-byte too.
	bad2 := Message{Address: "/r", Args: []Arg{{Tag: TagRGBA32, Blob: []byte{1, 2, 3, 4, 5}}}}
	if _, err := bad2.Encode(); err == nil {
		t.Errorf("5-byte RGBA should error")
	}
}

func TestPerTag_MIDI_StrictFourBytes(t *testing.T) {
	good := []byte{0x90, 0x3C, 0x64, 0x00}
	m := Message{Address: "/m", Args: []Arg{MIDI(good)}}
	got := encodeDecode(t, m)
	if !bytes.Equal(got.Args[0].Blob, good) {
		t.Errorf("midi: %x", got.Args[0].Blob)
	}
	bad := Message{Address: "/m", Args: []Arg{{Tag: TagMIDI, Blob: []byte{1, 2, 3}}}}
	if _, err := bad.Encode(); err == nil {
		t.Errorf("3-byte MIDI should error")
	}
}

func TestPerTag_Timetag_RoundTrip(t *testing.T) {
	values := []uint64{0, 1, 0xFFFFFFFFFFFFFFFF, 0x0000000100000000}
	for _, v := range values {
		m := Message{Address: "/t", Args: []Arg{Timetag(v)}}
		got := encodeDecode(t, m)
		if got.Args[0].Uint64 != v {
			t.Errorf("timetag: 0x%x", got.Args[0].Uint64)
		}
	}
}

func TestPerTag_PayloadlessNoBytes(t *testing.T) {
	// T/F/N/I encode + decode without consuming any payload bytes.
	m := Message{Address: "/x", Args: []Arg{True(), False(), Nil(), Infinitum()}}
	wire, err := m.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	// Wire = address(4) + ",TFNI\0\0\0" (8) + zero arg payload = 12 bytes total.
	// "/x\0\0" is 4 bytes (data 2 + NUL 1 + pad 1).
	wantLen := 4 + 8
	if len(wire) != wantLen {
		t.Errorf("wire len=%d, want %d (%x)", len(wire), wantLen, wire)
	}
	got, err := DecodeMessage(wire)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Args) != 4 {
		t.Fatalf("args=%d, want 4", len(got.Args))
	}
	wantTags := []byte{TagTrue, TagFalse, TagNil, TagInfinitum}
	for i, want := range wantTags {
		if got.Args[i].Tag != want {
			t.Errorf("arg[%d]: %c, want %c", i, got.Args[i].Tag, want)
		}
	}
}

func TestArrayMarkers_RoundTrip(t *testing.T) {
	// /array ,i[ii] 1 [10 20]  — wraps two ints in an array marker pair.
	m := Message{Address: "/array", Args: []Arg{
		Int32(1),
		ArrayBegin(),
		Int32(10),
		Int32(20),
		ArrayEnd(),
	}}
	got := encodeDecode(t, m)
	wantTags := []byte{TagInt32, TagArrayBegin, TagInt32, TagInt32, TagArrayEnd}
	if len(got.Args) != len(wantTags) {
		t.Fatalf("args=%d, want %d", len(got.Args), len(wantTags))
	}
	for i, want := range wantTags {
		if got.Args[i].Tag != want {
			t.Errorf("arg[%d] tag=%c, want %c", i, got.Args[i].Tag, want)
		}
	}
	if got.Args[0].Int32 != 1 || got.Args[2].Int32 != 10 || got.Args[3].Int32 != 20 {
		t.Errorf("array values: %d / %d / %d", got.Args[0].Int32, got.Args[2].Int32, got.Args[3].Int32)
	}
}

func TestArrayMarkers_NestedTwoDeep(t *testing.T) {
	// ,[i[ii]] — outer array around (int + inner array of 2 ints)
	m := Message{Address: "/nested", Args: []Arg{
		ArrayBegin(),
		Int32(1),
		ArrayBegin(),
		Int32(10),
		Int32(20),
		ArrayEnd(),
		ArrayEnd(),
	}}
	got := encodeDecode(t, m)
	wantTags := []byte{TagArrayBegin, TagInt32, TagArrayBegin, TagInt32, TagInt32, TagArrayEnd, TagArrayEnd}
	if len(got.Args) != len(wantTags) {
		t.Fatalf("args=%d, want %d", len(got.Args), len(wantTags))
	}
	for i, want := range wantTags {
		if got.Args[i].Tag != want {
			t.Errorf("arg[%d] tag=%c, want %c", i, got.Args[i].Tag, want)
		}
	}
}

// Compliance triggers — each must surface as a ComplianceNote or error.

func TestCompliance_TruncatedMidArg(t *testing.T) {
	// /x ,i 7 — truncate the int32 to 2 bytes.
	wire := encodeString(nil, "/x")
	wire = encodeString(wire, ",i")
	wire = append(wire, 0x00, 0x07) // only 2 of 4 bytes
	if _, err := DecodeMessage(wire); !errors.Is(err, ErrTruncated) {
		t.Errorf("want ErrTruncated, got %v", err)
	}
}

func TestCompliance_BlobTruncated(t *testing.T) {
	// /b ,b <size=10> <only-3-bytes>
	wire := encodeString(nil, "/b")
	wire = encodeString(wire, ",b")
	wire = encodeInt32(wire, 10)
	wire = append(wire, 1, 2, 3)
	if _, err := DecodeMessage(wire); err == nil {
		t.Errorf("expected truncation error")
	}
}

// encodeDecode is a helper: encode + decode, fail on any error.
func encodeDecode(t *testing.T, m Message) Message {
	t.Helper()
	wire, err := m.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := DecodeMessage(wire)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return got
}
