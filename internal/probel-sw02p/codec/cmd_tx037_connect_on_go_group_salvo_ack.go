package codec

// ConnectOnGoGroupSalvoAckParams carries tx 37 CONNECT ON GO GROUP
// SALVO ACKNOWLEDGE fields. Matrix emits this in reply to rx 35 to
// confirm the slot was stored against SalvoID. §3.2.38.
//
//	| Byte | Field       | Notes                                       |
//	|------|-------------|---------------------------------------------|
//	|  1   | Multiplier  | bad source bit always 0 per §3.2.38         |
//	|  2   | Destination | Destination MOD 128                         |
//	|  3   | Source      | Source MOD 128                              |
//	|  4   | SalvoID     | salvo group number echoed from rx 35        |
type ConnectOnGoGroupSalvoAckParams struct {
	Destination uint16 // 0-1023
	Source      uint16 // 0-1023
	SalvoID     uint8  // 0-127
}

// PayloadLenConnectOnGoGroupSalvoAck is the fixed MESSAGE byte count
// for tx 37.
const PayloadLenConnectOnGoGroupSalvoAck = 4

// EncodeConnectOnGoGroupSalvoAck builds tx 37 wire bytes. §3.2.38
// clamps the bad-source Multiplier bit to 0.
func EncodeConnectOnGoGroupSalvoAck(p ConnectOnGoGroupSalvoAckParams) Frame {
	mult := byte(((p.Destination / 128) & 0x07) << 4)
	mult |= byte((p.Source / 128) & 0x07)
	return Frame{
		ID: TxConnectOnGoGroupSalvoAck,
		Payload: []byte{
			mult,
			byte(p.Destination % 128),
			byte(p.Source % 128),
			p.SalvoID & 0x7F,
		},
	}
}

// DecodeConnectOnGoGroupSalvoAck parses tx 37.
func DecodeConnectOnGoGroupSalvoAck(f Frame) (ConnectOnGoGroupSalvoAckParams, error) {
	if f.ID != TxConnectOnGoGroupSalvoAck {
		return ConnectOnGoGroupSalvoAckParams{}, ErrWrongCommand
	}
	if len(f.Payload) < PayloadLenConnectOnGoGroupSalvoAck {
		return ConnectOnGoGroupSalvoAckParams{}, ErrShortPayload
	}
	mult := f.Payload[0]
	return ConnectOnGoGroupSalvoAckParams{
		Destination: (uint16(mult>>4) & 0x07) * 128 + uint16(f.Payload[1]),
		Source:      (uint16(mult) & 0x07) * 128 + uint16(f.Payload[2]),
		SalvoID:     f.Payload[3] & 0x7F,
	}, nil
}
