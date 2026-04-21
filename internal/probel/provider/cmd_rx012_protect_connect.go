package probel

import (
	"acp/internal/probel/codec"
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
func (s *server) handleProtectConnect(f codec.Frame) (handlerResult, error) {
	p, err := codec.DecodeProtectConnect(f)
	if err != nil {
		return handlerResult{}, err
	}
	state := uint8(codec.ProtectProbel)
	if err := s.tree.applyProtectConnect(p.MatrixID, p.LevelID, p.DestinationID, p.DeviceID, state, false); err != nil {
		return handlerResult{}, err
	}
	body := codec.ProtectTallyParams{
		MatrixID:      p.MatrixID,
		LevelID:       p.LevelID,
		DestinationID: p.DestinationID,
		DeviceID:      p.DeviceID,
		State:         codec.ProtectProbel,
	}
	reply := codec.EncodeProtectConnected(body)
	tally := codec.EncodeProtectTally(body)
	return handlerResult{reply: &reply, tallies: []codec.Frame{tally}}, nil
}
