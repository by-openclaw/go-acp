package probel

import (
	iprobel "acp/internal/probel"
)

// handleSalvoGroupInterrogate: rx 124 → tx 125. Returns one salvo slot
// per call; callers iterate by bumping ConnectIndex until Validity
// switches from ValidMore to ValidLast (or Invalid when nothing is
// stored).
//
// Reference: SW-P-08 §3.2.31 (rx 124) → §3.3.26 (tx 125).
func (s *server) handleSalvoGroupInterrogate(f iprobel.Frame) (handlerResult, error) {
	p, err := iprobel.DecodeSalvoGroupInterrogate(f)
	if err != nil {
		return handlerResult{}, err
	}
	slot, ok, last := s.tree.salvoSlotAt(p.SalvoID, p.ConnectIndex)
	if !ok {
		reply := iprobel.EncodeSalvoGroupTally(iprobel.SalvoGroupTallyParams{
			SalvoID:      p.SalvoID,
			ConnectIndex: p.ConnectIndex,
			Validity:     iprobel.SalvoTallyInvalid,
		})
		return handlerResult{reply: &reply}, nil
	}
	v := iprobel.SalvoTallyValidMore
	if last {
		v = iprobel.SalvoTallyValidLast
	}
	reply := iprobel.EncodeSalvoGroupTally(iprobel.SalvoGroupTallyParams{
		MatrixID:      slot.matrix,
		LevelID:       slot.level,
		DestinationID: slot.dst,
		SourceID:      slot.src,
		SalvoID:       p.SalvoID,
		ConnectIndex:  p.ConnectIndex,
		Validity:      v,
	})
	return handlerResult{reply: &reply}, nil
}
