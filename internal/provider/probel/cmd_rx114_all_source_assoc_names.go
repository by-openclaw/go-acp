package probel

import (
	iprobel "acp/internal/probel"
)

// handleAllSourceAssocNames: rx 114 → tx 116. Reuses the sources
// slice from (matrix, level=0) — our simple tree doesn't model source
// associations separately from sources. Pagination caveat identical
// to handleAllSourceNames.
//
// Reference: SW-P-08 §3.2.24 (rx 114) → §3.3.22 (tx 116).
func (s *server) handleAllSourceAssocNames(f iprobel.Frame) (handlerResult, error) {
	p, err := iprobel.DecodeAllSourceAssocNamesRequest(f)
	if err != nil {
		return handlerResult{}, err
	}
	st, ok := s.tree.lookup(p.MatrixID, 0)
	if !ok {
		empty := iprobel.EncodeSourceAssocNamesResponse(iprobel.SourceAssocNamesResponseParams{
			MatrixID: p.MatrixID, LevelID: 0, NameLength: p.NameLength,
			FirstSourceAssociationID: 0, Names: nil,
		})
		return handlerResult{reply: &empty}, nil
	}
	max := p.NameLength.MaxNamesPerMessage()
	count := st.sourceCount
	if count > max {
		count = max
	}
	names := make([]string, count)
	for i := 0; i < count; i++ {
		names[i] = sourceNameOrDefault(st, i)
	}
	reply := iprobel.EncodeSourceAssocNamesResponse(iprobel.SourceAssocNamesResponseParams{
		MatrixID: p.MatrixID, LevelID: 0, NameLength: p.NameLength,
		FirstSourceAssociationID: 0, Names: names,
	})
	return handlerResult{reply: &reply}, nil
}
