package probel

import (
	iprobel "acp/internal/probel"
)

// handleSingleSourceName: rx 101 → tx 106 with NumNames=1.
//
// Reference: SW-P-08 §3.2.19 (rx 101) → §3.3.19 (tx 106).
func (s *server) handleSingleSourceName(f iprobel.Frame) (handlerResult, error) {
	p, err := iprobel.DecodeSingleSourceNameRequest(f)
	if err != nil {
		return handlerResult{}, err
	}
	st, ok := s.tree.lookup(p.MatrixID, p.LevelID)
	var name string
	if ok && int(p.SourceID) < st.sourceCount {
		name = sourceNameOrDefault(st, int(p.SourceID))
	} else {
		// Unknown source — return a positional name rather than erroring.
		name = positionalName("SRC ", int(p.SourceID)+1)
	}
	reply := iprobel.EncodeSourceNamesResponse(iprobel.SourceNamesResponseParams{
		MatrixID:      p.MatrixID,
		LevelID:       p.LevelID,
		NameLength:    p.NameLength,
		FirstSourceID: p.SourceID,
		Names:         []string{name},
	})
	return handlerResult{reply: &reply}, nil
}
