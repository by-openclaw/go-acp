package codec

import "fmt"

// Numeric conventions of §6.5 and §8.5.
const (
	// PortsPerSlot is the number of ports one client card slot spans in
	// the port address formula.
	PortsPerSlot = 8
	// MaxPort is the highest port address.
	MaxPort = 255
	// GainMute is the gain value that means mute (§8.5).
	GainMute = -128
	// GainMin and GainMax bound a gain; one step is half a decibel.
	GainMin = -36
	GainMax = 36
)

// PortNumber applies the §6.5 formula
//
//	Port address = ((Slot no. - 1) * 8) + Port no. on Client Card - 1
//
// whose own example is port 5 on slot 2 giving 12.
func PortNumber(slot, position int) (int, error) {
	if slot < 1 || position < 1 || position > PortsPerSlot {
		return 0, fmt.Errorf("%w: slot %d, position %d", ErrRange, slot, position)
	}
	port := (slot-1)*PortsPerSlot + position - 1
	if port > MaxPort {
		return 0, fmt.Errorf("%w: slot %d, position %d gives port %d", ErrRange, slot, position, port)
	}
	return port, nil
}

// SlotPosition inverts PortNumber.
func SlotPosition(port int) (slot, position int, err error) {
	if port < 0 || port > MaxPort {
		return 0, 0, fmt.Errorf("%w: port %d", ErrRange, port)
	}
	return port/PortsPerSlot + 1, port%PortsPerSlot + 1, nil
}

// GainDB converts an input or output gain to decibels: "gain [dB] =
// Gain / 2.0", with -128 meaning mute (§8.5).
func GainDB(gain int) (db float64, mute bool, err error) {
	if gain == GainMute {
		return 0, true, nil
	}
	if gain < GainMin || gain > GainMax {
		return 0, false, fmt.Errorf("%w: gain %d", ErrRange, gain)
	}
	return float64(gain) / 2.0, false, nil
}

// VolumeDB converts a crosspoint volume to decibels (§6.5): zero or
// less is mute, 1..255 is (volume-230)/2 dB, above 255 is +12.5 dB.
func VolumeDB(volume int) (db float64, mute bool) {
	switch {
	case volume <= 0:
		return 0, true
	case volume > 255:
		return 12.5, false
	}
	return float64(volume-230) / 2.0, false
}
