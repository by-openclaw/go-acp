package dtp

import (
	"bytes"
	"testing"
)

// TestBitmap_Packing pins the byte order: eight bits per byte, least
// significant byte first, so bit 0 is the low bit of the first byte. The
// specification's own example is a 17-bit map holding 0x12345, which lands on
// the wire as 45 23 01.
func TestBitmap_Packing(t *testing.T) {
	bm := NewBitmap(17)
	for i := range 17 {
		if 0x12345&(1<<uint(i)) != 0 {
			bm.Set(i)
		}
	}
	if want := mustHex(t, "45 23 01"); !bytes.Equal(bm.bytes(), want) {
		t.Errorf("packed %x, want %x", bm.bytes(), want)
	}
}

func TestBitmap_SetGetClear(t *testing.T) {
	bm := NewBitmap(10)
	if bm.Count() != 0 {
		t.Errorf("a new bitmap has %d bits set, want 0", bm.Count())
	}

	bm.Set(0)
	bm.Set(9)
	if !bm.Get(0) || !bm.Get(9) {
		t.Error("Set did not take")
	}
	if bm.Get(1) {
		t.Error("Get found a bit that was never set")
	}
	if bm.Count() != 2 {
		t.Errorf("Count = %d, want 2", bm.Count())
	}
	if want := []int{0, 9}; !equalInts(bm.Indices(), want) {
		t.Errorf("Indices = %v, want %v", bm.Indices(), want)
	}

	bm.Clear(0)
	if bm.Get(0) {
		t.Error("Clear did not take")
	}
	if bm.Count() != 1 {
		t.Errorf("Count = %d, want 1", bm.Count())
	}
}

// TestBitmap_OutOfRange covers a peer sending a shorter bitmap than the reader
// expects. That means "not set" for the bits it does not reach, not a
// malformed message, so reading past the end must not panic.
func TestBitmap_OutOfRange(t *testing.T) {
	bm := NewBitmap(4)

	for _, i := range []int{-1, 4, 1000} {
		if bm.Get(i) {
			t.Errorf("Get(%d) = true, want false", i)
		}
		bm.Set(i)   // must be ignored
		bm.Clear(i) // must be ignored
	}
	if bm.Count() != 0 {
		t.Errorf("an out-of-range Set changed the bitmap: %s", bm)
	}

	empty := NewBitmap(0)
	if empty.Count() != 0 || len(empty.Indices()) != 0 {
		t.Errorf("an empty bitmap is not empty: %s", empty)
	}
	if got := NewBitmap(-5); got.NumBits != 0 {
		t.Errorf("a negative size gave %d bits, want 0", got.NumBits)
	}
}

func TestBitmapOf(t *testing.T) {
	bm := BitmapOf(0, 3, 7)
	if bm.NumBits != 8 {
		t.Errorf("NumBits = %d, want 8 to hold bit 7", bm.NumBits)
	}
	if want := []int{0, 3, 7}; !equalInts(bm.Indices(), want) {
		t.Errorf("Indices = %v, want %v", bm.Indices(), want)
	}
	if bm.Bits[0] != 0x89 {
		t.Errorf("packed %02x, want 89", bm.Bits[0])
	}

	if got := BitmapOf(); got.NumBits != 0 {
		t.Errorf("BitmapOf() = %d bits, want 0", got.NumBits)
	}
	// One bit set high up sizes the whole map.
	if got := BitmapOf(64); got.NumBits != 65 || len(got.Bits) != 9 {
		t.Errorf("BitmapOf(64) = %d bits in %d bytes, want 65 in 9", got.NumBits, len(got.Bits))
	}
}

// TestBitmap_ShortBackingArray covers a bitmap built by hand whose byte slice
// is smaller than its bit count claims. Encoding must widen rather than send a
// truncated map, which a receiver would read as bits that are simply clear.
func TestBitmap_ShortBackingArray(t *testing.T) {
	bm := Bitmap{NumBits: 17, Bits: []byte{0xFF}}
	got := bm.bytes()
	if len(got) != 3 {
		t.Fatalf("packed %d bytes, want 3 for 17 bits", len(got))
	}
	if want := []byte{0xFF, 0, 0}; !bytes.Equal(got, want) {
		t.Errorf("packed %x, want %x", got, want)
	}

	// A longer backing array is trimmed to the bit count.
	long := Bitmap{NumBits: 9, Bits: []byte{1, 2, 3, 4}}
	if got := long.bytes(); len(got) != 2 {
		t.Errorf("packed %d bytes, want 2 for 9 bits", len(got))
	}
}

func TestBitmap_String(t *testing.T) {
	tests := []struct {
		in   Bitmap
		want string
	}{
		{NewBitmap(0), "bitmap(0)[]"},
		{BitmapOf(0), "bitmap(1)[1]"},
		{BitmapOf(0, 2), "bitmap(3)[101]"},
		{NewBitmap(4), "bitmap(4)[0000]"},
	}
	for _, tc := range tests {
		if got := tc.in.String(); got != tc.want {
			t.Errorf("String() = %q, want %q", got, tc.want)
		}
	}
}

func equalInts(a, b []int) bool {
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
