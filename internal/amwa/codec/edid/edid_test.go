package edid_test

// Tests for the BCP-005-01 EDID→caps mapping. The AMWA tool ships no
// automated suite (BCP0050101Test is a stub), so the oracle is the
// spec's Examples.md byte vectors, plus E-EDID / CTA-861 byte layouts
// for the descriptors Examples.md does not enumerate.

import (
	"encoding/json"
	"testing"

	"dhs/internal/amwa/codec/edid"
)

// baseBlock builds a checksum-valid 128-byte base EDID with the given
// mutations applied by offset.
func baseBlock(mut map[int]byte) []byte {
	b := make([]byte, 128)
	copy(b, []byte{0x00, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x00})
	// Standard-timing slots default to the "unused" marker.
	for i := 0; i < 8; i++ {
		b[0x26+i*2] = 0x01
		b[0x26+i*2+1] = 0x01
	}
	for off, v := range mut {
		b[off] = v
	}
	var sum byte
	for _, v := range b[:127] {
		sum += v
	}
	b[127] = byte(256 - int(sum))
	return b
}

// csHas checks one constraint URN's enum contains the wanted JSON.
func enumOf(t *testing.T, cs edid.ConstraintSet, urn string) string {
	t.Helper()
	raw, ok := cs[urn]
	if !ok {
		t.Fatalf("constraint set missing %s: %v", urn, cs)
	}
	return string(raw)
}

func TestEstablishedTimingsExample(t *testing.T) {
	// Examples.md "Established Timings": bytes 20 08 00 -> 640x480@60
	// and 1024x768@60.
	b := baseBlock(map[int]byte{0x23: 0x20, 0x24: 0x08, 0x25: 0x00})
	res, err := edid.Parse(b)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(res.Video) != 2 {
		t.Fatalf("video sets = %d, want 2: %+v", len(res.Video), res.Video)
	}
	want := []struct{ w, h int }{{640, 480}, {1024, 768}}
	for i, wexp := range want {
		cs := res.Video[i]
		if got := enumOf(t, cs, "urn:x-nmos:cap:format:frame_width"); got != `{"enum":[`+itoa(wexp.w)+`]}` {
			t.Errorf("set %d width = %s, want %d", i, got, wexp.w)
		}
		if got := enumOf(t, cs, "urn:x-nmos:cap:format:frame_height"); got != `{"enum":[`+itoa(wexp.h)+`]}` {
			t.Errorf("set %d height = %s, want %d", i, got, wexp.h)
		}
		if got := enumOf(t, cs, "urn:x-nmos:cap:format:interlace_mode"); got != `{"enum":["progressive"]}` {
			t.Errorf("set %d interlace = %s", i, got)
		}
		if got := enumOf(t, cs, "urn:x-nmos:cap:format:grain_rate"); got != `{"enum":[{"numerator":60}]}` {
			t.Errorf("set %d grain_rate = %s", i, got)
		}
	}
}

func TestStandardTimingDecode(t *testing.T) {
	// Examples.md "Standard Timings" uses D1 C0. Byte0 0xD1 -> hactive
	// (209+31)*8 = 1920; byte1 0xC0 -> aspect 16:9, refresh 60 ->
	// 1920x1080@60. (The published example transposes W/H to
	// 1080/1920; "could contain" marks it illustrative, so the
	// normative E-EDID §3.9 arithmetic is asserted here.)
	b := baseBlock(map[int]byte{0x26: 0xD1, 0x27: 0xC0})
	res, err := edid.Parse(b)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(res.Video) != 1 {
		t.Fatalf("video sets = %d, want 1", len(res.Video))
	}
	cs := res.Video[0]
	if got := enumOf(t, cs, "urn:x-nmos:cap:format:frame_width"); got != `{"enum":[1920]}` {
		t.Errorf("width = %s, want 1920", got)
	}
	if got := enumOf(t, cs, "urn:x-nmos:cap:format:frame_height"); got != `{"enum":[1080]}` {
		t.Errorf("height = %s, want 1080", got)
	}
	if got := enumOf(t, cs, "urn:x-nmos:cap:format:grain_rate"); got != `{"enum":[{"numerator":60}]}` {
		t.Errorf("grain_rate = %s", got)
	}
}

func TestDetailedTimingAndColor(t *testing.T) {
	// A 1920x1080p DTD: pclk 148.5MHz = 14850 (×10kHz) = 0x3A02 LE;
	// hactive 1920 (0x780), hblank 280 (0x118); vactive 1080 (0x438),
	// vblank 45 (0x2D); progressive.
	dtd := make([]byte, 18)
	dtd[0], dtd[1] = 0x02, 0x3A // 14850
	dtd[2] = 0x80               // hactive low = 0x80
	dtd[3] = 0x18               // hblank low = 0x18
	dtd[4] = 0x71               // hactive hi 0x7, hblank hi 0x1 -> 0x780 / 0x118
	dtd[5] = 0x38               // vactive low
	dtd[6] = 0x2D               // vblank low
	dtd[7] = 0x40               // vactive hi 0x4, vblank hi 0x0 -> 0x438 / 0x02D
	dtd[17] = 0x00              // progressive
	mut := map[int]byte{
		0x14: 0xA0, // digital input, 8-bit depth (010)
		0x18: 0x18, // feature: YCbCr 4:4:4 & 4:2:2 (bits4-3 = 11)
	}
	for i, v := range dtd {
		mut[0x36+i] = v
	}
	b := baseBlock(mut)
	res, err := edid.Parse(b)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(res.Video) != 1 {
		t.Fatalf("video sets = %d, want 1: %+v", len(res.Video), res.Video)
	}
	cs := res.Video[0]
	if got := enumOf(t, cs, "urn:x-nmos:cap:format:frame_width"); got != `{"enum":[1920]}` {
		t.Errorf("DTD width = %s", got)
	}
	if got := enumOf(t, cs, "urn:x-nmos:cap:format:frame_height"); got != `{"enum":[1080]}` {
		t.Errorf("DTD height = %s", got)
	}
	// component_depth 8 and color_sampling RGB+YCbCr on every set.
	if got := enumOf(t, cs, "urn:x-nmos:cap:format:component_depth"); got != `{"enum":[8]}` {
		t.Errorf("component_depth = %s", got)
	}
	var samp struct{ Enum []string }
	_ = json.Unmarshal([]byte(enumOf(t, cs, "urn:x-nmos:cap:format:color_sampling")), &samp)
	if len(samp.Enum) != 3 || samp.Enum[0] != "RGB" {
		t.Errorf("color_sampling = %v, want RGB+YCbCr 4:4:4/4:2:2", samp.Enum)
	}
	// Preferred Timing Mode gets the top preference.
	if got := enumOf(t, cs, "urn:x-nmos:cap:meta:preference"); got != "100" {
		t.Errorf("preferred DTD preference = %s, want 100", got)
	}
}

func TestCTAExtensionAudioAndVideo(t *testing.T) {
	base := baseBlock(map[int]byte{0x7E: 0x01}) // one extension follows

	ext := make([]byte, 128)
	ext[0] = 0x02 // CTA extension tag
	ext[1] = 0x03 // revision 3
	ext[3] = 0x60 // basic audio + YCbCr444
	// Data block collection at byte 4:
	// Video Data Block (tag 2), length 1: VIC 16 (1920x1080p60).
	ext[4] = (2 << 5) | 1
	ext[5] = 16
	// Audio Data Block (tag 1), length 3: LPCM, 2ch, 48k, 16+24-bit.
	ext[6] = (1 << 5) | 3
	ext[7] = (1 << 3) | 0x01 // format 1 (LPCM), channels-1 = 1 -> 2ch
	ext[8] = 0x04            // sample rate bit2 -> 48000
	ext[9] = 0x05            // LPCM depths: bit0 16-bit + bit2 24-bit
	ext[2] = 0x00           // no DTDs in this extension
	var sum byte
	for _, v := range ext[:127] {
		sum += v
	}
	ext[127] = byte(256 - int(sum))

	res, err := edid.Parse(append(base, ext...))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// VIC 16 video set present.
	foundVideo := false
	for _, cs := range res.Video {
		if enumOf(t, cs, "urn:x-nmos:cap:format:frame_width") == `{"enum":[1920]}` {
			foundVideo = true
		}
	}
	if !foundVideo {
		t.Errorf("VIC 16 video set missing: %+v", res.Video)
	}
	// One audio SAD -> L16+L24, 2ch, 48k.
	if len(res.Audio) != 1 {
		t.Fatalf("audio sets = %d, want 1: %+v", len(res.Audio), res.Audio)
	}
	a := res.Audio[0]
	if got := enumOf(t, a, "urn:x-nmos:cap:format:channel_count"); got != `{"enum":[2]}` {
		t.Errorf("channel_count = %s", got)
	}
	var mt struct{ Enum []string }
	_ = json.Unmarshal([]byte(enumOf(t, a, "urn:x-nmos:cap:format:media_type")), &mt)
	if len(mt.Enum) != 2 || mt.Enum[0] != "audio/L16" || mt.Enum[1] != "audio/L24" {
		t.Errorf("media_type = %v, want [audio/L16 audio/L24]", mt.Enum)
	}
	if got := enumOf(t, a, "urn:x-nmos:cap:format:sample_rate"); got != `{"enum":[{"numerator":48000}]}` {
		t.Errorf("sample_rate = %s", got)
	}
}

func TestBasicAudioDefault(t *testing.T) {
	base := baseBlock(map[int]byte{0x7E: 0x01})
	ext := make([]byte, 128)
	ext[0], ext[1], ext[3] = 0x02, 0x03, 0x40 // basic audio, no descriptors
	ext[2] = 0x00
	var sum byte
	for _, v := range ext[:127] {
		sum += v
	}
	ext[127] = byte(256 - int(sum))
	res, err := edid.Parse(append(base, ext...))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(res.Audio) != 1 {
		t.Fatalf("audio sets = %d, want the L8 stereo default", len(res.Audio))
	}
	if got := enumOf(t, res.Audio[0], "urn:x-nmos:cap:format:media_type"); got != `{"enum":["audio/L8"]}` {
		t.Errorf("default media_type = %s, want audio/L8", got)
	}
}

func TestParseValidation(t *testing.T) {
	if _, err := edid.Parse(make([]byte, 64)); err == nil {
		t.Error("short blob must be rejected")
	}
	bad := baseBlock(nil)
	bad[0] = 0x11 // corrupt header
	if _, err := edid.Parse(bad); err == nil {
		t.Error("bad header must be rejected")
	}
	cksum := baseBlock(nil)
	cksum[127] ^= 0xFF // corrupt checksum
	if _, err := edid.Parse(cksum); err == nil {
		t.Error("bad checksum must be rejected")
	}
}

// itoa avoids strconv import noise in the format assertions above.
func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var d []byte
	for v > 0 {
		d = append([]byte{byte('0' + v%10)}, d...)
		v /= 10
	}
	if neg {
		d = append([]byte{'-'}, d...)
	}
	return string(d)
}
