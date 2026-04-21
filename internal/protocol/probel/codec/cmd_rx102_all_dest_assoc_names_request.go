package codec

// AllDestAssocNamesRequestParams: rx 102 — fetch every destination-
// association name on a given matrix. Unlike rx 100, this command's
// byte 1 encodes matrix only (bits 0-3); level has no meaning here.
//
// Reference: SW-P-08 §3.2.20.
type AllDestAssocNamesRequestParams struct {
	MatrixID   uint8 // 0-15 (bits 0-3)
	NameLength NameLength
}

// EncodeAllDestAssocNamesRequest packs rx 102.
//
// | Byte | Field        | Notes                                     |
// |------|--------------|-------------------------------------------|
// |  1   | Matrix       | bits[0-3] = Matrix; bits[4-7] unused         |
// |  2   | Name Length  | 0=4, 1=8, 2=12                               |
//
// Spec: SW-P-08 §3.2.20.
func EncodeAllDestAssocNamesRequest(p AllDestAssocNamesRequestParams) Frame {
	return Frame{
		ID:      RxAllDestNamesRequest,
		Payload: []byte{p.MatrixID & 0x0F, byte(p.NameLength)},
	}
}

// DecodeAllDestAssocNamesRequest parses rx 102.
func DecodeAllDestAssocNamesRequest(f Frame) (AllDestAssocNamesRequestParams, error) {
	if f.ID != RxAllDestNamesRequest {
		return AllDestAssocNamesRequestParams{}, ErrWrongCommand
	}
	if len(f.Payload) < 2 {
		return AllDestAssocNamesRequestParams{}, ErrShortPayload
	}
	n := NameLength(f.Payload[1])
	if err := validateNameLength(n); err != nil {
		return AllDestAssocNamesRequestParams{}, err
	}
	return AllDestAssocNamesRequestParams{
		MatrixID:   f.Payload[0] & 0x0F,
		NameLength: n,
	}, nil
}
