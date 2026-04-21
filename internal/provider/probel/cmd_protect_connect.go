package probel

import (
	iprobel "acp/internal/probel"
)

// handleProtectConnect installs a Probel-class protect on (matrix,
// level, dst) for the requesting device. Replies tx 013 Protect
// Connected to the originator and broadcasts tx 011 Protect Tally to
// other sessions so every controller sees the state change.
//
// Failure modes: out-of-range dst or an existing override-protect
// return the error through the handler; no broadcast happens.
//
// Reference: SW-P-08 §3.2 (rx 012 / rx 0x8C) → §3.3 (tx 013 + tx 011).
func (s *server) handleProtectConnect(f iprobel.Frame) (handlerResult, error) {
	p, err := iprobel.DecodeProtectConnect(f)
	if err != nil {
		return handlerResult{}, err
	}
	state := uint8(iprobel.ProtectProbel)
	if err := s.tree.applyProtectConnect(p.MatrixID, p.LevelID, p.DestinationID, p.DeviceID, state, false); err != nil {
		return handlerResult{}, err
	}
	body := iprobel.ProtectTallyParams{
		MatrixID:      p.MatrixID,
		LevelID:       p.LevelID,
		DestinationID: p.DestinationID,
		DeviceID:      p.DeviceID,
		State:         iprobel.ProtectProbel,
	}
	reply := iprobel.EncodeProtectConnected(body)
	tally := iprobel.EncodeProtectTally(body)
	return handlerResult{reply: &reply, tallies: []iprobel.Frame{tally}}, nil
}
