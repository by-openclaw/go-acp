package dtp

import (
	"bytes"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		t.Fatalf("bad test vector %q: %v", s, err)
	}
	return b
}

// TestSpecSimpleExample is the first worked example of the Full Control Command
// Set: a single uint of 200 decimal, which needs two bytes because 200 does not
// fit in the seven bits one byte carries.
func TestSpecSimpleExample(t *testing.T) {
	want := mustHex(t, "01 05 c8 01")

	got, err := Encode(Params{Uint(200)}, false)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("encoded %x, want %x", got, want)
	}

	back, err := Decode(want)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(back) != 1 || back[0].Type != TypeUint || back[0].Uint != 200 {
		t.Errorf("decoded %v, want a single uint of 200", back)
	}
}

// specComplexExample is the second worked example, byte for byte from the
// document's offset table. It exercises every item type in one block, which
// makes it the single most valuable vector in this package: it was written by
// the protocol's author, not by this implementation.
const specComplexExample = "06" + // <number-of-items> = 6
	"01" + "04" + "01" + "7f" + "8001" + "d209" + // uint array {1, 127, 128, 1234}
	"02" + "11" + "452301" + // bitmap, 17 bits, 0x12345
	"03" + // bool false
	"04" + // bool true
	"05" + "c5c604" + // uint 0x12345
	"06" + "09" + "446576696365 5f3233" // string "Device_23"

func specComplexParams() Params {
	bm := NewBitmap(17)
	// 0x12345 written into the bitmap, low bit first.
	for i := range 17 {
		if 0x12345&(1<<uint(i)) != 0 {
			bm.Set(i)
		}
	}
	return Params{
		Uints(1, 127, 128, 1234),
		Bits(bm),
		Bool(false),
		Bool(true),
		Uint(0x12345),
		String("Device_23"),
	}
}

func TestSpecComplexExample_Encode(t *testing.T) {
	got, err := Encode(specComplexParams(), false)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	want := mustHex(t, specComplexExample)
	if !bytes.Equal(got, want) {
		t.Errorf("encoded\n got %x\nwant %x", got, want)
	}
	// The document's table ends at offset 30, so the block is 31 bytes.
	if len(got) != 31 {
		t.Errorf("block is %d bytes, want the 31 of the worked example", len(got))
	}
}

func TestSpecComplexExample_Decode(t *testing.T) {
	got, err := Decode(mustHex(t, specComplexExample))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(got) != 6 {
		t.Fatalf("decoded %d items, want 6", len(got))
	}

	if want := []uint32{1, 127, 128, 1234}; !equalUints(got[0].Uints, want) {
		t.Errorf("uint array = %v, want %v", got[0].Uints, want)
	}
	if got[1].Bitmap.NumBits != 17 {
		t.Errorf("bitmap has %d bits, want 17", got[1].Bitmap.NumBits)
	}
	for i := range 17 {
		want := 0x12345&(1<<uint(i)) != 0
		if got[1].Bitmap.Get(i) != want {
			t.Errorf("bitmap bit %d = %v, want %v", i, got[1].Bitmap.Get(i), want)
		}
	}
	if v, ok := got[2].Bool(); !ok || v {
		t.Errorf("item 2 = %v,%v, want false,true", v, ok)
	}
	if v, ok := got[3].Bool(); !ok || !v {
		t.Errorf("item 3 = %v,%v, want true,true", v, ok)
	}
	if got[4].Uint != 0x12345 {
		t.Errorf("uint = %#x, want 0x12345", got[4].Uint)
	}
	if got[5].Str != "Device_23" {
		t.Errorf("string = %q, want %q", got[5].Str, "Device_23")
	}
}

func equalUints(a, b []uint32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestVarUint pins the integer encoding: seven bits per byte, least significant
// first, continuation in bit 7. The boundaries are what the document calls out,
// so they are all here.
func TestVarUint(t *testing.T) {
	tests := []struct {
		v   uint32
		hex string
	}{
		{0, "00"},
		{1, "01"},
		{127, "7f"},         // the largest single byte
		{128, "8001"},       // the first two-byte value
		{200, "c801"},       // the document's simple example
		{1234, "d209"},      // 0x4D2
		{0x12345, "c5c604"}, // the document's complex example
		{16383, "ff7f"},     // the largest two-byte value
		{16384, "808001"},   // the first three-byte value
		{1 << 21, "80808001"},
		{1 << 28, "8080808001"},
		{0xFFFFFFFF, "ffffffff0f"}, // the largest 32-bit value, five bytes
	}
	for _, tc := range tests {
		want := mustHex(t, tc.hex)

		got := appendVarUint(nil, tc.v)
		if !bytes.Equal(got, want) {
			t.Errorf("appendVarUint(%d) = %x, want %x", tc.v, got, want)
		}
		if n := VarUintSize(tc.v); n != len(want) {
			t.Errorf("VarUintSize(%d) = %d, want %d", tc.v, n, len(want))
		}

		back, n, err := decodeVarUint(want)
		if err != nil {
			t.Errorf("decodeVarUint(%x): %v", want, err)
			continue
		}
		if back != tc.v || n != len(want) {
			t.Errorf("decodeVarUint(%x) = %d,%d want %d,%d", want, back, n, tc.v, len(want))
		}
	}
}

func TestVarUint_Errors(t *testing.T) {
	// Continuation set on every byte, so the value never terminates.
	if _, _, err := decodeVarUint(mustHex(t, "ffffffffff")); !errors.Is(err, ErrVarUintOverflow) {
		t.Errorf("err = %v, want ErrVarUintOverflow", err)
	}
	// Five bytes where the last contributes bits above 32.
	if _, _, err := decodeVarUint(mustHex(t, "ffffffff10")); !errors.Is(err, ErrVarUintOverflow) {
		t.Errorf("err = %v, want ErrVarUintOverflow", err)
	}
	// Five bytes that each stay within 32 bits but still ask for a sixth.
	if _, _, err := decodeVarUint(mustHex(t, "ffffffff8f01")); !errors.Is(err, ErrVarUintOverflow) {
		t.Errorf("err = %v, want ErrVarUintOverflow", err)
	}
	// Ends mid-value.
	if _, _, err := decodeVarUint(mustHex(t, "80")); !errors.Is(err, ErrShortBuffer) {
		t.Errorf("err = %v, want ErrShortBuffer", err)
	}
	if _, _, err := decodeVarUint(nil); !errors.Is(err, ErrShortBuffer) {
		t.Errorf("err = %v, want ErrShortBuffer", err)
	}
}

func TestRoundTrip(t *testing.T) {
	cases := []Params{
		{},
		{Uint(0)},
		{Bool(true), Bool(false)},
		{String("")},
		{String(strings.Repeat("x", MaxStringLen))},
		{Uints()},
		{Uints(0, 0xFFFFFFFF)},
		{Bits(NewBitmap(0))},
		{Bits(BitmapOf(0, 7, 8, 63))},
		specComplexParams(),
	}
	for _, safe := range []bool{false, true} {
		for i, p := range cases {
			b, err := Encode(p, safe)
			if err != nil {
				t.Fatalf("case %d safe=%v: Encode: %v", i, safe, err)
			}
			if IsStringSafe(b) != safe {
				t.Errorf("case %d: IsStringSafe = %v, want %v", i, IsStringSafe(b), safe)
			}
			got, err := Decode(b)
			if err != nil {
				t.Fatalf("case %d safe=%v: Decode: %v", i, safe, err)
			}
			if !sameParams(got, p) {
				t.Errorf("case %d safe=%v round trip\n got %v\nwant %v", i, safe, got, p)
			}
		}
	}
}

func sameParams(a, b Params) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Type != b[i].Type || a[i].Uint != b[i].Uint || a[i].Str != b[i].Str {
			return false
		}
		if !equalUints(a[i].Uints, b[i].Uints) {
			return false
		}
		if a[i].Bitmap.NumBits != b[i].Bitmap.NumBits {
			return false
		}
		for k := range a[i].Bitmap.NumBits {
			if a[i].Bitmap.Get(k) != b[i].Bitmap.Get(k) {
				return false
			}
		}
	}
	return true
}

// TestDecode_UnknownTypeKeepsWhatItUnderstood is the forward-compatibility rule
// the document states: a receiver meeting parameters it does not know must work
// with the ones it does, rather than reject the message. An unknown type cannot
// be skipped, because its length is defined by its type, so decoding stops
// there and returns what came before.
func TestDecode_UnknownTypeKeepsWhatItUnderstood(t *testing.T) {
	// Three items: a uint, a string, then a type from some later revision.
	block := mustHex(t, "03"+"05 2a"+"06 02 6f6b"+"63 11 22 33")

	got, err := Decode(block)
	if !errors.Is(err, ErrUnknownItemType) {
		t.Fatalf("err = %v, want ErrUnknownItemType", err)
	}
	if len(got) != 2 {
		t.Fatalf("kept %d items, want the 2 that were understood", len(got))
	}
	if got[0].Uint != 42 || got[1].Str != "ok" {
		t.Errorf("kept %v, want uint 42 and \"ok\"", got)
	}
}

func TestDecode_Errors(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want error
	}{
		{"empty", "", ErrShortBuffer},
		{"count without items", "01", ErrShortBuffer},
		{"uint runs out", "01 05", ErrShortBuffer},
		{"array longer than the block", "01 01 7f 01", ErrShortBuffer},
		{"array length never terminates", "01 01 80", ErrShortBuffer},
		{"array element runs out", "01 01 02 01", ErrShortBuffer},
		// The declared length fits what is left, but the element itself is a
		// variable-length integer that continues past the end.
		{"array element never terminates", "01 01 01 80", ErrShortBuffer},
		{"bitmap shorter than its bit count", "01 02 20 ff", ErrShortBuffer},
		{"bitmap count runs out", "01 02", ErrShortBuffer},
		{"string without a length", "01 06", ErrShortBuffer},
		{"string shorter than its length", "01 06 05 6162", ErrShortBuffer},
		{"second item missing", "02 03", ErrShortBuffer},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var in []byte
			if tc.in != "" {
				in = mustHex(t, tc.in)
			}
			if _, err := Decode(in); !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestEncode_Errors(t *testing.T) {
	tooMany := make(Params, MaxItems+1)
	for i := range tooMany {
		tooMany[i] = Bool(true)
	}
	if _, err := Encode(tooMany, false); !errors.Is(err, ErrTooManyItems) {
		t.Errorf("err = %v, want ErrTooManyItems", err)
	}
	// The limit itself is accepted.
	if _, err := Encode(tooMany[:MaxItems], false); err != nil {
		t.Errorf("%d items must be accepted: %v", MaxItems, err)
	}

	long := Params{String(strings.Repeat("x", MaxStringLen+1))}
	if _, err := Encode(long, false); !errors.Is(err, ErrStringTooLong) {
		t.Errorf("err = %v, want ErrStringTooLong", err)
	}

	if _, err := Encode(Params{{Type: 99}}, false); !errors.Is(err, ErrUnknownItemType) {
		t.Errorf("err = %v, want ErrUnknownItemType", err)
	}
}

func TestAppend_PreservesPrefix(t *testing.T) {
	got, err := Append([]byte{0xAA, 0xBB}, Params{Bool(true)}, false)
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if want := []byte{0xAA, 0xBB, 0x01, 0x04}; !bytes.Equal(got, want) {
		t.Errorf("= %x, want %x", got, want)
	}
}

func TestType_String(t *testing.T) {
	tests := map[Type]string{
		TypeUintArray: "uint-array", TypeBitmap: "bitmap", TypeFalse: "false",
		TypeTrue: "true", TypeUint: "uint", TypeString: "string", 99: "type(99)",
	}
	for typ, want := range tests {
		if got := typ.String(); got != want {
			t.Errorf("Type(%d) = %q, want %q", typ, got, want)
		}
	}
}

func TestItem_String(t *testing.T) {
	tests := []struct {
		in   Item
		want string
	}{
		{Uint(42), "uint(42)"},
		{Uints(1, 2), "uints[1 2]"},
		{Bool(true), "true"},
		{Bool(false), "false"},
		{String("hi"), `"hi"`},
		{Item{Type: 99}, "type(99)"},
	}
	for _, tc := range tests {
		if got := tc.in.String(); got != tc.want {
			t.Errorf("String() = %q, want %q", got, tc.want)
		}
	}
	if got := Bits(BitmapOf(0, 2)).String(); got != "bitmap(3)[101]" {
		t.Errorf("bitmap String() = %q", got)
	}
}

func TestParams_String(t *testing.T) {
	if got := (Params{}).String(); got != "[]" {
		t.Errorf("empty = %q", got)
	}
	if got := (Params{Uint(1), Bool(true)}).String(); got != "[uint(1) true]" {
		t.Errorf("= %q", got)
	}
}

func TestItem_BoolOnNonBool(t *testing.T) {
	if v, ok := Uint(1).Bool(); ok || v {
		t.Errorf("Bool() on a uint = %v,%v, want false,false", v, ok)
	}
}

func TestIsStringSafe_Empty(t *testing.T) {
	if IsStringSafe(nil) {
		t.Error("an empty block is not string-safe")
	}
}
