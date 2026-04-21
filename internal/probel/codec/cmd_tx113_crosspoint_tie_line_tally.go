package codec

import "fmt"

// TieLineSource is one row of the tx 113 source list: the (matrix, level,
// sourceID) currently routed to the queried destination association
// on that level. Spec §3.3.23 bytes 5-8 per source.
type TieLineSource struct {
	MatrixID uint8
	LevelID  uint8
	SourceID uint16
}

// TieLineTallyParams: tx 113 CROSSPOINT TIE LINE TALLY. One row per
// destination level.
//
// Reference: SW-P-08 §3.3.23.
type TieLineTallyParams struct {
	DestMatrixID      uint8
	DestAssociationID uint16
	Sources           []TieLineSource
}

// EncodeTieLineTally packs tx 113.
//
// | Byte    | Field           | Notes                              |
// |---------|-----------------|------------------------------------|
// |  1      | Dest Matrix     |                                    |
// |  2      | Dest Assoc mult | DestAssociationID DIV 256          |
// |  3      | Dest Assoc num  | DestAssociationID MOD 256          |
// |  4      | Num Srcs        | len(Sources)                       |
// |  5..8   | Src 0 tuple     | Matrix, Level, Mult, Num           |
// |  9..12  | Src 1 tuple     | Matrix, Level, Mult, Num           |
// |  ...    | ...             | repeated NumSrcs times             |
//
// Spec: SW-P-08 §3.3.23.
func EncodeTieLineTally(p TieLineTallyParams) Frame {
	n := len(p.Sources)
	payload := make([]byte, 0, 4+n*4)
	payload = append(payload, p.DestMatrixID)
	payload = append(payload, byte(p.DestAssociationID/256))
	payload = append(payload, byte(p.DestAssociationID%256))
	payload = append(payload, byte(n))
	for _, s := range p.Sources {
		payload = append(payload,
			s.MatrixID,
			s.LevelID,
			byte(s.SourceID/256),
			byte(s.SourceID%256),
		)
	}
	return Frame{ID: TxCrosspointTieLineTally, Payload: payload}
}

// DecodeTieLineTally parses tx 113.
func DecodeTieLineTally(f Frame) (TieLineTallyParams, error) {
	if f.ID != TxCrosspointTieLineTally {
		return TieLineTallyParams{}, ErrWrongCommand
	}
	if len(f.Payload) < 4 {
		return TieLineTallyParams{}, ErrShortPayload
	}
	destMatrix := f.Payload[0]
	destAssoc := uint16(f.Payload[1])*256 + uint16(f.Payload[2])
	n := int(f.Payload[3])
	if len(f.Payload) < 4+n*4 {
		return TieLineTallyParams{}, fmt.Errorf("probel: tx 113 needs %d bytes for %d sources, got %d",
			4+n*4, n, len(f.Payload))
	}
	srcs := make([]TieLineSource, n)
	for i := 0; i < n; i++ {
		off := 4 + i*4
		srcs[i] = TieLineSource{
			MatrixID: f.Payload[off],
			LevelID:  f.Payload[off+1],
			SourceID: uint16(f.Payload[off+2])*256 + uint16(f.Payload[off+3]),
		}
	}
	return TieLineTallyParams{
		DestMatrixID:      destMatrix,
		DestAssociationID: destAssoc,
		Sources:           srcs,
	}, nil
}
