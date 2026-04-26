package codec

import "fmt"

// TallyColor is the 2-bit colour shared by v4.0 XDATA and v5.0 CONTROL
// tallies. v3.1 has no colour — its 4 tallies are binary on/off.
type TallyColor uint8

const (
	TallyOff   TallyColor = 0
	TallyRed   TallyColor = 1
	TallyGreen TallyColor = 2
	TallyAmber TallyColor = 3
)

// String renders the canonical spec name.
func (c TallyColor) String() string {
	switch c {
	case TallyOff:
		return "off"
	case TallyRed:
		return "red"
	case TallyGreen:
		return "green"
	case TallyAmber:
		return "amber"
	}
	return fmt.Sprintf("tally-color(%d)", uint8(c))
}

// Brightness is the 2-bit brightness field (CTRL bits 4-5 in v3.1/v4.0,
// CONTROL bits 6-7 in v5.0).
type Brightness uint8

const (
	BrightnessOff       Brightness = 0
	BrightnessOneSeveth Brightness = 1 // 1/7
	BrightnessHalf      Brightness = 2 // 1/2
	BrightnessFull      Brightness = 3
)

// String renders the canonical spec name.
func (b Brightness) String() string {
	switch b {
	case BrightnessOff:
		return "off"
	case BrightnessOneSeveth:
		return "1/7"
	case BrightnessHalf:
		return "1/2"
	case BrightnessFull:
		return "full"
	}
	return fmt.Sprintf("brightness(%d)", uint8(b))
}

// ComplianceNote records a spec deviation surfaced by the decoder. The
// consumer/provider layer lifts these into the compliance.Profile event
// stream.
type ComplianceNote struct {
	Kind   string // e.g. "tsl_reserved_bit_set"
	Detail string
}
