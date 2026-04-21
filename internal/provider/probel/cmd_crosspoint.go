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

// --- rx 002 / rx 0x82 : Crosspoint Connect -> tx 004 Connected + tx 003 Tally

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
func (s *server) handleCrosspointConnect(f iprobel.Frame) (handlerResult, error) {
	p, err := iprobel.DecodeCrosspointConnect(f)
	if err != nil {
		return handlerResult{}, err
	}
	if err := s.tree.applyConnect(p.MatrixID, p.LevelID, p.DestinationID, p.SourceID); err != nil {
		return handlerResult{}, err
	}
	reply := iprobel.EncodeCrosspointConnected(iprobel.CrosspointConnectedParams{
		MatrixID:      p.MatrixID,
		LevelID:       p.LevelID,
		DestinationID: p.DestinationID,
		SourceID:      p.SourceID,
	})
	tally := iprobel.EncodeCrosspointTally(iprobel.CrosspointTallyParams{
		MatrixID:      p.MatrixID,
		LevelID:       p.LevelID,
		DestinationID: p.DestinationID,
		SourceID:      p.SourceID,
	})
	return handlerResult{reply: &reply, tallies: []iprobel.Frame{tally}}, nil
}
