package codec

// ExtendedProtectConnectParams carries rx 102 Extended PROTECT
// CONNECT fields — §3.2.66. A router or remote device asks the
// matrix to protect a destination on behalf of a given Device
// Number. The matrix replies with tx 097 Extended PROTECT CONNECTED
// (§3.2.61) broadcast on all ports.
//
// §3.2.66 carries NO protect-state byte — the spec implicitly means
// "apply Pro-Bel Protected (state=1)" when the request arrives. The
// ProbelOverride (state=2) and OEM (state=3) states are entered via
// local-admin paths (server.AdminProtect) rather than rx 102.
//
//	| Byte | Field              | Notes                             |
//	|------|--------------------|-----------------------------------|
//	|  1   | Destination Mult.  | §3.2.47 form, bits 0-6 DIV 128    |
//	|  2   | Destination MOD 128|                                   |
//	|  3   | Device Mult.       | bits 0-6 Device DIV 128, bit 7=0  |
//	|  4   | Device MOD 128     |                                   |
//
// Spec: SW-P-02 Issue 26 §3.2.66.
type ExtendedProtectConnectParams struct {
	Destination uint16 // 0-16383
	Device      uint16 // 0-16383, owner identity for the §3.2.58 authority rule
}

// PayloadLenExtendedProtectConnect is the fixed MESSAGE byte count
// for rx 102.
const PayloadLenExtendedProtectConnect = 4

// EncodeExtendedProtectConnect builds rx 102 wire bytes.
func EncodeExtendedProtectConnect(p ExtendedProtectConnectParams) Frame {
	return Frame{
		ID: RxExtendedProtectConnect,
		Payload: []byte{
			byte((p.Destination / 128) & 0x7F),
			byte(p.Destination % 128),
			byte((p.Device / 128) & 0x7F),
			byte(p.Device % 128),
		},
	}
}

// DecodeExtendedProtectConnect parses rx 102.
func DecodeExtendedProtectConnect(f Frame) (ExtendedProtectConnectParams, error) {
	if f.ID != RxExtendedProtectConnect {
		return ExtendedProtectConnectParams{}, ErrWrongCommand
	}
	if len(f.Payload) < PayloadLenExtendedProtectConnect {
		return ExtendedProtectConnectParams{}, ErrShortPayload
	}
	return ExtendedProtectConnectParams{
		Destination: (uint16(f.Payload[0])&0x7F)*128 + uint16(f.Payload[1]),
		Device:      (uint16(f.Payload[2])&0x7F)*128 + uint16(f.Payload[3]),
	}, nil
}
