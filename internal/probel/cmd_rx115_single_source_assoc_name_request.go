package probel

// SingleSourceAssocNameRequestParams: rx 115 SINGLE SOURCE ASSOCIATION
// NAME REQUEST.
//
// Reference: SW-P-08 §3.2.25.
type SingleSourceAssocNameRequestParams struct {
	MatrixID           uint8
	NameLength         NameLength
	SourceAssociationID uint16
}

// EncodeSingleSourceAssocNameRequest packs rx 115.
//
// | Byte | Field        | Notes                          |
// |------|--------------|--------------------------------|
// |  1   | Matrix       | bits[0-3] = Matrix                |
// |  2   | Name Length  | 0=4, 1=8, 2=12                    |
// |  3   | Src assoc mult | SrcAssocID DIV 256              |
// |  4   | Src assoc num  | SrcAssocID MOD 256              |
//
// Spec: SW-P-08 §3.2.25.
func EncodeSingleSourceAssocNameRequest(p SingleSourceAssocNameRequestParams) Frame {
	return Frame{
		ID: RxSingleSourceAssocNameRequest,
		Payload: []byte{
			p.MatrixID & 0x0F,
			byte(p.NameLength),
			byte(p.SourceAssociationID / 256),
			byte(p.SourceAssociationID % 256),
		},
	}
}

// DecodeSingleSourceAssocNameRequest parses rx 115.
func DecodeSingleSourceAssocNameRequest(f Frame) (SingleSourceAssocNameRequestParams, error) {
	if f.ID != RxSingleSourceAssocNameRequest {
		return SingleSourceAssocNameRequestParams{}, ErrWrongCommand
	}
	if len(f.Payload) < 4 {
		return SingleSourceAssocNameRequestParams{}, ErrShortPayload
	}
	n := NameLength(f.Payload[1])
	if err := validateNameLength(n); err != nil {
		return SingleSourceAssocNameRequestParams{}, err
	}
	return SingleSourceAssocNameRequestParams{
		MatrixID:           f.Payload[0] & 0x0F,
		NameLength:         n,
		SourceAssociationID: uint16(f.Payload[2])*256 + uint16(f.Payload[3]),
	}, nil
}
