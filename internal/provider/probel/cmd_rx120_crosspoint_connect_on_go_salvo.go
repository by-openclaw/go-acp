package probel

import (
	iprobel "acp/internal/probel"
)

// handleSalvoConnectOnGo: rx 120 → tx 122. Appends the crosspoint to
// the named salvo group and echoes it back as an acknowledge.
//
// Reference: SW-P-08 §3.2.29 (rx 120) → §3.3.24 (tx 122).
func (s *server) handleSalvoConnectOnGo(f iprobel.Frame) (handlerResult, error) {
	p, err := iprobel.DecodeSalvoConnectOnGo(f)
	if err != nil {
		return handlerResult{}, err
	}
	s.tree.salvoAppend(p.SalvoID, salvoSlot{
		matrix: p.MatrixID,
		level:  p.LevelID,
		dst:    p.DestinationID,
		src:    p.SourceID,
	})
	ack := iprobel.EncodeSalvoConnectOnGoAck(iprobel.SalvoConnectOnGoAckParams(p))
	return handlerResult{reply: &ack}, nil
}
