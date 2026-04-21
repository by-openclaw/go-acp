package probel

import (
	"acp/internal/protocol/probel/codec"
)

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
//
// Reference: SW-P-08 §3.2 (rx 001 / rx 0x81) → §3.3 (tx 003 / tx 0x83).
func (s *server) handleCrosspointInterrogate(f codec.Frame) (handlerResult, error) {
	p, err := codec.DecodeCrosspointInterrogate(f)
	if err != nil {
		return handlerResult{}, err
	}
	src, ok := s.tree.currentSource(p.MatrixID, p.LevelID, p.DestinationID)
	if !ok {
		src = 0
	}
	reply := codec.EncodeCrosspointTally(codec.CrosspointTallyParams{
		MatrixID:      p.MatrixID,
		LevelID:       p.LevelID,
		DestinationID: p.DestinationID,
		SourceID:      src,
	})
	return handlerResult{reply: &reply}, nil
}
