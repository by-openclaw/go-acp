package codec

// SingleSourceNameRequestParams: rx 101 — fetch exactly one source's
// name.  Matrix uses the full Matrix/Level byte; SourceID is u16 split
// into a DIV/MOD 256 pair on the wire.
//
// Reference: SW-P-08 §3.2.19.
type SingleSourceNameRequestParams struct {
	MatrixID   uint8 // 0-15
	LevelID    uint8 // 0-15
	NameLength NameLength
	SourceID   uint16 // 0-65535
}

// EncodeSingleSourceNameRequest packs rx 101 SINGLE SOURCE NAME REQUEST.
//
// | Byte | Field          | Notes                                       |
// |------|----------------|---------------------------------------------|
// |  1   | Matrix / Level | bits[4-7] = Matrix, bits[0-3] = Level          |
// |  2   | Name Length    | 0=4, 1=8, 2=12                                 |
// |  3   | Src mult       | SourceID DIV 256                               |
// |  4   | Src num        | SourceID MOD 256                               |
//
// Spec: SW-P-08 §3.2.19.
func EncodeSingleSourceNameRequest(p SingleSourceNameRequestParams) Frame {
	return Frame{
		ID: RxSingleSourceNameRequest,
		Payload: []byte{
			encodeMatrixLevel(p.MatrixID, p.LevelID),
			byte(p.NameLength),
			byte(p.SourceID / 256),
			byte(p.SourceID % 256),
		},
	}
}

// DecodeSingleSourceNameRequest parses rx 101.
func DecodeSingleSourceNameRequest(f Frame) (SingleSourceNameRequestParams, error) {
	if f.ID != RxSingleSourceNameRequest {
		return SingleSourceNameRequestParams{}, ErrWrongCommand
	}
	if len(f.Payload) < 4 {
		return SingleSourceNameRequestParams{}, ErrShortPayload
	}
	m, l := decodeMatrixLevel(f.Payload[0])
	n := NameLength(f.Payload[1])
	if err := validateNameLength(n); err != nil {
		return SingleSourceNameRequestParams{}, err
	}
	return SingleSourceNameRequestParams{
		MatrixID:   m,
		LevelID:    l,
		NameLength: n,
		SourceID:   uint16(f.Payload[2])*256 + uint16(f.Payload[3]),
	}, nil
}
