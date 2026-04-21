package probel

import (
	iprobel "acp/internal/probel"
)

// handleProtectDisconnect releases the protect iff the caller owns it.
// Replies tx 015 Protect Disconnected with the final (now-None) state
// and broadcasts tx 011 Protect Tally to other sessions so they see
// the clear.
//
// Reference: SW-P-08 §3.2 (rx 014 / rx 0x8E) → §3.3 (tx 015 + tx 011).
func (s *server) handleProtectDisconnect(f iprobel.Frame) (handlerResult, error) {
	p, err := iprobel.DecodeProtectDisconnect(f)
	if err != nil {
		return handlerResult{}, err
	}
	if err := s.tree.applyProtectDisconnect(p.MatrixID, p.LevelID, p.DestinationID, p.DeviceID); err != nil {
		return handlerResult{}, err
	}
	body := iprobel.ProtectTallyParams{
		MatrixID:      p.MatrixID,
		LevelID:       p.LevelID,
		DestinationID: p.DestinationID,
		DeviceID:      p.DeviceID,
		State:         iprobel.ProtectNone,
	}
	reply := iprobel.EncodeProtectDisconnected(body)
	tally := iprobel.EncodeProtectTally(body)
	return handlerResult{reply: &reply, tallies: []iprobel.Frame{tally}}, nil
}
