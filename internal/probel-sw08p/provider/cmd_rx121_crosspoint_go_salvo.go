package probelsw08p

import (
	"acp/internal/probel-sw08p/codec"
)

// handleSalvoGo: rx 121 → tx 123. Sets or clears the stored salvo
// crosspoints and reports the final status via tx 123.
//
// Spec note: tx 123 status values per §3.3.25:
//   - 0x00 Crosspoints set
//   - 0x01 Stored crosspoints cleared
//   - 0x02 No crosspoints to set / clear
//
// "No individual CONNECTED messages (Command Byte 04) are issued"
// when a salvo fires — the controller must track state via tx 122 +
// tx 123 alone (spec §3.2.30).
//
// Reference: SW-P-08 §3.2.30 (rx 121) → §3.3.25 (tx 123).
func (s *server) handleSalvoGo(f codec.Frame) (handlerResult, error) {
	p, err := codec.DecodeSalvoGo(f)
	if err != nil {
		return handlerResult{}, err
	}
	var status codec.SalvoGoDoneStatus
	switch p.Op {
	case codec.SalvoOpSet:
		applied := s.tree.salvoApply(p.SalvoID)
		if applied == 0 {
			status = codec.SalvoDoneNone
		} else {
			status = codec.SalvoDoneSet
		}
	case codec.SalvoOpClear:
		cleared := s.tree.salvoClear(p.SalvoID)
		if cleared == 0 {
			status = codec.SalvoDoneNone
		} else {
			status = codec.SalvoDoneCleared
		}
	}
	reply := codec.EncodeSalvoGoDoneAck(codec.SalvoGoDoneAckParams{
		Status: status, SalvoID: p.SalvoID,
	})
	return handlerResult{reply: &reply}, nil
}
