package codec

// TieLineInterrogateParams: rx 112 CROSSPOINT TIE LINE INTERROGATE —
// asks the matrix for the per-level tally of a destination association.
// The matrix replies with tx 113 CROSSPOINT TIE LINE TALLY (one row
// per destination level).
//
// No extended pair — there is no cmd 240 in SW-P-08 Issue 30. The
// narrow form is already wide enough: byte 1 is a FULL 8-bit matrix
// byte (the spec quotes a device range of 0-19 but the wire field is
// the full 0-255), and DestAssociationID is 16-bit (DIV/MOD 256). The
// tie-line family does not need a needsExtended() method.
//
// Reference: SW-P-08 Issue 30 §3.2.28.
type TieLineInterrogateParams struct {
	MatrixID          uint8  // wire range 0-255 (spec device limit 0-19)
	DestAssociationID uint16 // 0-65535 (DIV/MOD 256)
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
