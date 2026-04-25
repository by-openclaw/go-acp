package codec

// ExtendedProtectDisconnectedParams carries tx 98 Extended PROTECT
// DIS-CONNECTED fields — broadcast on all ports when a destination
// is unprotected (successfully or otherwise) as a result of an
// EXTENDED PROTECT DIS-CONNECT (§3.2.68). Same layout as tx 96
// (§3.2.60). See §3.2.62.
//
//	| Byte | Field            | Notes                                |
//	|------|------------------|--------------------------------------|
//	|  1   | Protect details  | bits[0-1] ProtectState (§3.2.58)     |
//	|  2   | Dest DIV 128     | §3.2.47 form                         |
//	|  3   | Dest MOD 128     |                                      |
//	|  4   | Device DIV 128   | bits 0-6 Device DIV 128, bit 7=0     |
//	|  5   | Device MOD 128   |                                      |
//
// Spec: SW-P-02 Issue 26 §3.2.62.
type ExtendedProtectDisconnectedParams struct {
	Protect     ProtectState
	Destination uint16 // 0-16383
	Device      uint16 // 0-16383
}

// PayloadLenExtendedProtectDisconnected is the fixed MESSAGE byte count
// for tx 98.
const PayloadLenExtendedProtectDisconnected = 5

// EncodeExtendedProtectDisconnected builds tx 98 wire bytes.
func EncodeExtendedProtectDisconnected(p ExtendedProtectDisconnectedParams) Frame {
	return Frame{
		ID: TxExtendedProtectDisconnected,
		Payload: []byte{
			byte(p.Protect) & 0x03,
			byte((p.Destination / 128) & 0x7F),
			byte(p.Destination % 128),
			byte((p.Device / 128) & 0x7F),
			byte(p.Device % 128),
		},
	}
}

// DecodeExtendedProtectDisconnected parses tx 98.
func DecodeExtendedProtectDisconnected(f Frame) (ExtendedProtectDisconnectedParams, error) {
	if f.ID != TxExtendedProtectDisconnected {
		return ExtendedProtectDisconnectedParams{}, ErrWrongCommand
	}
	if len(f.Payload) < PayloadLenExtendedProtectDisconnected {
		return ExtendedProtectDisconnectedParams{}, ErrShortPayload
	}
	return ExtendedProtectDisconnectedParams{
		Protect:     ProtectState(f.Payload[0] & 0x03),
		Destination: (uint16(f.Payload[1]) & 0x7F) * 128 + uint16(f.Payload[2]),
		Device:      (uint16(f.Payload[3]) & 0x7F) * 128 + uint16(f.Payload[4]),
	}, nil
}
