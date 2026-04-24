package codec

// ExtendedConnectOnGoAckParams carries tx 070 Extended CONNECT ON GO
// ACKNOWLEDGE fields — matrix confirms rx 069 was staged. Same
// 4-byte layout as the request per §3.2.52.
//
// Spec: SW-P-02 Issue 26 §3.2.52 + §3.2.47 + §3.2.48.
type ExtendedConnectOnGoAckParams struct {
	Destination uint16 // 0-16383
	Source      uint16 // 0-16383
}

// PayloadLenExtendedConnectOnGoAck is the fixed MESSAGE byte count
// for tx 070.
const PayloadLenExtendedConnectOnGoAck = 4

// EncodeExtendedConnectOnGoAck builds tx 070 wire bytes.
func EncodeExtendedConnectOnGoAck(p ExtendedConnectOnGoAckParams) Frame {
	return Frame{
		ID: TxExtendedConnectOnGoAck,
		Payload: []byte{
			byte((p.Destination / 128) & 0x7F),
			byte(p.Destination % 128),
			byte((p.Source / 128) & 0x7F),
			byte(p.Source % 128),
		},
	}
}

// DecodeExtendedConnectOnGoAck parses tx 070.
func DecodeExtendedConnectOnGoAck(f Frame) (ExtendedConnectOnGoAckParams, error) {
	if f.ID != TxExtendedConnectOnGoAck {
		return ExtendedConnectOnGoAckParams{}, ErrWrongCommand
	}
	if len(f.Payload) < PayloadLenExtendedConnectOnGoAck {
		return ExtendedConnectOnGoAckParams{}, ErrShortPayload
	}
	return ExtendedConnectOnGoAckParams{
		Destination: (uint16(f.Payload[0]) & 0x7F) * 128 + uint16(f.Payload[1]),
		Source:      (uint16(f.Payload[2]) & 0x7F) * 128 + uint16(f.Payload[3]),
	}, nil
}
