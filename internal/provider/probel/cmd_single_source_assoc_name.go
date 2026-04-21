package probel

import (
	iprobel "acp/internal/probel"
)

// handleSingleSourceAssocName: rx 115 → tx 116 with NumNames=1.
//
// Reference: SW-P-08 §3.2.25 (rx 115) → §3.3.22 (tx 116).
func (s *server) handleSingleSourceAssocName(f iprobel.Frame) (handlerResult, error) {
	p, err := iprobel.DecodeSingleSourceAssocNameRequest(f)
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
	reply := iprobel.EncodeSourceAssocNamesResponse(iprobel.SourceAssocNamesResponseParams{
		MatrixID:                p.MatrixID,
		LevelID:                 0,
		NameLength:              p.NameLength,
		FirstSourceAssociationID: p.SourceAssociationID,
		Names:                   []string{name},
	})
	return handlerResult{reply: &reply}, nil
}
