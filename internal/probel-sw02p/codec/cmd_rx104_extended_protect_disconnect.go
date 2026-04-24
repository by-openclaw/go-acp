package codec

// ExtendedProtectDisconnectParams carries rx 104 Extended PROTECT
// DIS-CONNECT fields — §3.2.68. A router or remote device asks the
// matrix to remove protection from a destination. Matrix replies
// with tx 098 Extended PROTECT DIS-CONNECTED (§3.2.62) broadcast on
// all ports.
//
// Same 4-byte wire layout as rx 102. Device identifies the
// requesting peer for the §3.2.60 owner-only authority rule — only
// the original protecting device can clear a Pro-Bel / OEM
// protection; ProbelOverride is remotely immutable.
//
//	| Byte | Field              | Notes                             |
//	|------|--------------------|-----------------------------------|
//	|  1   | Destination Mult.  | §3.2.47 form, bits 0-6 DIV 128    |
//	|  2   | Destination MOD 128|                                   |
//	|  3   | Device Mult.       | bits 0-6 Device DIV 128, bit 7=0  |
//	|  4   | Device MOD 128     |                                   |
//
// Spec: SW-P-02 Issue 26 §3.2.68.
type ExtendedProtectDisconnectParams struct {
	Destination uint16 // 0-16383
	Device      uint16 // 0-16383, identity used for owner-only authority
}

// PayloadLenExtendedProtectDisconnect is the fixed MESSAGE byte count
// for rx 104.
const PayloadLenExtendedProtectDisconnect = 4

// EncodeExtendedProtectDisconnect builds rx 104 wire bytes.
func EncodeExtendedProtectDisconnect(p ExtendedProtectDisconnectParams) Frame {
	return Frame{
		ID: RxExtendedProtectDisconnect,
		Payload: []byte{
			byte((p.Destination / 128) & 0x7F),
			byte(p.Destination % 128),
			byte((p.Device / 128) & 0x7F),
			byte(p.Device % 128),
		},
	}
}

// DecodeExtendedProtectDisconnect parses rx 104.
func DecodeExtendedProtectDisconnect(f Frame) (ExtendedProtectDisconnectParams, error) {
	if f.ID != RxExtendedProtectDisconnect {
		return ExtendedProtectDisconnectParams{}, ErrWrongCommand
	}
	if len(f.Payload) < PayloadLenExtendedProtectDisconnect {
		return ExtendedProtectDisconnectParams{}, ErrShortPayload
	}
	return ExtendedProtectDisconnectParams{
		Destination: (uint16(f.Payload[0])&0x7F)*128 + uint16(f.Payload[1]),
		Device:      (uint16(f.Payload[2])&0x7F)*128 + uint16(f.Payload[3]),
	}, nil
}
