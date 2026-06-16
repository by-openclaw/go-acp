package codec

import (
	"errors"
	"testing"
)

func TestV40Encode_SpecShape(t *testing.T) {
	f := V40Frame{
		V31: V31Frame{
			Address:    1,
			Tally1:     true,
			Brightness: BrightnessFull,
			Text:       "CAM 1",
		},
		DisplayLeft:  XByte{LH: TallyRed, Text: TallyGreen, RH: TallyAmber},
		DisplayRight: XByte{LH: TallyOff, Text: TallyAmber, RH: TallyRed},
	}
	wire, err := f.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if len(wire) != V40FrameSize {
		t.Fatalf("len=%d, want %d", len(wire), V40FrameSize)
	}

	// v3.1 block: HEADER=0x81, CTRL=0x01|0x30=0x31, DATA "CAM 1" space-padded.
	if wire[0] != 0x81 {
		t.Errorf("HEADER=0x%02x, want 0x81", wire[0])
	}
	if wire[1] != 0x31 {
		t.Errorf("CTRL=0x%02x, want 0x31", wire[1])
	}

	// CHKSUM: 2's-comp mod 128 of sum(HEADER+CTRL+DATA).
	got := wire[V40ChksumIdx]
	want := computeV40Chksum(wire[:V31FrameSize])
	if got != want {
		t.Errorf("CHKSUM=0x%02x, want 0x%02x", got, want)
	}

	// VBC: minor version 0, XDATA count 2 → 0x02
	if wire[V40VBCIdx] != 0x02 {
		t.Errorf("VBC=0x%02x, want 0x02 (minor=0, xdata=2)", wire[V40VBCIdx])
	}

	// Xbyte1 (DisplayLeft): LH=RED(1) << 4 | Text=GREEN(2) << 2 | RH=AMBER(3) = 0x1B
	if got := wire[V40XDataStartIdx]; got != (0x1<<4)|(0x2<<2)|0x3 {
		t.Errorf("Xbyte L = 0x%02x, want 0x%02x", got, byte(0x1<<4)|byte(0x2<<2)|byte(0x3))
	}
	// Xbyte2 (DisplayRight): LH=OFF(0) | Text=AMBER(3) << 2 | RH=RED(1) = 0x0D
	if got := wire[V40XDataStartIdx+1]; got != (0x3<<2)|0x1 {
		t.Errorf("Xbyte R = 0x%02x, want 0x%02x", got, byte(0x3<<2)|byte(0x1))
	}
}

func TestV40RoundTrip_NoNotes(t *testing.T) {
	in := V40Frame{
		V31: V31Frame{
			Address: 10,
			Tally2:  true, Tally4: true,
			Brightness: BrightnessHalf,
			Text:       "PGM",
		},
		DisplayLeft:  XByte{LH: TallyRed, Text: TallyRed, RH: TallyOff},
		DisplayRight: XByte{LH: TallyGreen, Text: TallyGreen, RH: TallyGreen},
	}
	wire, err := in.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := DecodeV40(wire)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.V31.Address != 10 || !got.V31.Tally2 || !got.V31.Tally4 || got.V31.Brightness != BrightnessHalf {
		t.Errorf("v3.1 round-trip: %+v", got.V31)
	}
	if got.DisplayLeft.LH != TallyRed || got.DisplayLeft.Text != TallyRed || got.DisplayLeft.RH != TallyOff {
		t.Errorf("DisplayLeft round-trip: %+v", got.DisplayLeft)
	}
	if got.DisplayRight.LH != TallyGreen || got.DisplayRight.Text != TallyGreen || got.DisplayRight.RH != TallyGreen {
		t.Errorf("DisplayRight round-trip: %+v", got.DisplayRight)
	}
	if got.MinorVersion != 0 || got.XDataCount != 2 {
		t.Errorf("VBC: minor=%d, count=%d (want 0, 2)", got.MinorVersion, got.XDataCount)
	}
	if len(got.Notes) != 0 || len(got.V31.Notes) != 0 {
		t.Errorf("clean frame should have no notes, v3.1=%+v, v4.0=%+v", got.V31.Notes, got.Notes)
	}
}

func TestV40Decode_BadChksum_FiresNote(t *testing.T) {
	f := V40Frame{V31: V31Frame{Address: 3, Text: "X"}}
	wire, err := f.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	wire[V40ChksumIdx] ^= 0xFF // corrupt
	got, err := DecodeV40(wire)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	found := false
	for _, n := range got.Notes {
		if n.Kind == "tsl_checksum_fail" {
			found = true
		}
	}
	if !found {
		t.Errorf("want tsl_checksum_fail note, got %+v", got.Notes)
	}
}

func TestV40Decode_BadMinorVersion_FiresNote(t *testing.T) {
	f := V40Frame{V31: V31Frame{Address: 0, Text: "X"}}
	wire, err := f.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	// Force VBC minor version to 1 (not 0). minor in bits 6-4.
	wire[V40VBCIdx] = (1 << 4) | 2 // minor=1, xdata=2
	got, err := DecodeV40(wire)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	found := false
	for _, n := range got.Notes {
		if n.Kind == "tsl_version_mismatch" {
			found = true
		}
	}
	if !found {
		t.Errorf("want tsl_version_mismatch note, got %+v", got.Notes)
	}
}

func TestV40Decode_VBCBit7Set_FiresNote(t *testing.T) {
	f := V40Frame{V31: V31Frame{Address: 0, Text: "X"}}
	wire, err := f.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	wire[V40VBCIdx] |= 0x80
	got, err := DecodeV40(wire)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	found := false
	for _, n := range got.Notes {
		if n.Kind == "tsl_reserved_bit_set" {
			found = true
		}
	}
	if !found {
		t.Errorf("want tsl_reserved_bit_set note, got %+v", got.Notes)
	}
}

func TestV40Decode_CtrlBit6_CommandData_FiresNote(t *testing.T) {
	// Build a raw v4.0 frame with CTRL.6 set (command data flag).
	wire := make([]byte, V40FrameSize)
	wire[0] = 0x80   // addr 0
	wire[1] = 1 << 6 // CTRL bit 6 set
	for i := 2; i < V31FrameSize; i++ {
		wire[i] = 0x20
	}
	wire[V40ChksumIdx] = computeV40Chksum(wire[:V31FrameSize])
	wire[V40VBCIdx] = 2 // minor=0, xdata=2
	// XDATA left/right zero
	got, err := DecodeV40(wire)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	found := false
	for _, n := range got.Notes {
		if n.Kind == "tsl_control_data_undefined" {
			found = true
		}
	}
	if !found {
		t.Errorf("want tsl_control_data_undefined note, got %+v", got.Notes)
	}
}

func TestV40Decode_TooShort(t *testing.T) {
	_, err := DecodeV40(make([]byte, 19))
	if !errors.Is(err, ErrV40FrameSize) {
		t.Errorf("size=19 should hit ErrV40FrameSize, got %v", err)
	}
}

func TestXByte_Encode_ReservedBit(t *testing.T) {
	// Reserved (bit 6) must round-trip through Encode/Decode.
	x := XByte{LH: TallyRed, Text: TallyGreen, RH: TallyAmber, Reserved: true}
	b := x.Encode()
	if b&v40XDataBit6Mask == 0 {
		t.Fatalf("Reserved bit not set in encoded byte 0x%02x", b)
	}
	got := DecodeXByte(b)
	if !got.Reserved {
		t.Errorf("Reserved did not round-trip: %+v", got)
	}
	if got.LH != TallyRed || got.Text != TallyGreen || got.RH != TallyAmber {
		t.Errorf("colour fields corrupted with Reserved set: %+v", got)
	}
}

func TestComputeV40Chksum_ShortBody(t *testing.T) {
	// A body shorter than the 18-byte v3.1 block returns 0 (guard arm).
	if got := computeV40Chksum(make([]byte, V31FrameSize-1)); got != 0 {
		t.Errorf("short body checksum = 0x%02x, want 0x00", got)
	}
}

func TestV40Encode_PropagatesV31Error(t *testing.T) {
	// V31.Encode rejects an out-of-range address; V40.Encode must surface it.
	f := V40Frame{V31: V31Frame{Address: 200}}
	if _, err := f.Encode(); !errors.Is(err, ErrV31AddressRng) {
		t.Errorf("want ErrV31AddressRng propagated, got %v", err)
	}
}

func TestV40Decode_PropagatesV31Error(t *testing.T) {
	// v3.1 section with HEADER bit 7 clear is structurally unparseable.
	wire := make([]byte, V40FrameSize)
	wire[0] = 0x00 // HEADER MSB clear
	if _, err := DecodeV40(wire); !errors.Is(err, ErrV31HeaderMSB) {
		t.Errorf("want ErrV31HeaderMSB propagated, got %v", err)
	}
}

func TestV40Decode_XDataCountExceedsAvailable_FiresNote(t *testing.T) {
	// Minimum-size frame (20 bytes: v3.1 + CHKSUM + VBC, no XDATA) but VBC
	// claims a non-zero XDATA count. available = len-20 = 0, count > 0.
	wire := make([]byte, V31FrameSize+2)
	wire[0] = 0x80
	for i := 2; i < V31FrameSize; i++ {
		wire[i] = 0x20
	}
	wire[V40ChksumIdx] = computeV40Chksum(wire[:V31FrameSize])
	wire[V40VBCIdx] = 2 // minor=0, xdata count=2 but no XDATA bytes present
	got, err := DecodeV40(wire)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	found := false
	for _, n := range got.Notes {
		if n.Kind == "tsl_v40_xdata_truncated" {
			found = true
		}
	}
	if !found {
		t.Errorf("want tsl_v40_xdata_truncated note, got %+v", got.Notes)
	}
}

func TestXByte_RoundTrip_AllColourPermutations(t *testing.T) {
	for lh := TallyOff; lh <= TallyAmber; lh++ {
		for text := TallyOff; text <= TallyAmber; text++ {
			for rh := TallyOff; rh <= TallyAmber; rh++ {
				x := XByte{LH: lh, Text: text, RH: rh}
				got := DecodeXByte(x.Encode())
				if got.LH != lh || got.Text != text || got.RH != rh {
					t.Errorf("LH=%v Text=%v RH=%v → %+v", lh, text, rh, got)
				}
			}
		}
	}
}
