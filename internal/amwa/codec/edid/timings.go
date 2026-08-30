package edid

// Video-mode decoders for the base EDID block (E-EDID §3.8–§3.10).

// etEntry is one Established Timings bit → mode.
type etEntry struct {
	bit       uint8
	w, h      int
	rate      int
	interlace bool
}

// Established Timings I (byte 0x23) and II (byte 0x24), E-EDID §3.8
// Table 3.16. Bit 7 is MSB.
var etByte1 = []etEntry{
	{7, 720, 400, 70, false},
	{6, 720, 400, 88, false},
	{5, 640, 480, 60, false},
	{4, 640, 480, 67, false},
	{3, 640, 480, 72, false},
	{2, 640, 480, 75, false},
	{1, 800, 600, 56, false},
	{0, 800, 600, 60, false},
}

var etByte2 = []etEntry{
	{7, 800, 600, 72, false},
	{6, 800, 600, 75, false},
	{5, 832, 624, 75, false},
	{4, 1024, 768, 87, true},
	{3, 1024, 768, 60, false},
	{2, 1024, 768, 70, false},
	{1, 1024, 768, 75, false},
	{0, 1280, 1024, 75, false},
}

// establishedTimings decodes the two Established Timings bytes. Modes
// appear in descending-bit order within each byte (byte1 then byte2),
// which matches the ordering the BCP-005-01 example expects.
func establishedTimings(b1, b2 byte) []VideoMode {
	var out []VideoMode
	emit := func(b byte, table []etEntry) {
		for _, e := range table {
			if b&(1<<e.bit) != 0 {
				out = append(out, VideoMode{
					Width: e.w, Height: e.h,
					Rate:      Rational{Numerator: e.rate},
					Interlace: e.interlace,
				})
			}
		}
	}
	emit(b1, etByte1)
	emit(b2, etByte2)
	return out
}

// standardTiming decodes one 2-byte Standard Timing (E-EDID §3.9).
// (0x01,0x01) is the "unused" marker. Returns ok=false for unused.
func standardTiming(b0, b1 byte) (VideoMode, bool) {
	if b0 == 0x01 && b1 == 0x01 {
		return VideoMode{}, false
	}
	if b0 == 0x00 {
		return VideoMode{}, false
	}
	w := (int(b0) + 31) * 8
	var h int
	switch b1 >> 6 { // image aspect ratio (EDID 1.4 codes)
	case 0b00: // 16:10
		h = w * 10 / 16
	case 0b01: // 4:3
		h = w * 3 / 4
	case 0b10: // 5:4
		h = w * 4 / 5
	case 0b11: // 16:9
		h = w * 9 / 16
	}
	rate := int(b1&0x3F) + 60
	return VideoMode{
		Width: w, Height: h,
		Rate: Rational{Numerator: rate},
	}, true
}

// detailedTiming decodes an 18-byte Detailed Timing Descriptor
// (E-EDID §3.10.2). A zero pixel clock (bytes 0-1) means the slot is
// a monitor descriptor, not a timing — ok=false.
func detailedTiming(d []byte) (VideoMode, bool) {
	if len(d) < 18 {
		return VideoMode{}, false
	}
	pclk := int(d[0]) | int(d[1])<<8 // ×10 kHz, little-endian
	if pclk == 0 {
		return VideoMode{}, false
	}
	hActive := int(d[2]) | (int(d[4]&0xF0) << 4)
	hBlank := int(d[3]) | (int(d[4]&0x0F) << 8)
	vActive := int(d[5]) | (int(d[7]&0xF0) << 4)
	vBlank := int(d[6]) | (int(d[7]&0x0F) << 8)
	interlace := d[17]&0x80 != 0

	height := vActive
	if interlace {
		height = vActive * 2
	}
	hTotal := hActive + hBlank
	vTotal := vActive + vBlank
	rate := Rational{Numerator: pclk * 10000}
	if hTotal > 0 && vTotal > 0 {
		rate.Denominator = hTotal * vTotal
	}
	return VideoMode{
		Width: hActive, Height: height,
		Rate: rate, Interlace: interlace,
	}, true
}

// baseColorSampling maps the base-block Feature Support byte (0x18)
// bits 4-3 (E-EDID §3.6.4) to color_sampling enum values. RGB 4:4:4
// is always supported.
func baseColorSampling(feature byte) []string {
	out := []string{"RGB"}
	switch (feature >> 3) & 0x03 {
	case 0b01: // RGB 4:4:4 & YCbCr 4:4:4
		out = append(out, "YCbCr-4:4:4")
	case 0b10: // RGB 4:4:4 & YCbCr 4:2:2
		out = append(out, "YCbCr-4:2:2")
	case 0b11: // RGB 4:4:4 & YCbCr 4:4:4 & YCbCr 4:2:2
		out = append(out, "YCbCr-4:4:4", "YCbCr-4:2:2")
	}
	return out
}

// baseColorDepth maps the Video Input Definition byte (0x14) Color
// Bit Depth field (bits 6-4) to component_depth (E-EDID §3.6.1). Only
// meaningful for a digital input (bit7 set); 0 = undefined.
func baseColorDepth(vid byte) int {
	if vid&0x80 == 0 {
		return 0 // analog input — no digital bit depth
	}
	switch (vid >> 4) & 0x07 {
	case 0b001:
		return 6
	case 0b010:
		return 8
	case 0b011:
		return 10
	case 0b100:
		return 12
	case 0b101:
		return 14
	case 0b110:
		return 16
	}
	return 0
}
