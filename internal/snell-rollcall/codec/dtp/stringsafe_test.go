package dtp

import (
	"bytes"
	"errors"
	"testing"
)

// TestStringSafe_RemovesInteriorZeros is the whole point of the encoding: a
// block carried in a string parameter passes through the vendor's strcpy-style
// copies, which stop at the first NUL. An interior zero would truncate the
// block and lose every parameter after it.
func TestStringSafe_RemovesInteriorZeros(t *testing.T) {
	// A uint of zero encodes as a zero byte, which is exactly the hazard.
	p := Params{Uint(0), Uint(1), Uint(0)}

	plain, err := Encode(p, false)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !bytes.Contains(plain[1:len(plain)-1], []byte{0}) {
		t.Fatal("the test block must contain an interior zero to be meaningful")
	}

	safe, err := Encode(p, true)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if bytes.Contains(safe[1:len(safe)-1], []byte{0}) {
		t.Errorf("string-safe block still has an interior zero: %x", safe)
	}
	if !IsStringSafe(safe) {
		t.Error("the count byte must carry the string-safe flag")
	}

	back, err := Decode(safe)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !sameParams(back, p) {
		t.Errorf("round trip\n got %v\nwant %v", back, p)
	}
}

// TestStringSafe_Escapes pins the two escape sequences and that everything else
// passes through untouched.
func TestStringSafe_Escapes(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"nothing to escape", "01 02 03", "01 02 03"},
		{"interior zero", "01 00 03", "01 ff fd 03"},
		{"interior ff", "01 ff 03", "01 ff fe 03"},
		{"both", "01 00 ff 03", "01 ff fd ff fe 03"},
		// The exemption in the specification is the block's first and last
		// byte. The block's first byte is the count, which this function
		// never sees, so within the body only the final byte is exempt.
		{"leading zero is interior to the block", "00 01 02", "ff fd 01 02"},
		{"trailing zero", "01 02 00", "01 02 00"},
		{"trailing ff", "01 02 ff", "01 02 ff"},
		{"single byte is the last byte", "00", "00"},
		{"two bytes", "00 00", "ff fd 00"},
		{"empty", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var body []byte
			if tc.body != "" {
				body = mustHex(t, tc.body)
			}
			got := appendEscaped(nil, body)
			var want []byte
			if tc.want != "" {
				want = mustHex(t, tc.want)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("escaped %x, want %x", got, want)
			}

			back, err := unescape(got)
			if err != nil {
				t.Fatalf("unescape: %v", err)
			}
			if !bytes.Equal(back, body) {
				t.Errorf("round trip %x -> %x", body, back)
			}
		})
	}
}

// TestUnescape_AcceptsAnUnescapedTail covers a peer that follows the
// specification's exemption literally and leaves the final byte alone. Both
// forms have to decode, or a vendor-encoded block would be rejected.
func TestUnescape_AcceptsAnUnescapedTail(t *testing.T) {
	for _, in := range []string{"01 02 ff", "01 02 00"} {
		got, err := unescape(mustHex(t, in))
		if err != nil {
			t.Errorf("unescape(%s): %v", in, err)
			continue
		}
		if want := mustHex(t, in); !bytes.Equal(got, want) {
			t.Errorf("unescape(%s) = %x, want it unchanged", in, got)
		}
	}
}

func TestUnescape_BadEscape(t *testing.T) {
	// 0xFF followed by anything but FD or FE is not a defined sequence.
	if _, err := unescape(mustHex(t, "01 ff 42 03")); !errors.Is(err, ErrBadEscape) {
		t.Errorf("err = %v, want ErrBadEscape", err)
	}
}

func TestDecode_BadStringSafeBlock(t *testing.T) {
	// The count byte marks it string-safe, and the body has a bad escape.
	if _, err := Decode(mustHex(t, "81 ff 42 03")); !errors.Is(err, ErrBadEscape) {
		t.Errorf("err = %v, want ErrBadEscape", err)
	}
}

// TestStringValue covers the wrapper a router controller reads from the string
// half of a numeric-plus-string command: one size byte, then the string-safe
// block.
func TestStringValue(t *testing.T) {
	p := Params{Uint(0x12345), String("Device_23"), Bool(true)}

	b, err := AppendStringValue(nil, p)
	if err != nil {
		t.Fatalf("AppendStringValue: %v", err)
	}
	if int(b[0]) != len(b)-1 {
		t.Errorf("size byte says %d, block is %d bytes", b[0], len(b)-1)
	}
	if !IsStringSafe(b[1:]) {
		t.Error("the wrapped block must be string-safe")
	}
	if bytes.Contains(b[1:len(b)-1], []byte{0}) {
		t.Errorf("wrapped block has an interior zero: %x", b)
	}

	got, n, err := ParseStringValue(b)
	if err != nil {
		t.Fatalf("ParseStringValue: %v", err)
	}
	if n != len(b) {
		t.Errorf("consumed %d of %d bytes", n, len(b))
	}
	if !sameParams(got, p) {
		t.Errorf("round trip\n got %v\nwant %v", got, p)
	}

	// Anything after the block is left for the caller.
	trailing := append(append([]byte{}, b...), 0xAA, 0xBB)
	_, n, err = ParseStringValue(trailing)
	if err != nil {
		t.Fatalf("ParseStringValue: %v", err)
	}
	if n != len(b) {
		t.Errorf("consumed %d bytes, want %d so the tail is left alone", n, len(b))
	}
}

func TestStringValue_Errors(t *testing.T) {
	if _, _, err := ParseStringValue(nil); !errors.Is(err, ErrShortBuffer) {
		t.Errorf("err = %v, want ErrShortBuffer", err)
	}
	if _, _, err := ParseStringValue([]byte{0x10, 0x01}); !errors.Is(err, ErrShortBuffer) {
		t.Errorf("err = %v, want ErrShortBuffer", err)
	}

	// A block that decodes badly still reports how much it consumed, so the
	// caller can skip it.
	_, n, err := ParseStringValue([]byte{0x02, 0x81, 0xFF})
	if err == nil {
		t.Error("a malformed block must be reported")
	}
	if n != 3 {
		t.Errorf("consumed %d, want 3 so the caller can move past it", n)
	}

	// Too many items to encode.
	tooMany := make(Params, MaxItems+1)
	for i := range tooMany {
		tooMany[i] = Bool(true)
	}
	if _, err := AppendStringValue(nil, tooMany); !errors.Is(err, ErrTooManyItems) {
		t.Errorf("err = %v, want ErrTooManyItems", err)
	}

	// A block whose escaped form exceeds what one size byte can describe.
	big := Params{String(string(make([]byte, MaxStringLen)))}
	if _, err := AppendStringValue(nil, big); !errors.Is(err, ErrStringTooLong) {
		t.Errorf("err = %v, want ErrStringTooLong", err)
	}
}

// TestStringSafe_WorstCaseGrowth records that escaping can nearly double a
// block, which is what bounds how much a caller may put in one. A block of all
// zeros is the worst case.
func TestStringSafe_WorstCaseGrowth(t *testing.T) {
	p := Params{Uints(0, 0, 0, 0, 0, 0, 0, 0)}

	plain, err := Encode(p, false)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	safe, err := Encode(p, true)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if len(safe) <= len(plain) {
		t.Fatalf("string-safe form is %d bytes against %d plain; it must grow",
			len(safe), len(plain))
	}
	if len(safe) > 2*len(plain) {
		t.Errorf("string-safe form more than doubled: %d against %d", len(safe), len(plain))
	}
}
