package probel

import (
	"acp/internal/protocol/probel/codec"
)

// handleSalvoGroupInterrogate: rx 124 → tx 125. Returns one salvo slot
// per call; callers iterate by bumping ConnectIndex until Validity
// switches from ValidMore to ValidLast (or Invalid when nothing is
// stored).
//
// Reference: SW-P-08 §3.2.31 (rx 124) → §3.3.26 (tx 125).
func (s *server) handleSalvoGroupInterrogate(f codec.Frame) (handlerResult, error) {
	p, err := codec.DecodeSalvoGroupInterrogate(f)
	if err != nil {
		return handlerResult{}, err
	}
	slot, ok, last := s.tree.salvoSlotAt(p.SalvoID, p.ConnectIndex)
	if !ok {
		reply := codec.EncodeSalvoGroupTally(codec.SalvoGroupTallyParams{
			SalvoID:      p.SalvoID,
			ConnectIndex: p.ConnectIndex,
			Validity:     codec.SalvoTallyInvalid,
		})
		return handlerResult{reply: &reply}, nil
	}
	v := codec.SalvoTallyValidMore
	if last {
		v = codec.SalvoTallyValidLast
	}
	reply := codec.EncodeSalvoGroupTally(codec.SalvoGroupTallyParams{
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
