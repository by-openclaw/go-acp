package codec

// ExtendedProtectConnectedParams carries tx 97 Extended PROTECT
// CONNECTED fields — broadcast on all ports when protect data is
// altered (successfully or otherwise) as a result of an
// EXTENDED PROTECT CONNECT (§3.2.66). Same layout as tx 96 (§3.2.60).
// See §3.2.61.
//
//	| Byte | Field            | Notes                                |
//	|------|------------------|--------------------------------------|
//	|  1   | Protect details  | bits[0-1] ProtectState (§3.2.58)     |
//	|  2   | Dest DIV 128     | §3.2.47 form                         |
//	|  3   | Dest MOD 128     |                                      |
//	|  4   | Device DIV 128   | bits 0-6 Device DIV 128, bit 7=0     |
//	|  5   | Device MOD 128   |                                      |
//
// Spec: SW-P-02 Issue 26 §3.2.61.
type ExtendedProtectConnectedParams struct {
	Protect     ProtectState
	Destination uint16 // 0-16383
	Device      uint16 // 0-16383
}

// PayloadLenExtendedProtectConnected is the fixed MESSAGE byte count
// for tx 97.
const PayloadLenExtendedProtectConnected = 5

// EncodeExtendedProtectConnected builds tx 97 wire bytes.
func EncodeExtendedProtectConnected(p ExtendedProtectConnectedParams) Frame {
	return Frame{
		ID: TxExtendedProtectConnected,
		Payload: []byte{
			byte(p.Protect) & 0x03,
			byte((p.Destination / 128) & 0x7F),
			byte(p.Destination % 128),
			byte((p.Device / 128) & 0x7F),
			byte(p.Device % 128),
		},
	}
}

// DecodeExtendedProtectConnected parses tx 97.
func DecodeExtendedProtectConnected(f Frame) (ExtendedProtectConnectedParams, error) {
	if f.ID != TxExtendedProtectConnected {
		return ExtendedProtectConnectedParams{}, ErrWrongCommand
	}
	if len(f.Payload) < PayloadLenExtendedProtectConnected {
		return ExtendedProtectConnectedParams{}, ErrShortPayload
	}
	return ExtendedProtectConnectedParams{
		Protect:     ProtectState(f.Payload[0] & 0x03),
		Destination: (uint16(f.Payload[1]) & 0x7F) * 128 + uint16(f.Payload[2]),
		Device:      (uint16(f.Payload[3]) & 0x7F) * 128 + uint16(f.Payload[4]),
	}, nil
}
