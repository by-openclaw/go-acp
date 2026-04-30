package codec

// ConnectedParams carries tx 04 CROSSPOINT CONNECTED fields. Matrices
// emit this on all ports after a route is set, usually in response to
// rx 02 CONNECT, and — per project convention mirroring SW-P-08 — also
// per slot after a rx 06 GO / rx 36 GO GROUP SALVO commit. See §3.2.6.
//
//	| Byte | Field       | Notes                                     |
//	|------|-------------|-------------------------------------------|
//	|  1   | Multiplier  | same layout as rx 05 §3.2.3               |
//	|  2   | Destination | Destination MOD 128                       |
//	|  3   | Source      | Source MOD 128                            |
//
// Spec: SW-P-02 Issue 26 §3.2.6 + §3.2.3 (Multiplier layout).
type ConnectedParams struct {
	Destination uint16 // 0-1023
	Source      uint16 // 0-1023
	BadSource   bool   // mirrors the Multiplier bit-3 flag
}

// PayloadLenConnected is the fixed MESSAGE byte count for tx 04.
const PayloadLenConnected = 3

// EncodeConnected builds tx 04 wire bytes.
func EncodeConnected(p ConnectedParams) Frame {
	mult := byte(((p.Destination / 128) & 0x07) << 4)
	mult |= byte((p.Source / 128) & 0x07)
	if p.BadSource {
		mult |= 0x08
	}
	return Frame{
		ID: TxCrosspointConnected,
		Payload: []byte{
			mult,
			byte(p.Destination % 128),
			byte(p.Source % 128),
		},
	}
}

// DecodeConnected parses tx 04.
func DecodeConnected(f Frame) (ConnectedParams, error) {
	if f.ID != TxCrosspointConnected {
		return ConnectedParams{}, ErrWrongCommand
	}
	if len(f.Payload) < PayloadLenConnected {
		return ConnectedParams{}, ErrShortPayload
	}
	mult := f.Payload[0]
	return ConnectedParams{
		Destination: (uint16(mult>>4) & 0x07) * 128 + uint16(f.Payload[1]),
		Source:      (uint16(mult) & 0x07) * 128 + uint16(f.Payload[2]),
		BadSource:   mult&0x08 != 0,
	}, nil
}
