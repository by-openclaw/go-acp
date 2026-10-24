package codec

import (
	"strings"
	"testing"
)

// The pattern sent to the lab CCP-1601 on 2026-10-25: frame, 16×8 block
// top-left, diagonal from the top-left corner. Sent through Encode, the
// button showed exactly this (photo by the operator).
func testPattern() Bitmap {
	var b Bitmap
	for x := 0; x < BitmapW; x++ {
		b.Set(x, 0)
		b.Set(x, BitmapH-1)
	}
	for y := 0; y < BitmapH; y++ {
		b.Set(0, y)
		b.Set(BitmapW-1, y)
	}
	for y := 0; y < 8; y++ {
		for x := 0; x < 16; x++ {
			b.Set(x, y)
		}
	}
	for i := 0; i < 32; i++ {
		b.Set(i, i)
	}
	return b
}

func TestBitmapEncodeLayout(t *testing.T) {
	t.Parallel()
	// Single pixels pinned to the measured layout.
	cases := []struct {
		x, y  int
		index int
		bit   byte
	}{
		{63, 31, 0*4 + 1, 0x80}, // bottom-right: first column sent, band 0 → byte 1, MSB = lowest row
		{63, 24, 0*4 + 1, 0x01},
		{63, 23, 0*4 + 0, 0x80},
		{63, 0, 0*4 + 2, 0x01},  // top-right: band 3 → byte 2, LSB = top row of the band
		{0, 0, 63*4 + 2, 0x01},  // top-left: last column
		{0, 8, 63*4 + 3, 0x80},  // row 8: band 2 → byte 3, lowest row of the band is 15 → bit 7 is row 15
	}
	for _, c := range cases {
		var b Bitmap
		b.Set(c.x, c.y)
		got := b.Encode()
		for i, v := range got {
			want := byte(0)
			if i == c.index {
				want = c.bit
			}
			if c.y == 8 && i == c.index {
				want = 0x01 << 0 // row 8 is the TOP row of band rows 8-15 → bit 0 after the in-band reversal
			}
			if v != want {
				t.Fatalf("pixel (%d,%d): byte %d = %#02x, want %#02x", c.x, c.y, i, v, want)
			}
		}
	}
}

func TestBitmapRoundTripAndCommand(t *testing.T) {
	t.Parallel()
	p := testPattern()
	enc := p.Encode()
	if len(enc) != BitmapBytes {
		t.Fatalf("%d bytes", len(enc))
	}
	if d := Decode(enc); d != p {
		t.Fatalf("round trip differs:\n%s", d.String())
	}
	cmd := WriteBitmap(1, &p)
	if len(cmd) != 258 || cmd[0] != 0x06 || cmd[1] != 1 {
		t.Fatalf("command header % x len %d", cmd[:2], len(cmd))
	}
	// What the first, wrong, row-major attempt looked like on the panel is
	// what Decode makes of row-major bytes: side bars and dotted lines —
	// the photo the layout was measured from.
	var rowMajor [256]byte
	for y := 0; y < BitmapH; y++ {
		for bx := 0; bx < 8; bx++ {
			var v byte
			for bit := 0; bit < 8; bit++ {
				if p[y][bx*8+bit] {
					v |= 0x80 >> bit
				}
			}
			rowMajor[y*8+bx] = v
		}
	}
	seen := Decode(rowMajor[:])
	if !seen[0][0] || !seen[31][0] || !seen[0][63] || !seen[31][63] || !seen[8][4] || seen[8][3] {
		t.Fatalf("row-major bytes must show side bars and a dotted line at row 8:\n%s", seen.String())
	}
}

func TestText(t *testing.T) {
	t.Parallel()
	b := Text("CAM 1")
	s := b.String()
	if !strings.Contains(s, "#") {
		t.Fatal("empty render")
	}
	// Scale 2: 5 characters fill 58 px, centred → columns 3..60.
	for y := 0; y < BitmapH; y++ {
		if b[y][0] || b[y][1] || b[y][62] || b[y][63] {
			t.Fatalf("text not centred:\n%s", s)
		}
	}
	two := Text("SAT 1|LOCK")
	rows := 0
	for y := 0; y < BitmapH; y++ {
		for x := 0; x < BitmapW; x++ {
			if two[y][x] {
				rows++
				break
			}
		}
	}
	if rows < 20 {
		t.Fatalf("two lines at scale 2 should cover most rows, got %d:\n%s", rows, two.String())
	}
	small := Text("DISCOVERY|HD|12360V")
	if small == (Bitmap{}) {
		t.Fatal("small text empty")
	}
	if Text("") != (Bitmap{}) {
		t.Fatal("empty text must give an empty bitmap")
	}
	if LCDColour(0, 3, 0) != 0x0C || NamedColours["green"] != 0x0C {
		t.Fatal("green is 0x0C (measured)")
	}
}
