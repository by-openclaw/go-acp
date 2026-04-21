package probel

import (
	"acp/internal/probel/codec"
)

// handleSingleDestAssocName: rx 103 → tx 107 with NumNames=1.
//
// Reference: SW-P-08 §3.2.21 (rx 103) → §3.3.20 (tx 107).
func (s *server) handleSingleDestAssocName(f codec.Frame) (handlerResult, error) {
	p, err := codec.DecodeSingleDestAssocNameRequest(f)
	if err != nil {
		return handlerResult{}, err
	}
	st, ok := s.tree.lookup(p.MatrixID, 0)
	var name string
	if ok && int(p.DestAssociationID) < st.targetCount {
		name = destNameOrDefault(st, int(p.DestAssociationID))
	} else {
		name = positionalName("DST ", int(p.DestAssociationID)+1)
	}
	reply := codec.EncodeDestAssocNamesResponse(codec.DestAssocNamesResponseParams{
		MatrixID:              p.MatrixID,
		LevelID:               0,
		NameLength:            p.NameLength,
		FirstDestAssociationID: p.DestAssociationID,
		Names:                 []string{name},
	})
	return handlerResult{reply: &reply}, nil
}
