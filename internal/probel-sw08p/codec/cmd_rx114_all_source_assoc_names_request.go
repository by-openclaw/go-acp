package codec

// AllSourceAssocNamesRequestParams: rx 114 ALL SOURCE ASSOCIATION
// NAMES REQUEST. Byte 1 is matrix-only (bits 0-3), like rx 102.
//
// Reference: SW-P-08 §3.2.24.
type AllSourceAssocNamesRequestParams struct {
	MatrixID   uint8
	NameLength NameLength
}

// EncodeAllSourceAssocNamesRequest packs rx 114.
//
// | Byte | Field        | Notes                         |
// |------|--------------|-------------------------------|
// |  1   | Matrix       | bits[0-3] = Matrix               |
// |  2   | Name Length  | 0=4, 1=8, 2=12                   |
//
// Spec: SW-P-08 §3.2.24.
func EncodeAllSourceAssocNamesRequest(p AllSourceAssocNamesRequestParams) Frame {
	return Frame{
		ID:      RxAllSourceAssocNamesRequest,
		Payload: []byte{p.MatrixID & 0x0F, byte(p.NameLength)},
	}
}

// DecodeAllSourceAssocNamesRequest parses rx 114.
func DecodeAllSourceAssocNamesRequest(f Frame) (AllSourceAssocNamesRequestParams, error) {
	if f.ID != RxAllSourceAssocNamesRequest {
		return AllSourceAssocNamesRequestParams{}, ErrWrongCommand
	}
	if len(f.Payload) < 2 {
		return AllSourceAssocNamesRequestParams{}, ErrShortPayload
	}
	n := NameLength(f.Payload[1])
	if err := validateNameLength(n); err != nil {
		return AllSourceAssocNamesRequestParams{}, err
	}
	return AllSourceAssocNamesRequestParams{
		MatrixID:   f.Payload[0] & 0x0F,
		NameLength: n,
	}, nil
}
