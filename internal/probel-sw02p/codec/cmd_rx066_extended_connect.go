package codec

// ExtendedConnectParams carries rx 66 Extended CONNECT fields.
// Extended addressing (dst/src up to 16383 per §3.2.47 / §3.2.48)
// replaces the narrow rx 02 §3.2.3 Multiplier. Applied immediately;
// the matrix broadcasts tx 68 Extended CONNECTED on all ports to
// confirm. See §3.2.48.
//
//	| Byte | Field              | Notes                             |
//	|------|--------------------|-----------------------------------|
//	|  1   | Destination Mult.  | bits[0-6] Dst DIV 128, bit 7 = 0  |
//	|  2   | Destination number | Destination MOD 128               |
//	|  3   | Source Multiplier  | bits[0-6] Src DIV 128, bit 7 = 0  |
//	|  4   | Source number      | Source MOD 128                    |
//
// Spec: SW-P-02 Issue 26 §3.2.48.
type ExtendedConnectParams struct {
	Destination uint16 // 0-16383
	Source      uint16 // 0-16383
}

// PayloadLenExtendedConnect is the fixed MESSAGE byte count for rx 66.
const PayloadLenExtendedConnect = 4

// EncodeExtendedConnect builds rx 66 wire bytes.
func EncodeExtendedConnect(p ExtendedConnectParams) Frame {
	return Frame{
		ID: RxExtendedConnect,
		Payload: []byte{
			byte((p.Destination / 128) & 0x7F),
			byte(p.Destination % 128),
			byte((p.Source / 128) & 0x7F),
			byte(p.Source % 128),
		},
	}
}

// DecodeExtendedConnect parses rx 66.
func DecodeExtendedConnect(f Frame) (ExtendedConnectParams, error) {
	if f.ID != RxExtendedConnect {
		return ExtendedConnectParams{}, ErrWrongCommand
	}
	if len(f.Payload) < PayloadLenExtendedConnect {
		return ExtendedConnectParams{}, ErrShortPayload
	}
	return ExtendedConnectParams{
		Destination: (uint16(f.Payload[0]) & 0x7F) * 128 + uint16(f.Payload[1]),
		Source:      (uint16(f.Payload[2]) & 0x7F) * 128 + uint16(f.Payload[3]),
	}, nil
}
