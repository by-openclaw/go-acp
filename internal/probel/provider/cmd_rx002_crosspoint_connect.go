package probel

import (
	"acp/internal/probel/codec"
)

// handleCrosspointConnect decodes the connect request, records the
// routing in the server tree, and builds two frames:
//
//  1. tx 004 Connected back to the originator (confirmation the router
//     accepted the request and now reflects the new state).
//  2. tx 003 Tally fanned out to every OTHER active session so all
//     controllers watching the bus see the crosspoint change.
//
// If the tree rejects the connect (unknown matrix/level, dst or src
// out of range), the handler returns an error; the session logs it and
// does NOT fan out a tally — SW-P-08 has no dedicated error reply for
// a bad Connect, so the originator learns via timeout.
//
// Reference: SW-P-08 §3.2 (rx 002 / rx 0x82) → §3.3 (tx 004 + tx 003).
func (s *server) handleCrosspointConnect(f codec.Frame) (handlerResult, error) {
	p, err := codec.DecodeCrosspointConnect(f)
	if err != nil {
		return handlerResult{}, err
	}
	if err := s.tree.applyConnect(p.MatrixID, p.LevelID, p.DestinationID, p.SourceID); err != nil {
		return handlerResult{}, err
	}
	reply := codec.EncodeCrosspointConnected(codec.CrosspointConnectedParams{
		MatrixID:      p.MatrixID,
		LevelID:       p.LevelID,
		DestinationID: p.DestinationID,
		SourceID:      p.SourceID,
	})
	tally := codec.EncodeCrosspointTally(codec.CrosspointTallyParams{
		MatrixID:      p.MatrixID,
		LevelID:       p.LevelID,
		DestinationID: p.DestinationID,
		SourceID:      p.SourceID,
	})
	return handlerResult{reply: &reply, tallies: []codec.Frame{tally}}, nil
}
