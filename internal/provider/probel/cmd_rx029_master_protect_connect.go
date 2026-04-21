package probel

import (
	iprobel "acp/internal/probel"
)

// handleMasterProtectConnect is Protect Connect with override=true —
// seizes the protect even from ProtectProbelOver. Emits the same
// tx 013 Protect Connected reply + tx 011 Protect Tally broadcast
// as the regular Protect Connect handler.
//
// Reference: SW-P-08 §3.2 (rx 029) → §3.3 (tx 013 + tx 011).
func (s *server) handleMasterProtectConnect(f iprobel.Frame) (handlerResult, error) {
	p, err := iprobel.DecodeMasterProtectConnect(f)
	if err != nil {
		return handlerResult{}, err
	}
	state := uint8(iprobel.ProtectProbelOver)
	if err := s.tree.applyProtectConnect(p.MatrixID, p.LevelID, p.DestinationID, p.DeviceID, state, true); err != nil {
		return handlerResult{}, err
	}
	body := iprobel.ProtectTallyParams{
		MatrixID:      p.MatrixID,
		LevelID:       p.LevelID,
		DestinationID: p.DestinationID,
		DeviceID:      p.DeviceID,
		State:         iprobel.ProtectProbelOver,
	}
	reply := iprobel.EncodeProtectConnected(body)
	tally := iprobel.EncodeProtectTally(body)
	return handlerResult{reply: &reply, tallies: []iprobel.Frame{tally}}, nil
}
