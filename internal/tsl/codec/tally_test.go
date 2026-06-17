package codec

import "testing"

// TestTallyColor_String pins the canonical spec names for every 2-bit
// colour value plus the out-of-range fallback.
func TestTallyColor_String(t *testing.T) {
	cases := []struct {
		in   TallyColor
		want string
	}{
		{TallyOff, "off"},
		{TallyRed, "red"},
		{TallyGreen, "green"},
		{TallyAmber, "amber"},
		{TallyColor(4), "tally-color(4)"},     // out of the 2-bit range
		{TallyColor(255), "tally-color(255)"}, // fallback path
	}
	for _, c := range cases {
		if got := c.in.String(); got != c.want {
			t.Errorf("TallyColor(%d).String() = %q, want %q", uint8(c.in), got, c.want)
		}
	}
}

// TestBrightness_String pins the canonical spec names for every 2-bit
// brightness value plus the out-of-range fallback.
func TestBrightness_String(t *testing.T) {
	cases := []struct {
		in   Brightness
		want string
	}{
		{BrightnessOff, "off"},
		{BrightnessOneSeveth, "1/7"},
		{BrightnessHalf, "1/2"},
		{BrightnessFull, "full"},
		{Brightness(4), "brightness(4)"},     // out of the 2-bit range
		{Brightness(255), "brightness(255)"}, // fallback path
	}
	for _, c := range cases {
		if got := c.in.String(); got != c.want {
			t.Errorf("Brightness(%d).String() = %q, want %q", uint8(c.in), got, c.want)
		}
	}
}
