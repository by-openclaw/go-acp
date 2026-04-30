package codec

// ConnectOnGoAckParams carries tx 12 CONNECT ON GO ACKNOWLEDGE fields.
// Matrix emits this in reply to rx 05 to confirm that the pending slot
// was stored. See §3.2.14.
//
//	| Byte | Field       | Notes                                       |
//	|------|-------------|---------------------------------------------|
//	|  1   | Multiplier  | same layout as rx 05, except bit 3          |
//	|      |             | "bad source" is always 0 on the ack         |
//	|  2   | Destination | Destination MOD 128                         |
//	|  3   | Source      | Source MOD 128                              |
//
// Spec: SW-P-02 Issue 26 §3.2.14 + §3.2.3 (Multiplier layout).
type ConnectOnGoAckParams struct {
	Destination uint16 // 0-1023
	Source      uint16 // 0-1023
}

// PayloadLenConnectOnGoAck is the fixed MESSAGE byte count for tx 12.
const PayloadLenConnectOnGoAck = 3

// EncodeConnectOnGoAck builds tx 12 wire bytes. §3.2.14 clamps the
// "bad source" flag to 0, so Multiplier bit 3 is always cleared.
func EncodeConnectOnGoAck(p ConnectOnGoAckParams) Frame {
	mult := byte(((p.Destination / 128) & 0x07) << 4)
	mult |= byte((p.Source / 128) & 0x07)
	return Frame{
		ID: TxConnectOnGoAck,
		Payload: []byte{
			mult,
			byte(p.Destination % 128),
			byte(p.Source % 128),
		},
	}
}

// DecodeConnectOnGoAck parses tx 12.
func DecodeConnectOnGoAck(f Frame) (ConnectOnGoAckParams, error) {
	if f.ID != TxConnectOnGoAck {
		return ConnectOnGoAckParams{}, ErrWrongCommand
	}
	if len(f.Payload) < PayloadLenConnectOnGoAck {
		return ConnectOnGoAckParams{}, ErrShortPayload
	}
	mult := f.Payload[0]
	return ConnectOnGoAckParams{
		Destination: (uint16(mult>>4) & 0x07) * 128 + uint16(f.Payload[1]),
		Source:      (uint16(mult) & 0x07) * 128 + uint16(f.Payload[2]),
	}, nil
}
