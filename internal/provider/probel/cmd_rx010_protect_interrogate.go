package probel

import (
	"acp/internal/protocol/probel/codec"
)

// handleProtectInterrogate replies with the current protect state of
// (matrix, level, dst). Unknown destinations report ProtectNone with
// deviceID=0 (canonical "no protect").
//
// Reference: SW-P-08 §3.2 (rx 010 / rx 0x8A) → §3.3 (tx 011 / tx 0x8B).
func (s *server) handleProtectInterrogate(f codec.Frame) (handlerResult, error) {
	p, err := codec.DecodeProtectInterrogate(f)
	if err != nil {
		return handlerResult{}, err
	}
	rec := s.tree.protectAt(p.MatrixID, p.LevelID, p.DestinationID)
	reply := codec.EncodeProtectTally(codec.ProtectTallyParams{
		MatrixID:      p.MatrixID,
		LevelID:       p.LevelID,
		DestinationID: p.DestinationID,
		DeviceID:      rec.deviceID,
		State:         codec.ProtectState(rec.state),
	})
	return handlerResult{reply: &reply}, nil
}
