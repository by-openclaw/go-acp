package probelsw08p

import (
	"acp/internal/probel-sw08p/codec"
)

// handleSalvoGo: rx 121 → tx 123 (+ spontaneous tx 004 per applied slot on Set).
//
// Sets or clears the stored salvo crosspoints and reports final status
// via tx 123 to the originator.
//
// On Set, every crosspoint the matrix applied is also announced as a
// tx 004 Crosspoint Connected — to every session, originator included.
// §3.2.3 defines cmd 04 as "issued spontaneously by the controller on
// all ports after it has confirmation that a route has been made" and
// SW-P-08 controllers (Lawo VSM, Commie, …) rely on cmd 04 to refresh
// their tally UI. §3.2.30's note "No individual CONNECTED messages
// (Command Byte 04) are issued" together with "listening devices
// should use cmd 122 and cmd 123" is not honoured in practice by any
// shipping controller, so leaving cmd 04 suppressed leaves every peer
// blind. We follow §3.2.3 and fire `SalvoEmittedConnected` once per
// applied slot so the deviation is auditable.
//
// Status values per §3.3.25:
//   - 0x00 Crosspoints set
//   - 0x01 Stored crosspoints cleared
//   - 0x02 No crosspoints to set / clear
//
// Reference: SW-P-08 §3.2.30 (rx 121) → §3.3.25 (tx 123) + §3.2.3 (tx 04).
func (s *server) handleSalvoGo(f codec.Frame) (handlerResult, error) {
	p, err := codec.DecodeSalvoGo(f)
	if err != nil {
		return handlerResult{}, err
	}
	var (
		status    codec.SalvoGoDoneStatus
		connected []codec.Frame
	)
	switch p.Op {
	case codec.SalvoOpSet:
		applied := s.tree.salvoApply(p.SalvoID)
		if len(applied) == 0 {
			status = codec.SalvoDoneNone
		} else {
			status = codec.SalvoDoneSet
			connected = make([]codec.Frame, 0, len(applied))
			for _, slot := range applied {
				connected = append(connected, codec.EncodeCrosspointConnected(codec.CrosspointConnectedParams{
					MatrixID:      slot.matrix,
					LevelID:       slot.level,
					DestinationID: slot.dst,
					SourceID:      slot.src,
				}))
				s.profile.Note(SalvoEmittedConnected)
			}
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
	res := handlerResult{reply: &reply, tallies: connected}
	if len(connected) > 0 {
		res.streamToSender = func(emit func(codec.Frame) error) error {
			for _, f := range connected {
				if err := emit(f); err != nil {
					return err
				}
			}
			return nil
		}
	}
	return res, nil
}
