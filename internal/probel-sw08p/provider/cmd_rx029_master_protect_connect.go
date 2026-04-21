package probel

import (
	"acp/internal/probel-sw08p/codec"
)

// handleMasterProtectConnect is Protect Connect with override=true —
// seizes the protect even from ProtectProbelOver. Emits the same
// tx 013 Protect Connected reply + tx 011 Protect Tally broadcast
// as the regular Protect Connect handler.
//
// Reference: SW-P-08 §3.2 (rx 029) → §3.3 (tx 013 + tx 011).
func (s *server) handleMasterProtectConnect(f codec.Frame) (handlerResult, error) {
	p, err := codec.DecodeMasterProtectConnect(f)
	if err != nil {
		return handlerResult{}, err
	}
	state := uint8(codec.ProtectProbelOver)
	if err := s.tree.applyProtectConnect(p.MatrixID, p.LevelID, p.DestinationID, p.DeviceID, state, true); err != nil {
		return handlerResult{}, err
	}
	body := codec.ProtectTallyParams{
		MatrixID:      p.MatrixID,
		LevelID:       p.LevelID,
		DestinationID: p.DestinationID,
		DeviceID:      p.DeviceID,
		State:         codec.ProtectProbelOver,
	}
	reply := codec.EncodeProtectConnected(body)
	tally := codec.EncodeProtectTally(body)
	return handlerResult{reply: &reply, tallies: []codec.Frame{tally}}, nil
}
