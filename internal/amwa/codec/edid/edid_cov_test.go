package edid_test

// Coverage pass over the byte-level decoders Examples.md does not
// enumerate: the Standard Timing aspect-ratio codes, the Feature
// Support colour-subsampling codes, the Video Input Definition depth
// codes, and the blob-shape refusals.

import (
	"strings"
	"testing"

	"dhs/internal/amwa/codec/edid"
)

// widthOf reads a constraint set's frame_width enum back as a string
// so the aspect-ratio table can compare without a JSON round-trip.
func widthHeight(t *testing.T, cs edid.ConstraintSet) (string, string) {
	t.Helper()
	return enumOf(t, cs, "urn:x-nmos:cap:format:frame_width"),
		enumOf(t, cs, "urn:x-nmos:cap:format:frame_height")
}

// standardBlock puts one Standard Timing in the first slot and leaves
// the rest unused.
func standardBlock(b0, b1 byte) []byte {
	return baseBlock(map[int]byte{0x26: b0, 0x27: b1})
}

// E-EDID §3.9 encodes the aspect ratio in the top two bits of the
// second byte; the height follows from it, and the refresh rate is
// the low six bits plus 60.
func TestStandardTimingAspectRatios(t *testing.T) {
	// (0x71 + 31) * 8 = 1152 across every case; the height follows
	// from the aspect code, in integer arithmetic (5:4 lands on 921).
	for name, tc := range map[string]struct {
		b1            byte
		width, height string
		rate          string
	}{
		"16:10":         {0x00, "1152", "720", `{"enum":[{"numerator":60}]}`},
		"4:3":           {0x40, "1152", "864", `{"enum":[{"numerator":60}]}`},
		"5:4":           {0x80, "1152", "921", `{"enum":[{"numerator":60}]}`},
		"16:9":          {0xC0, "1152", "648", `{"enum":[{"numerator":60}]}`},
		"16:9 at 75 Hz": {0xCF, "1152", "648", `{"enum":[{"numerator":75}]}`},
	} {
		t.Run(name, func(t *testing.T) {
			res, err := edid.Parse(standardBlock(0x71, tc.b1))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if len(res.Video) != 1 {
				t.Fatalf("video sets = %d, want the one standard timing", len(res.Video))
			}
			w, h := widthHeight(t, res.Video[0])
			if w != `{"enum":[`+tc.width+`]}` || h != `{"enum":[`+tc.height+`]}` {
				t.Errorf("mode = %s x %s, want %s x %s", w, h, tc.width, tc.height)
			}
			if got := enumOf(t, res.Video[0], "urn:x-nmos:cap:format:grain_rate"); got != tc.rate {
				t.Errorf("rate = %s, want %s", got, tc.rate)
			}
		})
	}

	// The unused marker and a zero horizontal byte both mean "no mode
	// here" rather than a 248-pixel-wide one.
	for name, tc := range map[string][2]byte{
		"the unused marker": {0x01, 0x01},
		"a zero width byte": {0x00, 0x40},
	} {
		t.Run(name, func(t *testing.T) {
			res, err := edid.Parse(standardBlock(tc[0], tc[1]))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if len(res.Video) != 0 {
				t.Errorf("video sets = %+v, want none", res.Video)
			}
		})
	}
}

// The Feature Support byte says which colour subsamplings the sink
// accepts beyond RGB 4:4:4, which is always in.
func TestBaseColourSampling(t *testing.T) {
	for name, tc := range map[string]struct {
		feature byte
		want    string
	}{
		"RGB only":            {0x00, `{"enum":["RGB"]}`},
		"RGB and YCbCr 4:4:4": {0x08, `{"enum":["RGB","YCbCr-4:4:4"]}`},
		"RGB and YCbCr 4:2:2": {0x10, `{"enum":["RGB","YCbCr-4:2:2"]}`},
		"RGB and both YCbCr":  {0x18, `{"enum":["RGB","YCbCr-4:4:4","YCbCr-4:2:2"]}`},
	} {
		t.Run(name, func(t *testing.T) {
			// One established timing so there is a set to carry it.
			b := baseBlock(map[int]byte{0x23: 0x20, 0x18: tc.feature})
			res, err := edid.Parse(b)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if len(res.Video) != 1 {
				t.Fatalf("video sets = %d", len(res.Video))
			}
			if got := enumOf(t, res.Video[0], "urn:x-nmos:cap:format:color_sampling"); got != tc.want {
				t.Errorf("color_sampling = %s, want %s", got, tc.want)
			}
		})
	}
}

// The Video Input Definition byte carries the bit depth, but only for
// a digital input — an analog sink states none, and so does a code
// the spec leaves undefined.
func TestBaseColourDepth(t *testing.T) {
	for name, tc := range map[string]struct {
		vid   byte
		depth string // "" = no component_depth constraint at all
	}{
		"analog input":       {0x00, ""},
		"digital, undefined": {0x80, ""},
		"digital 6-bit":      {0x90, `{"enum":[6]}`},
		"digital 8-bit":      {0xA0, `{"enum":[8]}`},
		"digital 10-bit":     {0xB0, `{"enum":[10]}`},
		"digital 12-bit":     {0xC0, `{"enum":[12]}`},
		"digital 14-bit":     {0xD0, `{"enum":[14]}`},
		"digital 16-bit":     {0xE0, `{"enum":[16]}`},
		"digital reserved":   {0xF0, ""},
	} {
		t.Run(name, func(t *testing.T) {
			b := baseBlock(map[int]byte{0x23: 0x20, 0x14: tc.vid})
			res, err := edid.Parse(b)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if len(res.Video) != 1 {
				t.Fatalf("video sets = %d", len(res.Video))
			}
			raw, has := res.Video[0]["urn:x-nmos:cap:format:component_depth"]
			if tc.depth == "" {
				if has {
					t.Errorf("component_depth = %s, want none", raw)
				}
				return
			}
			if !has || string(raw) != tc.depth {
				t.Errorf("component_depth = %s, want %s", raw, tc.depth)
			}
		})
	}
}

// A blob the parser cannot trust is refused rather than half-decoded:
// too short, not a whole number of blocks, an extension whose
// checksum does not hold, or an extension count that runs past the
// bytes we were given.
func TestParseRefusesMalformedBlobs(t *testing.T) {
	for name, tc := range map[string]struct {
		raw  []byte
		want string
	}{
		"shorter than one block": {make([]byte, 64), "short blob"},
		"not a whole number of blocks": {
			append(baseBlock(nil), 0x00), "not a multiple",
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := edid.Parse(tc.raw); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("= %v, want an error mentioning %q", err, tc.want)
			}
		})
	}

	// An extension block whose checksum does not hold fails the whole
	// parse — a sink's capabilities are not partly trustworthy.
	base := baseBlock(map[int]byte{0x7E: 0x01})
	ext := make([]byte, 128)
	ext[0], ext[1], ext[3] = 0x02, 0x03, 0x40
	ext[127] = 0x01 // deliberately wrong
	if _, err := edid.Parse(append(base, ext...)); err == nil ||
		!strings.Contains(err.Error(), "checksum") {
		t.Error("an extension with a bad checksum must fail the parse")
	}

	// An extension that is not CTA-861 is skipped, not refused — a
	// sink may carry blocks we do not map.
	other := make([]byte, 128)
	other[0] = 0x40 // DisplayID extension tag
	var sum byte
	for _, v := range other[:127] {
		sum += v
	}
	other[127] = byte(256 - int(sum))
	res, err := edid.Parse(append(baseBlock(map[int]byte{0x23: 0x20, 0x7E: 0x01}), other...))
	if err != nil {
		t.Fatalf("an unmapped extension must be skipped: %v", err)
	}
	if len(res.Video) != 1 {
		t.Errorf("video sets = %d, want the base block's own", len(res.Video))
	}

	// An extension COUNT larger than the bytes present stops at what
	// is there rather than reading past the blob.
	res, err = edid.Parse(baseBlock(map[int]byte{0x23: 0x20, 0x7E: 0x04}))
	if err != nil {
		t.Fatalf("an over-stated extension count must be tolerated: %v", err)
	}
	if len(res.Video) != 1 {
		t.Errorf("video sets = %d", len(res.Video))
	}
}

// A Receiver's caps.constraint_sets is one flat list: video sets
// first, then audio.
func TestResultAllFlattensVideoThenAudio(t *testing.T) {
	base := baseBlock(map[int]byte{0x23: 0x20, 0x7E: 0x01})
	ext := make([]byte, 128)
	// A CTA-861 extension with basic audio and one Short Audio
	// Descriptor (LPCM, 2 channels, 48 kHz, 16-bit).
	ext[0], ext[1], ext[2], ext[3] = 0x02, 0x03, 0x07, 0x40
	ext[4] = 0x23 // audio data block, 3 bytes
	ext[5], ext[6], ext[7] = 0x09, 0x04, 0x01
	var sum byte
	for _, v := range ext[:127] {
		sum += v
	}
	ext[127] = byte(256 - int(sum))

	res, err := edid.Parse(append(base, ext...))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	all := res.All()
	if len(all) != len(res.Video)+len(res.Audio) {
		t.Fatalf("All = %d sets, want %d video + %d audio",
			len(all), len(res.Video), len(res.Audio))
	}
	if len(res.Audio) == 0 {
		t.Fatal("the Short Audio Descriptor must produce an audio set")
	}
	// Video first: the last entries are the audio ones.
	for i := range res.Video {
		if _, isVideo := all[i]["urn:x-nmos:cap:format:frame_width"]; !isVideo {
			t.Errorf("All[%d] is not a video set: %v", i, all[i])
		}
	}
}

// An interlaced Detailed Timing reports the full frame height (the
// descriptor counts one field) and says so in interlace_mode — a
// receiver that reads it as progressive would take half the picture.
func TestDetailedTimingInterlaced(t *testing.T) {
	dtd := make([]byte, 18)
	dtd[0], dtd[1] = 0x02, 0x3A // pixel clock, ×10 kHz
	dtd[2], dtd[3] = 0x80, 0xC0 // hactive 0x780, hblank 0x0C0
	dtd[4] = 0x70
	dtd[5], dtd[6] = 0x1A, 0x2D // vactive 0x21A (one field), vblank 0x02D
	dtd[7] = 0x20
	dtd[17] = 0x80 // interlaced

	mut := map[int]byte{}
	for i, v := range dtd {
		mut[0x36+i] = v
	}
	res, err := edid.Parse(baseBlock(mut))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(res.Video) != 1 {
		t.Fatalf("video sets = %d, want the one detailed timing", len(res.Video))
	}
	if got := enumOf(t, res.Video[0], "urn:x-nmos:cap:format:interlace_mode"); got != `{"enum":["interlaced_tff"]}` {
		t.Errorf("interlace_mode = %s", got)
	}
	// 0x21A fields = 538; the frame is twice that.
	if got := enumOf(t, res.Video[0], "urn:x-nmos:cap:format:frame_height"); got != `{"enum":[1076]}` {
		t.Errorf("frame_height = %s, want the full frame", got)
	}
}

// ctaBlock builds a checksum-valid CTA-861 extension with the given
// header byte 3 and data-block payload.
func ctaBlock(header byte, payload []byte) []byte {
	ext := make([]byte, 128)
	ext[0], ext[1], ext[3] = 0x02, 0x03, header
	ext[2] = byte(4 + len(payload)) // DTD start offset
	copy(ext[4:], payload)
	var sum byte
	for _, v := range ext[:127] {
		sum += v
	}
	ext[127] = byte(256 - int(sum))
	return ext
}

// The CTA header's colour-subsampling bits supersede the base
// block's, and an LPCM descriptor that declares no bit depth still
// names a media type rather than none.
func TestCTAHeaderSamplingAndLPCMFallback(t *testing.T) {
	base := baseBlock(map[int]byte{0x23: 0x20, 0x7E: 0x01, 0x18: 0x00})
	// Header: basic audio + YCbCr 4:2:2 only. Payload: one audio data
	// block holding an LPCM SAD with no bit-depth bits set.
	ext := ctaBlock(0x50, []byte{0x23, 0x09, 0x04, 0x00})

	res, err := edid.Parse(append(base, ext...))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(res.Video) == 0 {
		t.Fatal("the base block's timing must survive")
	}
	if got := enumOf(t, res.Video[0], "urn:x-nmos:cap:format:color_sampling"); got != `{"enum":["RGB","YCbCr-4:2:2"]}` {
		t.Errorf("color_sampling = %s, want the CTA header's own", got)
	}
	if len(res.Audio) != 1 {
		t.Fatalf("audio sets = %d, want the one descriptor", len(res.Audio))
	}
	if got := enumOf(t, res.Audio[0], "urn:x-nmos:cap:format:media_type"); got != `{"enum":["audio/L16"]}` {
		t.Errorf("media_type = %s, want the LPCM fallback", got)
	}
}
