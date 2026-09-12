package dtp

import (
	"fmt"
	"strings"
)

// Bitmap is a run of bits carried by a TypeBitmap item.
//
// On the wire it is a bit count followed by the bits packed eight to a byte,
// least significant byte first, so bit 0 is the low bit of the first byte. The
// router command set uses one to say which levels of a matrix a request applies
// to, where the alternative would be an array of level numbers costing a byte
// each.
type Bitmap struct {
	NumBits int
	Bits    []byte
}

// NewBitmap returns a bitmap of n bits, all clear.
func NewBitmap(n int) Bitmap {
	if n < 0 {
		n = 0
	}
	return Bitmap{NumBits: n, Bits: make([]byte, (n+7)/8)}
}

// BitmapOf returns a bitmap with the listed bits set, sized to hold the
// highest of them.
func BitmapOf(bits ...int) Bitmap {
	high := -1
	for _, b := range bits {
		if b > high {
			high = b
		}
	}
	bm := NewBitmap(high + 1)
	for _, b := range bits {
		bm.Set(b)
	}
	return bm
}

// Get reports whether bit i is set. An index outside the bitmap reads as
// false rather than panicking, because a peer may send a shorter bitmap than
// the reader expects and that means "not set", not "malformed".
func (b Bitmap) Get(i int) bool {
	if i < 0 || i >= b.NumBits {
		return false
	}
	return b.Bits[i/8]&(1<<uint(i%8)) != 0
}

// Set sets bit i. An index outside the bitmap is ignored, matching Get.
func (b Bitmap) Set(i int) {
	if i < 0 || i >= b.NumBits {
		return
	}
	b.Bits[i/8] |= 1 << uint(i%8)
}

// Clear clears bit i.
func (b Bitmap) Clear(i int) {
	if i < 0 || i >= b.NumBits {
		return
	}
	b.Bits[i/8] &^= 1 << uint(i%8)
}

// Count returns how many bits are set.
func (b Bitmap) Count() int {
	n := 0
	for i := range b.NumBits {
		if b.Get(i) {
			n++
		}
	}
	return n
}

// Indices returns the set bit positions in ascending order. This is what a
// caller wants when the bitmap names levels or channels.
func (b Bitmap) Indices() []int {
	out := make([]int, 0, b.Count())
	for i := range b.NumBits {
		if b.Get(i) {
			out = append(out, i)
		}
	}
	return out
}

// bytes returns the packed bits, sized to the bit count. Trailing bits beyond
// NumBits in the final byte are left as they are: the count is what bounds the
// bitmap, and the vendor does not clear them either.
func (b Bitmap) bytes() []byte {
	want := (b.NumBits + 7) / 8
	if len(b.Bits) >= want {
		return b.Bits[:want]
	}
	out := make([]byte, want)
	copy(out, b.Bits)
	return out
}

func (b Bitmap) String() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "bitmap(%d)[", b.NumBits)
	// Printed most significant bit first, the way the specification writes
	// its worked example.
	for i := b.NumBits - 1; i >= 0; i-- {
		if b.Get(i) {
			sb.WriteByte('1')
		} else {
			sb.WriteByte('0')
		}
	}
	sb.WriteByte(']')
	return sb.String()
}
