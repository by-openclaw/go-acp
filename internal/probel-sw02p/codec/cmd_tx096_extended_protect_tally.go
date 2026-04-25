package codec

// ExtendedProtectTallyParams carries tx 96 Extended PROTECT TALLY
// fields — emitted by a controller or router in reply to an
// EXTENDED PROTECT INTERROGATE (§3.2.65) to report the current
// protect status of a destination. See §3.2.60.
//
//	| Byte | Field              | Notes                             |
//	|------|--------------------|-----------------------------------|
//	|  1   | Protect details    | bits[0-1] ProtectState            |
//	|      |                    | bits[2-7] reserved 0              |
//	|  2   | Dest DIV 128       | §3.2.47 form (bits 0-6, bit 7=0)  |
//	|  3   | Dest MOD 128       |                                   |
//	|  4   | Device DIV 128     | bits 0-6 Device DIV 128, bit 7=0  |
//	|  5   | Device MOD 128     |                                   |
//
// Spec: SW-P-02 Issue 26 §3.2.60 + §3.2.47 (Destination Multiplier).
type ExtendedProtectTallyParams struct {
	Protect     ProtectState
	Destination uint16 // 0-16383
	Device      uint16 // 0-16383
}

// PayloadLenExtendedProtectTally is the fixed MESSAGE byte count for
// tx 96.
const PayloadLenExtendedProtectTally = 5

// EncodeExtendedProtectTally builds tx 96 wire bytes.
func EncodeExtendedProtectTally(p ExtendedProtectTallyParams) Frame {
	return Frame{
		ID: TxExtendedProtectTally,
		Payload: []byte{
			byte(p.Protect) & 0x03,
			byte((p.Destination / 128) & 0x7F),
			byte(p.Destination % 128),
			byte((p.Device / 128) & 0x7F),
			byte(p.Device % 128),
		},
	}
}

// DecodeExtendedProtectTally parses tx 96.
func DecodeExtendedProtectTally(f Frame) (ExtendedProtectTallyParams, error) {
	if f.ID != TxExtendedProtectTally {
		return ExtendedProtectTallyParams{}, ErrWrongCommand
	}
	if len(f.Payload) < PayloadLenExtendedProtectTally {
		return ExtendedProtectTallyParams{}, ErrShortPayload
	}
	return ExtendedProtectTallyParams{
		Protect:     ProtectState(f.Payload[0] & 0x03),
		Destination: (uint16(f.Payload[1]) & 0x7F) * 128 + uint16(f.Payload[2]),
		Device:      (uint16(f.Payload[3]) & 0x7F) * 128 + uint16(f.Payload[4]),
	}, nil
}
