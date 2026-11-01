package codec

import (
	"fmt"
	"image"
	_ "image/gif"  // decoders for LoadImage
	_ "image/jpeg" //
	_ "image/png"  //
	"io"
)

// ImageOptions controls how a picture becomes a 1-bit button bitmap.
type ImageOptions struct {
	// Dither diffuses the grey levels (Floyd–Steinberg) instead of a hard
	// threshold: better for photos, worse for flat logos.
	Dither bool
	// Threshold is the luminance (0..255) below which a pixel is drawn
	// (dark ink on the lit LCD). 0 means 128.
	Threshold uint8
	// Invert draws the light parts instead of the dark ones.
	Invert bool
	// Margin keeps this many pixels empty around the picture.
	Margin int
}

// LoadImage decodes a PNG, JPEG or GIF.
func LoadImage(r io.Reader) (image.Image, error) {
	img, _, err := image.Decode(r)
	if err != nil {
		return nil, fmt.Errorf("ccp: image: %w", err)
	}
	return img, nil
}

// FromImage scales img to fit 64×32 (minus the margin) keeping its
// proportions, centres it, and turns it into ink: luminance below the
// threshold is drawn, transparent pixels never are. Each target pixel
// averages the source pixels it covers, so a large logo shrinks cleanly.
func FromImage(img image.Image, o ImageOptions) Bitmap {
	var b Bitmap
	th := float64(o.Threshold)
	if th == 0 {
		th = 128
	}
	r := img.Bounds()
	sw, sh := r.Dx(), r.Dy()
	if sw == 0 || sh == 0 {
		return b
	}
	aw, ah := BitmapW-2*o.Margin, BitmapH-2*o.Margin
	if aw < 1 || ah < 1 {
		return b
	}
	// Fit: the largest w×h with the source aspect ratio inside aw×ah.
	w, h := aw, sh*aw/sw
	if h > ah {
		h, w = ah, sw*ah/sh
	}
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	x0, y0 := (BitmapW-w)/2, (BitmapH-h)/2

	lum := make([][]float64, h) // grey 0..255, -1 = transparent
	for y := 0; y < h; y++ {
		lum[y] = make([]float64, w)
		sy0, sy1 := r.Min.Y+y*sh/h, r.Min.Y+(y+1)*sh/h
		if sy1 == sy0 {
			sy1++
		}
		for x := 0; x < w; x++ {
			sx0, sx1 := r.Min.X+x*sw/w, r.Min.X+(x+1)*sw/w
			if sx1 == sx0 {
				sx1++
			}
			var sum, alpha, n float64
			for sy := sy0; sy < sy1; sy++ {
				for sx := sx0; sx < sx1; sx++ {
					cr, cg, cb, ca := img.At(sx, sy).RGBA()
					a := float64(ca) / 0xFFFF
					if a > 0 {
						// un-premultiply, then Rec. 601 luminance
						l := (0.299*float64(cr) + 0.587*float64(cg) + 0.114*float64(cb)) / a / 257
						sum += l * a
					}
					alpha += a
					n++
				}
			}
			if alpha/n < 0.5 {
				lum[y][x] = -1
				continue
			}
			lum[y][x] = sum / alpha
			if o.Invert {
				lum[y][x] = 255 - lum[y][x]
			}
		}
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := lum[y][x]
			if v < 0 {
				continue
			}
			ink := v < th
			if o.Dither {
				target := 255.0
				if ink {
					target = 0
				}
				e := v - target
				spread := func(dx, dy int, f float64) {
					xx, yy := x+dx, y+dy
					if xx >= 0 && xx < w && yy < h && lum[yy][xx] >= 0 {
						lum[yy][xx] += e * f
					}
				}
				spread(1, 0, 7.0/16)
				spread(-1, 1, 3.0/16)
				spread(0, 1, 5.0/16)
				spread(1, 1, 1.0/16)
			}
			if ink {
				b.Set(x0+x, y0+y)
			}
		}
	}
	return b
}

// Overlay draws every pixel of src on top of b (logo + text on one key).
func (b *Bitmap) Overlay(src Bitmap) {
	for y := 0; y < BitmapH; y++ {
		for x := 0; x < BitmapW; x++ {
			if src[y][x] {
				b[y][x] = true
			}
		}
	}
}
