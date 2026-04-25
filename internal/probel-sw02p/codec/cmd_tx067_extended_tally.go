package codec

// ExtendedTallyParams carries tx 67 Extended TALLY fields. Matrix
// emits this in reply to rx 65 Extended INTERROGATE, reporting which
// source is routed to the queried destination. See §3.2.49.
//
// Per §3.2.5 note, Source = 1023 signalled out-of-range for the narrow
// TALLY. In the extended form there is no equivalent sentinel in the
// spec; callers signal an unrouted destination by setting Source to
// the declared source-count (or any agreed-upon out-of-range value)
// and relying on the Status byte's bits for additional information.
// This plugin answers "unrouted" by emitting the extended sentinel
// codec.DestOutOfRangeSource (=1023) for consistency with rx 01 / tx 03.
//
//	| Byte | Field              | Notes                              |
//	|------|--------------------|------------------------------------|
//	|  1   | Destination Mult.  | bits[0-6] Dst DIV 128, bit 7 = 0   |
//	|  2   | Destination number | Destination MOD 128                |
//	|  3   | Source Multiplier  | bits[0-6] Src DIV 128, bit 7 = 0   |
//	|  4   | Source number      | Source MOD 128                     |
//	|  5   | Status             | bit 0 = Crosspoint update disabled |
//	|      |                    | bit 1 = Bad source                 |
//	|      |                    | bit 2-7 = 0                        |
//
// Spec: SW-P-02 Issue 26 §3.2.49.
type ExtendedTallyParams struct {
	Destination uint16 // 0-16383
	Source      uint16 // 0-16383
	UpdateOff   bool   // Status bit 0
	BadSource   bool   // Status bit 1
}

// PayloadLenExtendedTally is the fixed MESSAGE byte count for tx 67.
const PayloadLenExtendedTally = 5

// EncodeExtendedTally builds tx 67 wire bytes.
func EncodeExtendedTally(p ExtendedTallyParams) Frame {
	var status byte
	if p.UpdateOff {
		status |= 1 << 0
	}
	if p.BadSource {
		status |= 1 << 1
	}
	return Frame{
		ID: TxExtendedTally,
		Payload: []byte{
			byte((p.Destination / 128) & 0x7F),
			byte(p.Destination % 128),
			byte((p.Source / 128) & 0x7F),
			byte(p.Source % 128),
			status,
		},
	}
}

// DecodeExtendedTally parses tx 67.
func DecodeExtendedTally(f Frame) (ExtendedTallyParams, error) {
	if f.ID != TxExtendedTally {
		return ExtendedTallyParams{}, ErrWrongCommand
	}
	if len(f.Payload) < PayloadLenExtendedTally {
		return ExtendedTallyParams{}, ErrShortPayload
	}
	s := f.Payload[4]
	return ExtendedTallyParams{
		Destination: (uint16(f.Payload[0]) & 0x7F) * 128 + uint16(f.Payload[1]),
		Source:      (uint16(f.Payload[2]) & 0x7F) * 128 + uint16(f.Payload[3]),
		UpdateOff:   s&(1<<0) != 0,
		BadSource:   s&(1<<1) != 0,
	}, nil
}
