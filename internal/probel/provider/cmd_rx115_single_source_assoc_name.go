package probel

import (
	"acp/internal/probel/codec"
)

// handleSingleSourceAssocName: rx 115 → tx 116 with NumNames=1.
//
// Reference: SW-P-08 §3.2.25 (rx 115) → §3.3.22 (tx 116).
func (s *server) handleSingleSourceAssocName(f codec.Frame) (handlerResult, error) {
	p, err := codec.DecodeSingleSourceAssocNameRequest(f)
	if err != nil {
		return handlerResult{}, err
	}
	st, ok := s.tree.lookup(p.MatrixID, 0)
	var name string
	if ok && int(p.SourceAssociationID) < st.sourceCount {
		name = sourceNameOrDefault(st, int(p.SourceAssociationID))
	} else {
		name = positionalName("SRC ", int(p.SourceAssociationID)+1)
	}
	reply := codec.EncodeSourceAssocNamesResponse(codec.SourceAssocNamesResponseParams{
		MatrixID:                p.MatrixID,
		LevelID:                 0,
		NameLength:              p.NameLength,
		FirstSourceAssociationID: p.SourceAssociationID,
		Names:                   []string{name},
	})
	return handlerResult{reply: &reply}, nil
}
