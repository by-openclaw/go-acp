package probelsw08p

import (
	"dhs/internal/probel-sw08p/codec"
)

// handleAllDestAssocNames: rx 102 → tx 107.
//
// SW-P-08's "destination associations" and "destinations" overlap in
// scope for simple matrices — we reuse the targetLabels slice of the
// level-0 state on the requested matrix, which is what most controllers
// expect. Every name is sent, over as many tx 107 messages as it takes
// (allNames).
//
// Reference: SW-P-08 §3.2.20 (rx 102) → §3.3.20 (tx 107).
func (s *server) handleAllDestAssocNames(f codec.Frame) (handlerResult, error) {
	p, err := codec.DecodeAllDestAssocNamesRequest(f)
	if err != nil {
		return handlerResult{}, err
	}
	st, ok := s.tree.lookup(p.MatrixID, 0)
	if !ok {
		empty := codec.EncodeDestAssocNamesResponse(codec.DestAssocNamesResponseParams{
			MatrixID: p.MatrixID, LevelID: 0, NameLength: p.NameLength,
			FirstDestAssociationID: 0, Names: nil,
		})
		return handlerResult{reply: &empty}, nil
	}
	return allNames(st.targetCount, p.NameLength.MaxNamesPerMessage(),
		func(i int) string { return destNameOrDefault(st, i) },
		func(first int, names []string) codec.Frame {
			return codec.EncodeDestAssocNamesResponse(codec.DestAssocNamesResponseParams{
				MatrixID: p.MatrixID, LevelID: 0, NameLength: p.NameLength,
				FirstDestAssociationID: uint16(first), Names: names,
			})
		}), nil
}
