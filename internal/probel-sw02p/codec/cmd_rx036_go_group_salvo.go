package codec

// GoGroupSalvoParams carries rx 36 GO GROUP SALVO fields. See §3.2.37.
//
//	| Byte | Field     | Notes                                        |
//	|------|-----------|----------------------------------------------|
//	|  1   | Operation | 00 = set previously received CONNECT-ON-GO   |
//	|      |           |       GROUP SALVO slots for SalvoID          |
//	|      |           | 01 = clear previously received slots         |
//	|  2   | SalvoID   | salvo group number as defined in §3.2.36     |
//
// The Operation enum reuses GoOperation from rx 06 — the byte layout
// and set-vs-clear semantics are identical; only the scope (single
// SalvoID instead of the global pending buffer) differs.
type GoGroupSalvoParams struct {
	Operation GoOperation
	SalvoID   uint8
}

// PayloadLenGoGroupSalvo is the fixed MESSAGE byte count for rx 36.
const PayloadLenGoGroupSalvo = 2

// EncodeGoGroupSalvo builds rx 36 wire bytes.
func EncodeGoGroupSalvo(p GoGroupSalvoParams) Frame {
	return Frame{
		ID:      RxGoGroupSalvo,
		Payload: []byte{byte(p.Operation), p.SalvoID & 0x7F},
	}
}

// DecodeGoGroupSalvo parses rx 36.
func DecodeGoGroupSalvo(f Frame) (GoGroupSalvoParams, error) {
	if f.ID != RxGoGroupSalvo {
		return GoGroupSalvoParams{}, ErrWrongCommand
	}
	if len(f.Payload) < PayloadLenGoGroupSalvo {
		return GoGroupSalvoParams{}, ErrShortPayload
	}
	return GoGroupSalvoParams{
		Operation: GoOperation(f.Payload[0]),
		SalvoID:   f.Payload[1] & 0x7F,
	}, nil
}
