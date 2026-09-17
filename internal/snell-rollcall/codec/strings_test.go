package codec

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// TestFixedString covers the fixed-width text field of the 16-bit
// generation. The case that matters most is the last one: bytes after the
// terminator are undefined by the specification, and every real device leaves
// junk there. Treating that junk as data, or as a compliance event, is wrong.
func TestFixedString(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		want string
	}{
		{"terminated", []byte("Vega\x00\x00\x00\x00"), "Vega"},
		{"empty", []byte("\x00\x00\x00\x00\x00\x00\x00\x00"), ""},
		{"fills the field", []byte("ABCDEFGH"), "ABCDEFGH"},
		{"leading terminator wins", []byte("\x00BCDEFGH"), ""},
		// A live Centra pads with 0xCD; the vendor test client does too.
		{"vendor padding after NUL", []byte("Hub\x00\xcd\xcd\xcd\xcd"), "Hub"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := fixedString(tc.in); got != tc.want {
				t.Errorf("= %q, want %q", got, tc.want)
			}
		})
	}
}

// TestDecodeError_NamesTheField pins that a decode failure names the structure
// and field rather than only a byte offset, so a log line from the field is
// actionable without a hex dump.
func TestDecodeError_NamesTheField(t *testing.T) {
	err := need([]byte("abc"), 8, "ID", "Name")
	if !errors.Is(err, ErrShortBuffer) {
		t.Fatalf("err = %v, want ErrShortBuffer", err)
	}
	var de *DecodeError
	if !errors.As(err, &de) {
		t.Fatalf("err = %v, want a *DecodeError", err)
	}
	if de.Struct != "ID" || de.Field != "Name" {
		t.Errorf("DecodeError names %s.%s, want ID.Name", de.Struct, de.Field)
	}
	if got := de.Error(); !strings.Contains(got, "ID.Name") {
		t.Errorf("Error() = %q, want it to name the field", got)
	}
}

// TestAppendFixedString checks that we zero-fill rather than leaking whatever
// was in the buffer. Devices tolerate junk padding, but emitting it makes our
// own frames non-reproducible and our golden fixtures worthless.
func TestAppendFixedString(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		width int
		want  []byte
	}{
		{"padded", "Vega", 8, []byte("Vega\x00\x00\x00\x00")},
		{"empty", "", 4, []byte{0, 0, 0, 0}},
		{"exactly fits with terminator", "ABC", 4, []byte("ABC\x00")},
		{"wider than the stack buffer", "x", 40, append([]byte("x"), make([]byte, 39)...)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := appendFixedString(nil, tc.in, tc.width)
			if err != nil {
				t.Fatalf("appendFixedString: %v", err)
			}
			if !bytes.Equal(got, tc.want) {
				t.Errorf("= %x, want %x", got, tc.want)
			}
		})
	}

	// Appending must not disturb what is already in the buffer.
	got, err := appendFixedString([]byte{0xAA, 0xBB}, "Hi", 4)
	if err != nil {
		t.Fatalf("appendFixedString: %v", err)
	}
	if want := []byte{0xAA, 0xBB, 'H', 'i', 0, 0}; !bytes.Equal(got, want) {
		t.Errorf("append = %x, want %x", got, want)
	}
}

// TestAppendFixedString_TooLong pins that overlong text is an error rather than
// a silent truncation. A truncated label is data loss the caller must decide
// about; only TruncateFixed does it, and only where the protocol says to.
func TestAppendFixedString_TooLong(t *testing.T) {
	for _, tc := range []struct {
		in    string
		width int
	}{
		{"ABCD", 4}, // no room for the terminator
		{strings.Repeat("x", MaxTextSize), MaxTextSize},
	} {
		if _, err := appendFixedString(nil, tc.in, tc.width); !errors.Is(err, ErrStringTooLong) {
			t.Errorf("appendFixedString(%q, %d) err = %v, want ErrStringTooLong",
				tc.in, tc.width, err)
		}
	}

	// The largest string that does fit must be accepted.
	ok := strings.Repeat("x", MaxTextSize-1)
	if _, err := appendFixedString(nil, ok, MaxTextSize); err != nil {
		t.Errorf("a %d-byte string must fit a %d-byte field: %v", len(ok), MaxTextSize, err)
	}
}

func TestFixedString_RoundTrip(t *testing.T) {
	for _, s := range []string{"", "a", "Vega", "IQMDA00 Analog", strings.Repeat("z", 19)} {
		b, err := appendFixedString(nil, s, MaxTextSize)
		if err != nil {
			t.Fatalf("append %q: %v", s, err)
		}
		if got := fixedString(b); got != s {
			t.Errorf("round trip %q -> %q", s, got)
		}
	}
}

// TestTruncateFixed covers the mandated-truncation path, including the UTF-8
// rule: a multi-byte rune is dropped whole, never cut in half. A half rune on
// the wire renders as a replacement character on every client.
func TestTruncateFixed(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		width int
		want  string
	}{
		{"fits", "Vega", 8, "Vega"},
		{"exactly fits", "ABC", 4, "ABC"},
		{"cut", "ABCDEFGH", 4, "ABC"},
		{"empty", "", 4, ""},
		// "e" + 3 x 2-byte runes = 7 bytes; a 6-byte field holds 5, and the
		// rune spanning that boundary is dropped rather than split.
		{"utf-8 boundary", "eééé", 6, "eéé"},
		{"first rune is multi-byte", "éx", 2, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := TruncateFixed(tc.in, tc.width)
			if got != tc.want {
				t.Errorf("= %q, want %q", got, tc.want)
			}
			if len(got) > tc.width-1 {
				t.Errorf("%q is %d bytes, too long for a %d-byte field",
					got, len(got), tc.width)
			}
			// Whatever comes out must still fit the field it was cut for.
			if _, err := appendFixedString(nil, got, tc.width); err != nil {
				t.Errorf("truncated text still does not fit: %v", err)
			}
		})
	}
}

// TestCString covers the NUL-terminated form of the 32-bit generation,
// including the unterminated tail the vendor emits when a value exactly fills
// its buffer.
func TestCString(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		want string
		n    int
	}{
		{"terminated", []byte("Vega\x00"), "Vega", 5},
		{"terminated with a tail", []byte("Vega\x00rest"), "Vega", 5},
		{"empty", []byte{0}, "", 1},
		{"unterminated tail", []byte("Vega"), "Vega", 4},
		{"nothing at all", nil, "", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, n := CString(tc.in)
			if got != tc.want || n != tc.n {
				t.Errorf("= %q,%d want %q,%d", got, n, tc.want, tc.n)
			}
		})
	}
}

func TestAppendCString(t *testing.T) {
	got, err := appendCString([]byte{0xAA}, "Vega")
	if err != nil {
		t.Fatalf("appendCString: %v", err)
	}
	if want := []byte("\xaaVega\x00"); !bytes.Equal(got, want) {
		t.Errorf("= %x, want %x", got, want)
	}

	// The long-string ceiling includes the terminator.
	ok := strings.Repeat("x", MaxLongString-1)
	if _, err := appendCString(nil, ok); err != nil {
		t.Errorf("a %d-byte string must fit: %v", len(ok), err)
	}
	if _, err := appendCString(nil, ok+"x"); !errors.Is(err, ErrStringTooLong) {
		t.Errorf("err = %v, want ErrStringTooLong", err)
	}
}

func TestCString_RoundTrip(t *testing.T) {
	for _, s := range []string{"", "a", "Vega", "IQMDA00 Analog Video", strings.Repeat("z", 63)} {
		b, err := appendCString(nil, s)
		if err != nil {
			t.Fatalf("append %q: %v", s, err)
		}
		got, n := CString(b)
		if got != s || n != len(b) {
			t.Errorf("round trip %q -> %q (consumed %d of %d)", s, got, n, len(b))
		}
	}
}

// TestDecodeError_NoField covers the form used when a failure belongs to a
// whole structure rather than one of its fields.
func TestDecodeError_NoField(t *testing.T) {
	err := decodeErr("Connect", "", 12, ErrShortBuffer)
	if got := err.Error(); !strings.Contains(got, "Connect at +12") {
		t.Errorf("Error() = %q", got)
	}
	if !errors.Is(err, ErrShortBuffer) {
		t.Error("Unwrap must expose the sentinel")
	}
}
