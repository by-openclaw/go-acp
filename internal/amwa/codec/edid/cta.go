package edid

// CTA-861 extension block parsing (CTA-861 §7): Short Video
// Descriptors, Short Audio Descriptors, basic-audio default, and the
// colour-subsampling header bits.

type ctaResult struct {
	video    []VideoMode
	audio    []ConstraintSet
	sampling []string
}

// vicMode maps a CTA-861 Video Identification Code to its timing.
// Subset of CTA-861 Table 3 covering the common broadcast modes; an
// unknown VIC is skipped (its mode is not asserted rather than
// guessed).
var vicMode = map[int]VideoMode{
	1:  {Width: 640, Height: 480, Rate: Rational{Numerator: 60}, Interlace: false},
	3:  {Width: 720, Height: 480, Rate: Rational{Numerator: 60}, Interlace: false},
	4:  {Width: 1280, Height: 720, Rate: Rational{Numerator: 60}, Interlace: false},
	5:  {Width: 1920, Height: 1080, Rate: Rational{Numerator: 60}, Interlace: true},
	16: {Width: 1920, Height: 1080, Rate: Rational{Numerator: 60}, Interlace: false},
	18: {Width: 720, Height: 576, Rate: Rational{Numerator: 50}, Interlace: false},
	19: {Width: 1280, Height: 720, Rate: Rational{Numerator: 50}, Interlace: false},
	20: {Width: 1920, Height: 1080, Rate: Rational{Numerator: 50}, Interlace: true},
	31: {Width: 1920, Height: 1080, Rate: Rational{Numerator: 50}, Interlace: false},
	32: {Width: 1920, Height: 1080, Rate: Rational{Numerator: 24}, Interlace: false},
	33: {Width: 1920, Height: 1080, Rate: Rational{Numerator: 25}, Interlace: false},
	34: {Width: 1920, Height: 1080, Rate: Rational{Numerator: 30}, Interlace: false},
	93: {Width: 3840, Height: 2160, Rate: Rational{Numerator: 24}, Interlace: false},
	94: {Width: 3840, Height: 2160, Rate: Rational{Numerator: 25}, Interlace: false},
	95: {Width: 3840, Height: 2160, Rate: Rational{Numerator: 30}, Interlace: false},
	96: {Width: 3840, Height: 2160, Rate: Rational{Numerator: 50}, Interlace: false},
	97: {Width: 3840, Height: 2160, Rate: Rational{Numerator: 60}, Interlace: false},
}

// audioSampleRates maps Short Audio Descriptor byte-1 bits to rates.
var audioSampleRates = []struct {
	bit  uint8
	rate int
}{
	{0, 32000}, {1, 44100}, {2, 48000},
	{3, 88200}, {4, 96000}, {5, 176400}, {6, 192000},
}

// parseCTA decodes one CTA-861 extension block.
func parseCTA(ext []byte) ctaResult {
	var res ctaResult

	// Header (CTA-861 §7.5): byte3 flags.
	flags := ext[3]
	basicAudio := flags&0x40 != 0
	ycbcr444 := flags&0x20 != 0
	ycbcr422 := flags&0x10 != 0
	res.sampling = []string{"RGB"}
	if ycbcr444 {
		res.sampling = append(res.sampling, "YCbCr-4:4:4")
	}
	if ycbcr422 {
		res.sampling = append(res.sampling, "YCbCr-4:2:2")
	}

	// Data Block Collection runs from byte 4 to byte d-1 (d = ext[2],
	// the offset of the first DTD; 0 or 4 means no DBC / no DTDs).
	d := int(ext[2])
	end := d
	if d == 0 || d > blockLen {
		end = blockLen
	}
	i := 4
	for i < end && i < blockLen {
		tag := ext[i] >> 5
		length := int(ext[i] & 0x1F)
		payload := ext[i+1 : min(i+1+length, blockLen)]
		switch tag {
		case 2: // Video Data Block — one SVD per byte
			for _, svd := range payload {
				vic := int(svd & 0x7F)
				native := svd&0x80 != 0
				if m, ok := vicMode[vic]; ok {
					m.Native = native
					res.video = append(res.video, m)
				}
			}
		case 1: // Audio Data Block — 3 bytes per SAD
			for j := 0; j+2 < len(payload)+1 && j+3 <= len(payload); j += 3 {
				res.audio = append(res.audio, shortAudioDescriptor(payload[j:j+3]))
			}
		}
		i += 1 + length
	}

	// Basic-audio default (spec "Audio Receivers"): if the header
	// basic-audio bit is set but no SADs were present, a default
	// stereo L8 capability MUST exist.
	if basicAudio && len(res.audio) == 0 {
		res.audio = append(res.audio, ConstraintSet{
			capMediaType:    enumStrs("audio/L8"),
			capChannelCount: enumInts(2),
		})
	}
	return res
}

// shortAudioDescriptor decodes one 3-byte SAD (CTA-861 §7.5.2).
func shortAudioDescriptor(sad []byte) ConstraintSet {
	format := (sad[0] >> 3) & 0x0F
	channels := int(sad[0]&0x07) + 1

	var rates []int
	for _, sr := range audioSampleRates {
		if sad[1]&(1<<sr.bit) != 0 {
			rates = append(rates, sr.rate)
		}
	}

	cs := ConstraintSet{capChannelCount: enumInts(channels)}
	if len(rates) > 0 {
		cs[capSampleRate] = enumRateFractions(rates)
	}

	// Media type: format 1 = LPCM, whose bit depths come from byte 2
	// (bit0 16-bit, bit1 20-bit, bit2 24-bit). Other format codes are
	// compressed; expose the CTA short name where unambiguous.
	if format == 1 {
		var mts []string
		if sad[2]&0x01 != 0 {
			mts = append(mts, "audio/L16")
		}
		if sad[2]&0x04 != 0 {
			mts = append(mts, "audio/L24")
		}
		if len(mts) == 0 {
			mts = []string{"audio/L16"}
		}
		cs[capMediaType] = enumStrs(mts...)
	}
	return cs
}

// enumRateFractions renders sample rates as grain-rate-style rationals
// ({"numerator":48000}) inside an enum — the sample_rate constraint
// uses the same rational shape.
func enumRateFractions(rates []int) []byte {
	rs := make([]Rational, len(rates))
	for i, r := range rates {
		rs[i] = Rational{Numerator: r}
	}
	return enumRationals(rs...)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
