package codec

import (
	"testing"
)

// EncodeProperty: nil property -> error.
func TestEncodeProperty_Nil(t *testing.T) {
	if _, err := EncodeProperty(nil); err == nil {
		t.Fatal("expected error encoding nil property")
	}
}

// EncodeProperties: empty slice yields empty output, no error.
func TestEncodeProperties_Empty(t *testing.T) {
	out, err := EncodeProperties(nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("expected empty output, got %d bytes", len(out))
	}
}

// DecodeProperties: property declaring plen < 4 is rejected.
func TestDecodeProperties_PlenTooSmall(t *testing.T) {
	data := []byte{0x08, 0x06, 0x00, 0x03} // plen = 3
	if _, err := DecodeProperties(data); err == nil {
		t.Fatal("expected error for plen < 4")
	}
}

// DecodeProperties: property whose plen runs past the buffer end is rejected.
func TestDecodeProperties_PlenExceedsBuffer(t *testing.T) {
	data := []byte{0x08, 0x06, 0x00, 0x10} // plen = 16, buffer only 4 bytes
	if _, err := DecodeProperties(data); err == nil {
		t.Fatal("expected error for plen exceeding buffer")
	}
}

// DecodeProperties: trailing bytes shorter than a 4-byte header are ignored
// (loop guard offset+4 <= len fails), returning the parsed properties.
func TestDecodeProperties_TrailingShortIgnored(t *testing.T) {
	// One valid plen=4 property followed by 2 stray bytes.
	data := []byte{0x01, 0x00, 0x00, 0x04, 0xFF, 0xFF}
	props, err := DecodeProperties(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(props) != 1 {
		t.Fatalf("props: got %d, want 1", len(props))
	}
}

// PropertyString: nil data -> empty string.
func TestPropertyString_Nil(t *testing.T) {
	if s := PropertyString(&Property{}); s != "" {
		t.Fatalf("expected empty string, got %q", s)
	}
}

// PropertyString: data with no NUL terminator returns the full string.
func TestPropertyString_NoTerminator(t *testing.T) {
	if s := PropertyString(&Property{Data: []byte("abc")}); s != "abc" {
		t.Fatalf("got %q, want abc", s)
	}
}

// PropertyChildren: data length not a multiple of 4 -> error.
func TestPropertyChildren_BadLength(t *testing.T) {
	if _, err := PropertyChildren(&Property{Data: []byte{1, 2, 3}}); err == nil {
		t.Fatal("expected error for non-multiple-of-4 length")
	}
}

// PropertyEventMessages: nil data -> two empty strings.
func TestPropertyEventMessages_Nil(t *testing.T) {
	on, off := PropertyEventMessages(&Property{})
	if on != "" || off != "" {
		t.Fatalf("expected empty messages, got %q / %q", on, off)
	}
}

// PropertyEventMessages: only the on-message present (single string).
func TestPropertyEventMessages_OnlyOn(t *testing.T) {
	on, off := PropertyEventMessages(&Property{Data: []byte("ON")})
	if on != "ON" || off != "" {
		t.Fatalf("got %q / %q, want ON / empty", on, off)
	}
}

// PropertyOptions / PropertyOptionsMap: data shorter than 4 bytes -> nil.
func TestPropertyOptions_TooShort(t *testing.T) {
	p := &Property{Data: []byte{0x00, 0x01}}
	if PropertyOptionsMap(p) != nil {
		t.Fatal("expected nil map for short data")
	}
	if PropertyOptions(p) != nil {
		t.Fatal("expected nil slice for short data")
	}
}

// PropertyOptions / PropertyOptionsMap: variable-length records with index +
// NUL-terminated name + 4-byte alignment padding.
func TestPropertyOptions_VariableLength(t *testing.T) {
	// record 0: idx=0, name="a\0", pad to 4-boundary
	// record 1: idx=1, name="bb\0", pad
	data := []byte{
		0x00, 0x00, 0x00, 0x00, 'a', 0x00, 0x00, 0x00, // idx0 + "a\0" + 2 pad
		0x00, 0x00, 0x00, 0x01, 'b', 'b', 0x00, 0x00, // idx1 + "bb\0" + 1 pad
	}
	p := &Property{Data: data}
	m := PropertyOptionsMap(p)
	if m == nil {
		t.Fatal("expected non-nil map")
	}
	if m[0] != "a" || m[1] != "bb" {
		t.Fatalf("map mismatch: %#v", m)
	}
	opts := PropertyOptions(p)
	if len(opts) != 2 || opts[0] != "a" || opts[1] != "bb" {
		t.Fatalf("options mismatch: %#v", opts)
	}
}

// PropertyOptions: a record header present but its name runs to the end of
// the buffer with no NUL (exercises the end-of-buffer break paths).
func TestPropertyOptions_NameRunsToEnd(t *testing.T) {
	data := []byte{0x00, 0x00, 0x00, 0x05, 'x', 'y', 'z'}
	p := &Property{Data: data}
	m := PropertyOptionsMap(p)
	if m[5] != "xyz" {
		t.Fatalf("map: got %#v", m)
	}
	opts := PropertyOptions(p)
	if len(opts) != 1 || opts[0] != "xyz" {
		t.Fatalf("options: got %#v", opts)
	}
}

// PropertyOptions: trailing partial record (< 4 bytes) after a full one is
// ignored via the pos+4 > len break.
func TestPropertyOptions_TrailingPartial(t *testing.T) {
	data := []byte{
		0x00, 0x00, 0x00, 0x00, 'a', 0x00, 0x00, 0x00, // full record
		0xFF, 0xFF, // 2 stray bytes
	}
	p := &Property{Data: data}
	if got := PropertyOptions(p); len(got) != 1 || got[0] != "a" {
		t.Fatalf("got %#v, want [a]", got)
	}
	if m := PropertyOptionsMap(p); len(m) != 1 || m[0] != "a" {
		t.Fatalf("map got %#v", m)
	}
}

// DecodeNumericValue: every type's short-data branch and the unknown default.
func TestDecodeNumericValue_ShortAndUnknown(t *testing.T) {
	short4 := []byte{0x00, 0x00} // too short for 4-byte types
	short8 := []byte{0, 0, 0, 0} // too short for 8-byte types
	for _, nt := range []NumberType{
		NumTypeS8, NumTypeS16, NumTypeS32, NumTypeU8, NumTypeU16,
		NumTypeU32, NumTypeFloat, NumTypePreset, NumTypeIPv4,
	} {
		if _, _, _, err := DecodeNumericValue(nt, short4); err == nil {
			t.Fatalf("%v: expected short-data error", nt)
		}
	}
	for _, nt := range []NumberType{NumTypeS64, NumTypeU64} {
		if _, _, _, err := DecodeNumericValue(nt, short8); err == nil {
			t.Fatalf("%v: expected short-data error", nt)
		}
	}
	if _, _, _, err := DecodeNumericValue(NumberType(99), []byte{0, 0, 0, 0}); err == nil {
		t.Fatal("expected unknown-number-type error")
	}
}

// DecodeNumericValue: signed value paths (negative sign extension) and the
// 64-bit + float + preset + ipv4 happy paths not already covered.
func TestDecodeNumericValue_HappyPaths(t *testing.T) {
	// S8 negative: low byte 0xFF -> -1
	iv, _, _, err := DecodeNumericValue(NumTypeS8, []byte{0xFF, 0xFF, 0xFF, 0xFF})
	if err != nil || iv != -1 {
		t.Fatalf("S8: iv=%d err=%v", iv, err)
	}
	// S16 negative
	iv, _, _, err = DecodeNumericValue(NumTypeS16, []byte{0xFF, 0xFF, 0xFF, 0xFF})
	if err != nil || iv != -1 {
		t.Fatalf("S16: iv=%d err=%v", iv, err)
	}
	// Float
	_, _, fv, err := DecodeNumericValue(NumTypeFloat, []byte{0x3F, 0x80, 0x00, 0x00})
	if err != nil || fv != 1.0 {
		t.Fatalf("Float: fv=%v err=%v", fv, err)
	}
	// IPv4
	_, uv, _, err := DecodeNumericValue(NumTypeIPv4, []byte{0x7F, 0x00, 0x00, 0x01})
	if err != nil || uv != 0x7F000001 {
		t.Fatalf("IPv4: uv=%X err=%v", uv, err)
	}
	// U8 / U16 / U32 / S32 / S64 / U64 / Preset happy paths.
	if _, uv, _, err := DecodeNumericValue(NumTypeU8, []byte{0, 0, 0, 0xFF}); err != nil || uv != 0xFF {
		t.Fatalf("U8: uv=%d err=%v", uv, err)
	}
	if _, uv, _, err := DecodeNumericValue(NumTypeU16, []byte{0, 0, 0x12, 0x34}); err != nil || uv != 0x1234 {
		t.Fatalf("U16: uv=%X err=%v", uv, err)
	}
	if _, uv, _, err := DecodeNumericValue(NumTypeU32, []byte{0x12, 0x34, 0x56, 0x78}); err != nil || uv != 0x12345678 {
		t.Fatalf("U32: uv=%X err=%v", uv, err)
	}
	if iv, _, _, err := DecodeNumericValue(NumTypeS32, []byte{0xFF, 0xFF, 0xFF, 0xFF}); err != nil || iv != -1 {
		t.Fatalf("S32: iv=%d err=%v", iv, err)
	}
	if iv, _, _, err := DecodeNumericValue(NumTypeS64, []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}); err != nil || iv != -1 {
		t.Fatalf("S64: iv=%d err=%v", iv, err)
	}
	if _, uv, _, err := DecodeNumericValue(NumTypeU64, []byte{0, 0, 0, 0, 0, 0, 0, 7}); err != nil || uv != 7 {
		t.Fatalf("U64: uv=%d err=%v", uv, err)
	}
	if _, uv, _, err := DecodeNumericValue(NumTypePreset, []byte{0, 0, 0, 3}); err != nil || uv != 3 {
		t.Fatalf("Preset: uv=%d err=%v", uv, err)
	}
}

// EncodeNumericValue: every supported type plus the default error branch.
func TestEncodeNumericValue_AllTypesAndError(t *testing.T) {
	cases := []struct {
		nt      NumberType
		wantLen int
	}{
		{NumTypeS8, 4}, {NumTypeS16, 4}, {NumTypeS32, 4}, {NumTypeS64, 8},
		{NumTypeU8, 4}, {NumTypeU16, 4}, {NumTypeU32, 4}, {NumTypeU64, 8},
		{NumTypeFloat, 4}, {NumTypePreset, 4}, {NumTypeIPv4, 4},
	}
	for _, c := range cases {
		b, err := EncodeNumericValue(c.nt, -1, 0x2A, 3.5)
		if err != nil {
			t.Fatalf("%v: %v", c.nt, err)
		}
		if len(b) != c.wantLen {
			t.Fatalf("%v: len got %d want %d", c.nt, len(b), c.wantLen)
		}
	}
	// String number type is not encodable here -> default error branch.
	if _, err := EncodeNumericValue(NumTypeString, 0, 0, 0); err == nil {
		t.Fatal("expected error for non-encodable number type")
	}
}
