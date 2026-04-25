package codec

// ConnectOnGoGroupSalvoParams carries rx 35 CONNECT ON GO GROUP SALVO
// fields. One crosspoint per message, grouped under SalvoID. See
// §3.2.36.
//
//	| Byte | Field       | Notes                                     |
//	|------|-------------|-------------------------------------------|
//	|  1   | Multiplier  | same layout as rx 05 §3.2.3               |
//	|  2   | Destination | Destination MOD 128                       |
//	|  3   | Source      | Source MOD 128                            |
//	|  4   | SalvoID     | bit 7 reserved 0, bits[0-6] salvo 0-127   |
//
// Destination and source values always overwrite previous data for
// the same SalvoID — there is no edit-by-index path; a later slot
// with the same dst + src simply re-stages the same crosspoint.
type ConnectOnGoGroupSalvoParams struct {
	Destination uint16 // 0-1023
	Source      uint16 // 0-1023
	BadSource   bool   // mirrors the Multiplier bit-3 flag
	SalvoID     uint8  // 0-127
}

// PayloadLenConnectOnGoGroupSalvo is the fixed MESSAGE byte count for
// rx 35.
const PayloadLenConnectOnGoGroupSalvo = 4

// EncodeConnectOnGoGroupSalvo builds rx 35 wire bytes.
func EncodeConnectOnGoGroupSalvo(p ConnectOnGoGroupSalvoParams) Frame {
	mult := byte(((p.Destination / 128) & 0x07) << 4)
	mult |= byte((p.Source / 128) & 0x07)
	if p.BadSource {
		mult |= 0x08
	}
	return Frame{
		ID: RxConnectOnGoGroupSalvo,
		Payload: []byte{
			mult,
			byte(p.Destination % 128),
			byte(p.Source % 128),
			p.SalvoID & 0x7F,
		},
	}
}

// DecodeConnectOnGoGroupSalvo parses rx 35.
func DecodeConnectOnGoGroupSalvo(f Frame) (ConnectOnGoGroupSalvoParams, error) {
	if f.ID != RxConnectOnGoGroupSalvo {
		return ConnectOnGoGroupSalvoParams{}, ErrWrongCommand
	}
	if len(f.Payload) < PayloadLenConnectOnGoGroupSalvo {
		return ConnectOnGoGroupSalvoParams{}, ErrShortPayload
	}
	mult := f.Payload[0]
	return ConnectOnGoGroupSalvoParams{
		Destination: (uint16(mult>>4) & 0x07) * 128 + uint16(f.Payload[1]),
		Source:      (uint16(mult) & 0x07) * 128 + uint16(f.Payload[2]),
		BadSource:   mult&0x08 != 0,
		SalvoID:     f.Payload[3] & 0x7F,
	}, nil
}
