package probel

import (
	iprobel "acp/internal/probel"
)

// handleAllDestAssocNames: rx 102 → tx 107.
//
// SW-P-08's "destination associations" and "destinations" overlap in
// scope for simple matrices — we reuse the targetLabels slice of the
// level-0 state on the requested matrix, which is what most controllers
// expect. Pagination caveat identical to handleAllSourceNames.
//
// Reference: SW-P-08 §3.2.20 (rx 102) → §3.3.20 (tx 107).
func (s *server) handleAllDestAssocNames(f iprobel.Frame) (handlerResult, error) {
	p, err := iprobel.DecodeAllDestAssocNamesRequest(f)
	if err != nil {
		return handlerResult{}, err
	}
	st, ok := s.tree.lookup(p.MatrixID, 0)
	if !ok {
		empty := iprobel.EncodeDestAssocNamesResponse(iprobel.DestAssocNamesResponseParams{
			MatrixID: p.MatrixID, LevelID: 0, NameLength: p.NameLength,
			FirstDestAssociationID: 0, Names: nil,
		})
		return handlerResult{reply: &empty}, nil
	}
	max := p.NameLength.MaxNamesPerMessage()
	count := st.targetCount
	if count > max {
		count = max
	}
	names := make([]string, count)
	for i := 0; i < count; i++ {
		names[i] = destNameOrDefault(st, i)
	}
	reply := iprobel.EncodeDestAssocNamesResponse(iprobel.DestAssocNamesResponseParams{
		MatrixID: p.MatrixID, LevelID: 0, NameLength: p.NameLength,
		FirstDestAssociationID: 0, Names: names,
	})
	return handlerResult{reply: &reply}, nil
}
