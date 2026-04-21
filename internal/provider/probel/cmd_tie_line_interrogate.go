package probel

import (
	iprobel "acp/internal/probel"
)

// handleTieLineInterrogate: rx 112 → tx 113.
//
// SW-P-08 tie-lines map destination associations to per-level sources.
// Our simple tree isn't association-aware, so this handler interprets
// DestAssociationID as a raw destination id and walks every level
// registered on the requested matrix. For each (matrix, level) pair
// with a routed source on that dst, one TieLineSource row is emitted.
//
// Reference: SW-P-08 §3.2.28 (rx 112) → §3.3.23 (tx 113).
func (s *server) handleTieLineInterrogate(f iprobel.Frame) (handlerResult, error) {
	p, err := iprobel.DecodeTieLineInterrogate(f)
	if err != nil {
		return handlerResult{}, err
	}
	s.tree.mu.RLock()
	defer s.tree.mu.RUnlock()

	var sources []iprobel.TieLineSource
	for key, st := range s.tree.matrices {
		if key.matrix != p.MatrixID {
			continue
		}
		if int(p.DestAssociationID) >= st.targetCount {
			continue
		}
		src := st.sources[p.DestAssociationID]
		if src < 0 {
			continue
		}
		sources = append(sources, iprobel.TieLineSource{
			MatrixID: key.matrix,
			LevelID:  key.level,
			SourceID: uint16(src),
		})
	}
	reply := iprobel.EncodeTieLineTally(iprobel.TieLineTallyParams{
		DestMatrixID:      p.MatrixID,
		DestAssociationID: p.DestAssociationID,
		Sources:           sources,
	})
	return handlerResult{reply: &reply}, nil
}
