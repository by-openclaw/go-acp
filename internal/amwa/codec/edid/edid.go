package edid

import (
	"encoding/json"
	"fmt"
)

// ConstraintSet is one BCP-004-01 constraint set: capability URNs →
// parameter constraints. Same shape as is11.ConstraintSet, kept local
// so this codec stays dependency-free (ADR-0006).
type ConstraintSet map[string]json.RawMessage

// Rational is the grain_rate value shape (denominator omitted when 1,
// per the spec examples which emit {"numerator":60}).
type Rational struct {
	Numerator   int `json:"numerator"`
	Denominator int `json:"denominator,omitempty"`
}

// VideoMode is one decoded timing before it becomes a constraint set.
type VideoMode struct {
	Width     int
	Height    int
	Rate      Rational
	Interlace bool
	// Native marks a Preferred/Native timing (higher preference).
	Native bool
}

// capability URNs used by the mapping.
const (
	capFrameWidth    = "urn:x-nmos:cap:format:frame_width"
	capFrameHeight   = "urn:x-nmos:cap:format:frame_height"
	capGrainRate     = "urn:x-nmos:cap:format:grain_rate"
	capInterlace     = "urn:x-nmos:cap:format:interlace_mode"
	capColorSampling = "urn:x-nmos:cap:format:color_sampling"
	capComponent     = "urn:x-nmos:cap:format:component_depth"
	capMediaType     = "urn:x-nmos:cap:format:media_type"
	capChannelCount  = "urn:x-nmos:cap:format:channel_count"
	capSampleRate    = "urn:x-nmos:cap:format:sample_rate"
	capMetaPreference = "urn:x-nmos:cap:meta:preference"
)

// enumInts renders {"enum":[...]} for integer values.
func enumInts(vs ...int) json.RawMessage {
	b, _ := json.Marshal(map[string][]int{"enum": vs})
	return b
}

// enumStrs renders {"enum":[...]} for string values.
func enumStrs(vs ...string) json.RawMessage {
	b, _ := json.Marshal(map[string][]string{"enum": vs})
	return b
}

// enumRationals renders {"enum":[...]} for grain rates.
func enumRationals(vs ...Rational) json.RawMessage {
	b, _ := json.Marshal(map[string][]Rational{"enum": vs})
	return b
}

// rawInt renders a bare integer value (for meta:preference).
func rawInt(v int) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// toConstraintSet turns a VideoMode into a BCP-004-01 constraint set.
func (m VideoMode) toConstraintSet() ConstraintSet {
	mode := "progressive"
	if m.Interlace {
		mode = "interlaced_tff"
	}
	cs := ConstraintSet{
		capFrameWidth:  enumInts(m.Width),
		capFrameHeight: enumInts(m.Height),
		capInterlace:   enumStrs(mode),
		capGrainRate:   enumRationals(m.Rate),
	}
	return cs
}

// Result is the mapped capabilities: video + audio constraint sets.
type Result struct {
	Video []ConstraintSet
	Audio []ConstraintSet
}

// All returns video sets followed by audio sets — the flat list a
// Receiver's caps.constraint_sets carries.
func (r Result) All() []ConstraintSet {
	out := make([]ConstraintSet, 0, len(r.Video)+len(r.Audio))
	out = append(out, r.Video...)
	out = append(out, r.Audio...)
	return out
}

// Block sizes.
const (
	blockLen = 128
)

// Parse validates and decodes an E-EDID blob (one 128-byte base block,
// optionally followed by 128-byte extension blocks) into mapped
// Receiver Capabilities per BCP-005-01.
func Parse(raw []byte) (Result, error) {
	if len(raw) < blockLen {
		return Result{}, fmt.Errorf("edid: short blob (%d bytes, need at least %d)", len(raw), blockLen)
	}
	if len(raw)%blockLen != 0 {
		return Result{}, fmt.Errorf("edid: length %d is not a multiple of %d", len(raw), blockLen)
	}
	base := raw[:blockLen]
	if err := validateBlock(base, true); err != nil {
		return Result{}, err
	}

	var res Result
	video := []VideoMode{}

	// Established Timings I & II (bytes 0x23,0x24) + III (in a base
	// descriptor); E-EDID §3.8 / §3.10.3.9.
	video = append(video, establishedTimings(base[0x23], base[0x24])...)

	// Standard Timings: 8 entries of 2 bytes at 0x26..0x35; §3.9.
	for i := 0; i < 8; i++ {
		b0, b1 := base[0x26+i*2], base[0x26+i*2+1]
		if m, ok := standardTiming(b0, b1); ok {
			video = append(video, m)
		}
	}

	// Detailed Timing Descriptors: four 18-byte descriptors at
	// 0x36..0x7D; §3.10. The FIRST is the Preferred Timing Mode.
	for d := 0; d < 4; d++ {
		off := 0x36 + d*18
		desc := base[off : off+18]
		if m, ok := detailedTiming(desc); ok {
			m.Native = d == 0 // Preferred Timing Mode
			video = append(video, m)
		}
	}

	// Colour subsampling + component depth apply to EVERY video
	// constraint set (§3.6.4 base, overridden by CTA header below).
	sampling := baseColorSampling(base[0x18])
	depth := baseColorDepth(base[0x14])

	// CTA-861 extension blocks (tag 0x02); §7.
	numExt := int(base[0x7E])
	ctaSampling := []string(nil)
	for e := 1; e <= numExt && e*blockLen+blockLen <= len(raw)+0; e++ {
		start := e * blockLen
		if start+blockLen > len(raw) {
			break
		}
		ext := raw[start : start+blockLen]
		if len(ext) < blockLen || ext[0] != 0x02 {
			continue
		}
		if err := validateBlock(ext, false); err != nil {
			return Result{}, err
		}
		cta := parseCTA(ext)
		video = append(video, cta.video...)
		res.Audio = append(res.Audio, cta.audio...)
		if len(cta.sampling) > 0 {
			ctaSampling = cta.sampling
		}
	}
	// The CTA header colour subsampling supersedes the base block when
	// any CTA extension is present (spec "Color Subsampling").
	if ctaSampling != nil {
		sampling = ctaSampling
	}

	// Preference: native modes rank above non-native. The single
	// highest goes to the Preferred Timing Mode / first SVD.
	highest := -1
	for i := range video {
		if video[i].Native {
			highest = i
			break
		}
	}
	for i, m := range video {
		cs := m.toConstraintSet()
		if len(sampling) > 0 {
			cs[capColorSampling] = enumStrs(sampling...)
		}
		if depth > 0 {
			cs[capComponent] = enumInts(depth)
		}
		if m.Native {
			pref := 90
			if i == highest {
				pref = 100
			}
			cs[capMetaPreference] = rawInt(pref)
		}
		res.Video = append(res.Video, cs)
	}
	return res, nil
}

// validateBlock checks the 128-byte checksum (sum mod 256 == 0) and,
// for the base block, the fixed 00 FF..FF 00 header (§3.1).
func validateBlock(b []byte, isBase bool) error {
	if isBase {
		hdr := []byte{0x00, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x00}
		for i, h := range hdr {
			if b[i] != h {
				return fmt.Errorf("edid: bad base header at byte %d", i)
			}
		}
	}
	var sum byte
	for _, v := range b {
		sum += v
	}
	if sum != 0 {
		return fmt.Errorf("edid: block checksum non-zero (%d)", sum)
	}
	return nil
}
