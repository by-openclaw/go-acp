package codec

// ConnectParams carries rx 02 CONNECT fields. Controller requests a
// route through the matrix; the matrix makes the route and broadcasts
// tx 04 CROSSPOINT CONNECTED on all ports (§3.2.6) to confirm. Unlike
// the salvo family (rx 05 / 35 / 71), rx 02 is applied immediately and
// does not use any pending buffer. See §3.2.4.
//
//	| Byte | Field       | Notes                                     |
//	|------|-------------|-------------------------------------------|
//	|  1   | Multiplier  | same layout as rx 01 §3.2.3               |
//	|  2   | Destination | Destination MOD 128                       |
//	|  3   | Source      | Source MOD 128                            |
//
// Spec: SW-P-02 Issue 26 §3.2.4 + §3.2.3 (Multiplier layout).
type ConnectParams struct {
	Destination uint16 // 0-1023
	Source      uint16 // 0-1023
	BadSource   bool   // mirrors the Multiplier bit-3 flag
}

// PayloadLenConnect is the fixed MESSAGE byte count for rx 02.
const PayloadLenConnect = 3

// EncodeConnect builds rx 02 wire bytes.
func EncodeConnect(p ConnectParams) Frame {
	mult := byte(((p.Destination / 128) & 0x07) << 4)
	mult |= byte((p.Source / 128) & 0x07)
	if p.BadSource {
		mult |= 0x08
	}
	return Frame{
		ID: RxConnect,
		Payload: []byte{
			mult,
			byte(p.Destination % 128),
			byte(p.Source % 128),
		},
	}
}

// DecodeConnect parses rx 02.
func DecodeConnect(f Frame) (ConnectParams, error) {
	if f.ID != RxConnect {
		return ConnectParams{}, ErrWrongCommand
	}
	if len(f.Payload) < PayloadLenConnect {
		return ConnectParams{}, ErrShortPayload
	}
	mult := f.Payload[0]
	return ConnectParams{
		Destination: (uint16(mult>>4) & 0x07) * 128 + uint16(f.Payload[1]),
		Source:      (uint16(mult) & 0x07) * 128 + uint16(f.Payload[2]),
		BadSource:   mult&0x08 != 0,
	}, nil
}
