package codec

// ConnectOnGoParams carries rx 05 CONNECT ON GO fields. One crosspoint
// per message; the matrix buffers them until the controller sends rx 06
// GO to commit or clear. See §3.2.7.
//
// SW-P-02 is single-matrix, single-level on the wire — there is no
// matrix/level byte. Extended destination/source addressing (up to
// 1023 each) is packed into the Multiplier byte per §3.2.3.
//
//	| Byte | Field       | Notes                                     |
//	|------|-------------|-------------------------------------------|
//	|  1   | Multiplier  | bit 7      reserved 0                     |
//	|      |             | bit 4-6    Destination DIV 128 (0-7)      |
//	|      |             | bit 3      BadSource / UpdateDisabled flag|
//	|      |             | bit 0-2    Source DIV 128 (0-7)           |
//	|  2   | Destination | Destination MOD 128                       |
//	|  3   | Source      | Source MOD 128                            |
//
// Spec: SW-P-02 Issue 26 §3.2.7 + §3.2.3 (Multiplier layout).
type ConnectOnGoParams struct {
	Destination uint16 // 0-1023
	Source      uint16 // 0-1023
	BadSource   bool   // mirrors the Multiplier bit-3 flag
}

// PayloadLenConnectOnGo is the fixed MESSAGE byte count for rx 05. Used
// by the session scanner to peel complete frames off the wire.
const PayloadLenConnectOnGo = 3

// EncodeConnectOnGo builds rx 05 wire bytes. Dest/Source high bits go
// into the Multiplier's bit 4-6 / 0-2 respectively; BadSource sets the
// Multiplier's bit 3.
func EncodeConnectOnGo(p ConnectOnGoParams) Frame {
	mult := byte(((p.Destination / 128) & 0x07) << 4)
	mult |= byte((p.Source / 128) & 0x07)
	if p.BadSource {
		mult |= 0x08
	}
	return Frame{
		ID: RxConnectOnGo,
		Payload: []byte{
			mult,
			byte(p.Destination % 128),
			byte(p.Source % 128),
		},
	}
}

// DecodeConnectOnGo parses rx 05. Rejects frames whose ID is not
// RxConnectOnGo or whose MESSAGE is shorter than PayloadLenConnectOnGo.
func DecodeConnectOnGo(f Frame) (ConnectOnGoParams, error) {
	if f.ID != RxConnectOnGo {
		return ConnectOnGoParams{}, ErrWrongCommand
	}
	if len(f.Payload) < PayloadLenConnectOnGo {
		return ConnectOnGoParams{}, ErrShortPayload
	}
	mult := f.Payload[0]
	return ConnectOnGoParams{
		Destination: (uint16(mult>>4) & 0x07) * 128 + uint16(f.Payload[1]),
		Source:      (uint16(mult) & 0x07) * 128 + uint16(f.Payload[2]),
		BadSource:   mult&0x08 != 0,
	}, nil
}
