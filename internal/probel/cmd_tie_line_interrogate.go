package probel

// TieLineInterrogateParams: rx 112 CROSSPOINT TIE LINE INTERROGATE —
// asks the matrix for the per-level tally of a destination association.
//
// Reference: SW-P-08 §3.2.28.
type TieLineInterrogateParams struct {
	MatrixID          uint8  // 0-19
	DestAssociationID uint16 // u16
}

// EncodeTieLineInterrogate packs rx 112.
//
// | Byte | Field           | Notes                               |
// |------|-----------------|-------------------------------------|
// |  1   | Matrix          | 0-19                                |
// |  2   | Dest Assoc mult | DestAssociationID DIV 256           |
// |  3   | Dest Assoc num  | DestAssociationID MOD 256           |
//
// Spec: SW-P-08 §3.2.28.
func EncodeTieLineInterrogate(p TieLineInterrogateParams) Frame {
	return Frame{
		ID: RxCrosspointTieLineInterrogate,
		Payload: []byte{
			p.MatrixID,
			byte(p.DestAssociationID / 256),
			byte(p.DestAssociationID % 256),
		},
	}
}

// DecodeTieLineInterrogate parses rx 112.
func DecodeTieLineInterrogate(f Frame) (TieLineInterrogateParams, error) {
	if f.ID != RxCrosspointTieLineInterrogate {
		return TieLineInterrogateParams{}, ErrWrongCommand
	}
	if len(f.Payload) < 3 {
		return TieLineInterrogateParams{}, ErrShortPayload
	}
	return TieLineInterrogateParams{
		MatrixID:          f.Payload[0],
		DestAssociationID: uint16(f.Payload[1])*256 + uint16(f.Payload[2]),
	}, nil
}
