package codec

// ExtendedConnectOnGoGroupSalvoParams carries rx 71 Extended CONNECT
// ON GO GROUP SALVO fields. §3.2.53 + §3.2.47 + §3.2.48.
//
// Extended addressing replaces the §3.2.3 packed Multiplier (1 byte
// carrying dst/src high bits + bad-source flag) with TWO dedicated
// multiplier bytes, one for destination and one for source. Each
// multiplier's bits 0-6 carry the high bits (DIV 128) → per-axis
// range expands from 1023 (§3.2.3) to 16383 (§3.2.47 / §3.2.48).
//
//	| Byte | Field                | Notes                         |
//	|------|----------------------|-------------------------------|
//	|  1   | Destination Mult.    | bits[0-6] Dst DIV 128, bit 7=0|
//	|  2   | Destination number   | Destination MOD 128           |
//	|  3   | Source Multiplier    | bits[0-6] Src DIV 128, bit 7=0|
//	|  4   | Source number        | Source MOD 128                |
//	|  5   | SalvoID              | bit 7 reserved 0, bits[0-6]   |
//
// Neither multiplier carries the §3.2.3 "bad source" flag — extended
// form has no equivalent field. Callers that need the flag must use
// the narrow rx 35 variant.
type ExtendedConnectOnGoGroupSalvoParams struct {
	Destination uint16 // 0-16383
	Source      uint16 // 0-16383
	SalvoID     uint8  // 0-127
}

// PayloadLenExtendedConnectOnGoGroupSalvo is the fixed MESSAGE byte
// count for rx 71.
const PayloadLenExtendedConnectOnGoGroupSalvo = 5

// EncodeExtendedConnectOnGoGroupSalvo builds rx 71 wire bytes.
func EncodeExtendedConnectOnGoGroupSalvo(p ExtendedConnectOnGoGroupSalvoParams) Frame {
	return Frame{
		ID: RxExtendedConnectOnGoGroupSalvo,
		Payload: []byte{
			byte((p.Destination / 128) & 0x7F),
			byte(p.Destination % 128),
			byte((p.Source / 128) & 0x7F),
			byte(p.Source % 128),
			p.SalvoID & 0x7F,
		},
	}
}

// DecodeExtendedConnectOnGoGroupSalvo parses rx 71.
func DecodeExtendedConnectOnGoGroupSalvo(f Frame) (ExtendedConnectOnGoGroupSalvoParams, error) {
	if f.ID != RxExtendedConnectOnGoGroupSalvo {
		return ExtendedConnectOnGoGroupSalvoParams{}, ErrWrongCommand
	}
	if len(f.Payload) < PayloadLenExtendedConnectOnGoGroupSalvo {
		return ExtendedConnectOnGoGroupSalvoParams{}, ErrShortPayload
	}
	return ExtendedConnectOnGoGroupSalvoParams{
		Destination: (uint16(f.Payload[0]) & 0x7F) * 128 + uint16(f.Payload[1]),
		Source:      (uint16(f.Payload[2]) & 0x7F) * 128 + uint16(f.Payload[3]),
		SalvoID:     f.Payload[4] & 0x7F,
	}, nil
}
