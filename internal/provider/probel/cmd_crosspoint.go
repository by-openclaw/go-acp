package probel

import (
	iprobel "acp/internal/probel"
)

// --- rx 001 / rx 0x81 : Crosspoint Interrogate → tx 003 / tx 0x83 Tally ----

// handleCrosspointInterrogate decodes the incoming interrogate, looks
// up the current source on (matrix, level, dst) in the tree, and
// builds a Crosspoint Tally reply.
//
// An unrouted destination is reported with SourceID=0 — SW-P-08 has no
// distinct "unrouted" sentinel, and controllers typically treat src=0
// as "unknown / default". Provider state-of-the-art: the tree starts
// unrouted; only an explicit Connect (rx 002) populates a source.
//
// No tallies broadcast — interrogate is a pure read.
func (s *server) handleCrosspointInterrogate(f iprobel.Frame) (handlerResult, error) {
	p, err := iprobel.DecodeCrosspointInterrogate(f)
	if err != nil {
		return handlerResult{}, err
	}
	src, ok := s.tree.currentSource(p.MatrixID, p.LevelID, p.DestinationID)
	if !ok {
		src = 0
	}
	reply := iprobel.EncodeCrosspointTally(iprobel.CrosspointTallyParams{
		MatrixID:      p.MatrixID,
		LevelID:       p.LevelID,
		DestinationID: p.DestinationID,
		SourceID:      src,
	})
	return handlerResult{reply: &reply}, nil
}
