package codec

// ExtendedConnectOnGoGroupSalvoAckParams carries tx 72 Extended
// CONNECT ON GO GROUP SALVO ACKNOWLEDGE fields. §3.2.54.
//
// Byte layout mirrors rx 71 (§3.2.53) — Destination Multiplier + Dest
// MOD 128 + Source Multiplier + Src MOD 128 + SalvoID. No separate
// "bad source" bit in extended form (unlike the narrow-form tx 37
// where §3.2.38 clamps Multiplier bit 3 to 0).
type ExtendedConnectOnGoGroupSalvoAckParams struct {
	Destination uint16 // 0-16383
	Source      uint16 // 0-16383
	SalvoID     uint8  // 0-127
}

// PayloadLenExtendedConnectOnGoGroupSalvoAck is the fixed MESSAGE byte
// count for tx 72.
const PayloadLenExtendedConnectOnGoGroupSalvoAck = 5

// EncodeExtendedConnectOnGoGroupSalvoAck builds tx 72 wire bytes.
func EncodeExtendedConnectOnGoGroupSalvoAck(p ExtendedConnectOnGoGroupSalvoAckParams) Frame {
	return Frame{
		ID: TxExtendedConnectOnGoGroupSalvoAck,
		Payload: []byte{
			byte((p.Destination / 128) & 0x7F),
			byte(p.Destination % 128),
			byte((p.Source / 128) & 0x7F),
			byte(p.Source % 128),
			p.SalvoID & 0x7F,
		},
	}
}

// DecodeExtendedConnectOnGoGroupSalvoAck parses tx 72.
func DecodeExtendedConnectOnGoGroupSalvoAck(f Frame) (ExtendedConnectOnGoGroupSalvoAckParams, error) {
	if f.ID != TxExtendedConnectOnGoGroupSalvoAck {
		return ExtendedConnectOnGoGroupSalvoAckParams{}, ErrWrongCommand
	}
	if len(f.Payload) < PayloadLenExtendedConnectOnGoGroupSalvoAck {
		return ExtendedConnectOnGoGroupSalvoAckParams{}, ErrShortPayload
	}
	return ExtendedConnectOnGoGroupSalvoAckParams{
		Destination: (uint16(f.Payload[0]) & 0x7F) * 128 + uint16(f.Payload[1]),
		Source:      (uint16(f.Payload[2]) & 0x7F) * 128 + uint16(f.Payload[3]),
		SalvoID:     f.Payload[4] & 0x7F,
	}, nil
}
