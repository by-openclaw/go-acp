package probel

import (
	iprobel "acp/internal/probel"
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
func (s *server) handleSalvoGo(f iprobel.Frame) (handlerResult, error) {
	p, err := iprobel.DecodeSalvoGo(f)
	if err != nil {
		return handlerResult{}, err
	}
	var status iprobel.SalvoGoDoneStatus
	switch p.Op {
	case iprobel.SalvoOpSet:
		applied := s.tree.salvoApply(p.SalvoID)
		if applied == 0 {
			status = iprobel.SalvoDoneNone
		} else {
			status = iprobel.SalvoDoneSet
		}
	case iprobel.SalvoOpClear:
		cleared := s.tree.salvoClear(p.SalvoID)
		if cleared == 0 {
			status = iprobel.SalvoDoneNone
		} else {
			status = iprobel.SalvoDoneCleared
		}
	}
	reply := iprobel.EncodeSalvoGoDoneAck(iprobel.SalvoGoDoneAckParams{
		Status: status, SalvoID: p.SalvoID,
	})
	return handlerResult{reply: &reply}, nil
}
