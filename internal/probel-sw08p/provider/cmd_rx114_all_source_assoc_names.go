package probelsw08p

import (
	"dhs/internal/probel-sw08p/codec"
)

// handleAllSourceAssocNames: rx 114 → tx 116. Reuses the sources
// slice from (matrix, level=0) — our simple tree doesn't model source
// associations separately from sources. Every name is sent, over as
// many tx 116 messages as it takes (allNames).
//
// Reference: SW-P-08 §3.2.24 (rx 114) → §3.3.22 (tx 116).
func (s *server) handleAllSourceAssocNames(f codec.Frame) (handlerResult, error) {
	p, err := codec.DecodeAllSourceAssocNamesRequest(f)
	if err != nil {
		return handlerResult{}, err
	}
	st, ok := s.tree.lookup(p.MatrixID, 0)
	if !ok {
		empty := codec.EncodeSourceAssocNamesResponse(codec.SourceAssocNamesResponseParams{
			MatrixID: p.MatrixID, LevelID: 0, NameLength: p.NameLength,
			FirstSourceAssociationID: 0, Names: nil,
		})
		return handlerResult{reply: &empty}, nil
	}
	return allNames(st.sourceCount, p.NameLength.MaxNamesPerMessage(),
		func(i int) string { return sourceNameOrDefault(st, i) },
		func(first int, names []string) codec.Frame {
			return codec.EncodeSourceAssocNamesResponse(codec.SourceAssocNamesResponseParams{
				MatrixID: p.MatrixID, LevelID: 0, NameLength: p.NameLength,
				FirstSourceAssociationID: uint16(first), Names: names,
			})
		}), nil
}
