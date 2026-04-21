package codec

// SingleDestAssocNameRequestParams: rx 103 — fetch one destination-
// association name. Byte 1 is matrix-only (bits 0-3); assoc id is u16
// split DIV/MOD 256.
//
// Reference: SW-P-08 §3.2.21.
type SingleDestAssocNameRequestParams struct {
	MatrixID          uint8
	NameLength        NameLength
	DestAssociationID uint16
}

// EncodeSingleDestAssocNameRequest packs rx 103.
//
// | Byte | Field        | Notes                                     |
// |------|--------------|-------------------------------------------|
// |  1   | Matrix       | bits[0-3] = Matrix                           |
// |  2   | Name Length  | 0=4, 1=8, 2=12                               |
// |  3   | Dest assoc mult | AssocID DIV 256                            |
// |  4   | Dest assoc num  | AssocID MOD 256                            |
//
// Spec: SW-P-08 §3.2.21.
func EncodeSingleDestAssocNameRequest(p SingleDestAssocNameRequestParams) Frame {
	return Frame{
		ID: RxSingleDestNameRequest,
		Payload: []byte{
			p.MatrixID & 0x0F,
			byte(p.NameLength),
			byte(p.DestAssociationID / 256),
			byte(p.DestAssociationID % 256),
		},
	}
}

// DecodeSingleDestAssocNameRequest parses rx 103.
func DecodeSingleDestAssocNameRequest(f Frame) (SingleDestAssocNameRequestParams, error) {
	if f.ID != RxSingleDestNameRequest {
		return SingleDestAssocNameRequestParams{}, ErrWrongCommand
	}
	if len(f.Payload) < 4 {
		return SingleDestAssocNameRequestParams{}, ErrShortPayload
	}
	n := NameLength(f.Payload[1])
	if err := validateNameLength(n); err != nil {
		return SingleDestAssocNameRequestParams{}, err
	}
	return SingleDestAssocNameRequestParams{
		MatrixID:          f.Payload[0] & 0x0F,
		NameLength:        n,
		DestAssociationID: uint16(f.Payload[2])*256 + uint16(f.Payload[3]),
	}, nil
}
