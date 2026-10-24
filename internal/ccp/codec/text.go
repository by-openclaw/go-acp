package codec

import "strings"

// Text sizes, largest first. A label is drawn at the first size where all
// of it fits — wrapped at spaces, never cut while a smaller size holds it.
//
//	size   font  cell   lines × characters (no frame / framed)
//	large  5×7   12×16  2 × 5  / 1 × 4
//	normal 5×7    6×8   4 × 10 / 3 × 10
//	small  3×5    4×6   5 × 16 / 4 × 15
const (
	SizeAuto   = 0
	SizeLarge  = 2
	SizeNormal = 1
	SizeSmall  = -1
)

type textSize struct {
	size      int
	cellW     int // advance per character
	cellH     int // advance per line
	glyphW    int // ink width of a glyph
	glyphH    int
	scale     int
	smallFont bool
}

var textSizes = []textSize{
	{SizeLarge, 12, 16, 10, 14, 2, false},
	{SizeNormal, 6, 8, 5, 7, 1, false},
	{SizeSmall, 4, 6, 3, 5, 1, true},
}

// TextScaled renders s (lines split on '|' or '\n') centred on the button.
// size picks one of SizeLarge, SizeNormal, SizeSmall; SizeAuto takes the
// largest at which every character fits. A fixed size that cannot hold
// the text steps down to the next. With frame, a 1-pixel border is drawn
// and the text keeps a 1-pixel gap inside it.
func TextScaled(s string, frame bool, size int) Bitmap {
	var b Bitmap
	areaW, areaH := BitmapW, BitmapH
	if frame {
		for x := 0; x < BitmapW; x++ {
			b.Set(x, 0)
			b.Set(x, BitmapH-1)
		}
		for y := 0; y < BitmapH; y++ {
			b.Set(0, y)
			b.Set(BitmapW-1, y)
		}
		areaW, areaH = BitmapW-4, BitmapH-4
	}
	raw := strings.FieldsFunc(s, func(r rune) bool { return r == '|' || r == '\n' })
	if len(raw) == 0 {
		return b
	}
	for i, ts := range textSizes {
		if size != SizeAuto && ts.size > size {
			continue
		}
		maxChars := (areaW + ts.cellW - ts.glyphW) / ts.cellW
		maxLines := (areaH + ts.cellH - ts.glyphH) / ts.cellH
		var lines []string
		for _, l := range raw {
			lines = append(lines, wrap(l, maxChars)...)
		}
		last := i == len(textSizes)-1
		if len(lines) > maxLines && !last {
			continue
		}
		if len(lines) > maxLines {
			lines = lines[:maxLines]
		}
		drawLines(&b, lines, ts)
		return b
	}
	return b
}

func drawLines(b *Bitmap, lines []string, ts textSize) {
	blockH := (len(lines)-1)*ts.cellH + ts.glyphH
	top := (BitmapH - blockH) / 2
	for i, l := range lines {
		w := (len(l)-1)*ts.cellW + ts.glyphW
		left := (BitmapW - w) / 2
		for j := 0; j < len(l); j++ {
			x, y := left+j*ts.cellW, top+i*ts.cellH
			if ts.smallFont {
				drawSmall(b, l[j], x, y)
			} else {
				drawGlyph(b, l[j], x, y, ts.scale)
			}
		}
	}
}

// drawSmall draws one 3×5 glyph; lower case is drawn as upper case.
func drawSmall(b *Bitmap, c byte, x0, y0 int) {
	if c >= 'a' && c <= 'z' {
		c -= 'a' - 'A'
	}
	if c < 0x20 || c > 0x5F {
		c = '?'
	}
	g := font3x5[c-0x20]
	for row := 0; row < 5; row++ {
		for col := 0; col < 3; col++ {
			if g[row]>>(2-col)&1 == 1 {
				b.Set(x0+col, y0+row)
			}
		}
	}
}

// font3x5 covers ASCII 0x20..0x5F: five rows of three bits, MSB left.
var font3x5 = [64][5]byte{
	{0, 0, 0, 0, 0}, {2, 2, 2, 0, 2}, {5, 5, 0, 0, 0}, {5, 7, 5, 7, 5}, // space ! " #
	{3, 6, 7, 3, 6}, {5, 1, 2, 4, 5}, {2, 5, 2, 5, 3}, {2, 2, 0, 0, 0}, // $ % & '
	{1, 2, 2, 2, 1}, {4, 2, 2, 2, 4}, {0, 5, 2, 5, 0}, {0, 2, 7, 2, 0}, // ( ) * +
	{0, 0, 0, 2, 4}, {0, 0, 7, 0, 0}, {0, 0, 0, 0, 2}, {1, 1, 2, 4, 4}, // , - . /
	{7, 5, 5, 5, 7}, {2, 6, 2, 2, 7}, {7, 1, 7, 4, 7}, {7, 1, 3, 1, 7}, // 0 1 2 3
	{5, 5, 7, 1, 1}, {7, 4, 7, 1, 7}, {7, 4, 7, 5, 7}, {7, 1, 1, 2, 2}, // 4 5 6 7
	{7, 5, 7, 5, 7}, {7, 5, 7, 1, 7}, {0, 2, 0, 2, 0}, {0, 2, 0, 2, 4}, // 8 9 : ;
	{1, 2, 4, 2, 1}, {0, 7, 0, 7, 0}, {4, 2, 1, 2, 4}, {7, 1, 3, 0, 2}, // < = > ?
	{7, 5, 7, 4, 7}, {2, 5, 7, 5, 5}, {6, 5, 6, 5, 6}, {3, 4, 4, 4, 3}, // @ A B C
	{6, 5, 5, 5, 6}, {7, 4, 6, 4, 7}, {7, 4, 6, 4, 4}, {3, 4, 5, 5, 3}, // D E F G
	{5, 5, 7, 5, 5}, {7, 2, 2, 2, 7}, {1, 1, 1, 5, 2}, {5, 5, 6, 5, 5}, // H I J K
	{4, 4, 4, 4, 7}, {5, 7, 7, 5, 5}, {6, 5, 5, 5, 5}, {2, 5, 5, 5, 2}, // L M N O
	{6, 5, 6, 4, 4}, {2, 5, 5, 6, 3}, {6, 5, 6, 5, 5}, {3, 4, 2, 1, 6}, // P Q R S
	{7, 2, 2, 2, 2}, {5, 5, 5, 5, 7}, {5, 5, 5, 5, 2}, {5, 5, 7, 7, 5}, // T U V W
	{5, 5, 2, 5, 5}, {5, 5, 2, 2, 2}, {7, 1, 2, 4, 7}, {6, 4, 4, 4, 6}, // X Y Z [
	{4, 4, 2, 1, 1}, {3, 1, 1, 1, 3}, {2, 5, 0, 0, 0}, {0, 0, 0, 0, 7}, // \ ] ^ _
}
