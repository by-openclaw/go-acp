package codec

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

func inkRows(b Bitmap) (first, last int) {
	first, last = -1, -1
	for y := 0; y < BitmapH; y++ {
		for x := 0; x < BitmapW; x++ {
			if b[y][x] {
				if first < 0 {
					first = y
				}
				last = y
				break
			}
		}
	}
	return
}

func TestTextSizesPickTheLargestThatFits(t *testing.T) {
	t.Parallel()
	cases := []struct {
		text     string
		frame    bool
		wantSize string
		minLines int
	}{
		{"CAM 1", false, "large", 1},
		{"SAT|1", false, "large", 2},
		{"LONG LABEL 1234", false, "normal", 2},
		{"SAT|DISCOVERY|12360V", false, "normal", 3},
		{"A|B|C|D|E", false, "small", 5},
		{"DISCOVERY CHANNEL HD 12360 V 27500 DVB-S2", false, "small", 3},
	}
	for _, c := range cases {
		b := TextScaled(c.text, c.frame, SizeAuto)
		first, last := inkRows(b)
		height := last - first + 1
		switch c.wantSize {
		case "large":
			if height < 14 {
				t.Errorf("%q: ink height %d, want large (>= 14)", c.text, height)
			}
		case "normal", "small":
			if first < 0 {
				t.Errorf("%q: empty", c.text)
			}
		}
	}
	// Five one-letter lines cannot fit at normal size (4 lines): small font.
	b := TextScaled("A|B|C|D|E", false, SizeAuto)
	first, last := inkRows(b)
	if last-first+1 > 29 || first < 0 {
		t.Fatalf("5 lines must fit in 32 rows:\n%s", b.String())
	}
	// Nothing cut: the long sentence keeps its last word.
	long := TextScaled("DISCOVERY CHANNEL HD 12360 V 27500 DVB-S2", false, SizeAuto)
	if strings.Count(long.String(), "#") < 150 {
		t.Fatalf("long text looks cut:\n%s", long.String())
	}
	// A fixed normal size never draws large.
	n := TextScaled("CAM", false, SizeNormal)
	f, l := inkRows(n)
	if l-f+1 != 7 {
		t.Fatalf("normal size must be 7 rows high, got %d", l-f+1)
	}
	s := TextScaled("CAM", false, SizeSmall)
	f, l = inkRows(s)
	if l-f+1 != 5 {
		t.Fatalf("small size must be 5 rows high, got %d", l-f+1)
	}
}

func TestFromImage(t *testing.T) {
	t.Parallel()
	// A 200×100 white picture with a black square in its left half and a
	// transparent right quarter.
	img := image.NewNRGBA(image.Rect(0, 0, 200, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 200; x++ {
			switch {
			case x >= 150:
				img.Set(x, y, color.NRGBA{0, 0, 0, 0})
			case x >= 25 && x < 75 && y >= 25 && y < 75:
				img.Set(x, y, color.NRGBA{0, 0, 0, 255})
			default:
				img.Set(x, y, color.NRGBA{255, 255, 255, 255})
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	dec, err := LoadImage(&buf)
	if err != nil {
		t.Fatal(err)
	}
	b := FromImage(dec, ImageOptions{})
	// 200×100 fits 64×32: scale 0.32 → square 16×16 at x 8..23, y 8..23.
	if !b[16][16] || b[16][4] || b[2][16] || b[16][56] {
		t.Fatalf("square misplaced:\n%s", b.String())
	}
	inv := FromImage(dec, ImageOptions{Invert: true})
	if inv[16][16] || !inv[16][4] || inv[16][60] {
		t.Fatalf("invert wrong (transparent must stay empty):\n%s", inv.String())
	}
	m := FromImage(dec, ImageOptions{Margin: 4})
	if m[0][0] || m[31][63] {
		t.Fatal("margin must stay empty")
	}
	_ = FromImage(dec, ImageOptions{Dither: true})
	var both Bitmap
	both.Overlay(b)
	both.Overlay(TextScaled("X", false, SizeSmall))
	if !both[16][16] {
		t.Fatal("overlay lost the logo")
	}
}
