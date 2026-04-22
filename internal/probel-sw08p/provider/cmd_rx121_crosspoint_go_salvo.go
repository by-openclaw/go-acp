package probelsw08p

import (
	"acp/internal/probel-sw08p/codec"
)

// handleSalvoGo: rx 121 → tx 123. Sets or clears the stored salvo
// crosspoints and reports the final status via tx 123. On op=set we
// also emit tx 003 Crosspoint Tally per applied slot, delivered both
// to the salvo sender (via streamToSender) and fanned out to every
// other active session (via tallies).
//
// Spec note: §3.2.30 suppresses tx 004 Connected as a reply to salvo
// commits — BUT says nothing about tx 003 Tally, which is a
// state-change notification (a different command on the wire). Without
// the Tally broadcast controllers like Lawo VSM that batch-connect via
// salvo can't reconcile per-slot state and flip-flop between old/new
// in their UI until a tally-dump races to catch up. Our handler emits
// one tx 003 per applied slot to match the per-slot state visibility
// controllers get from cmd 002 Connect.
//
// tx 123 status values per §3.3.25:
//   - 0x00 Crosspoints set
//   - 0x01 Stored crosspoints cleared
//   - 0x02 No crosspoints to set / clear
//
// Reference: SW-P-08 §3.2.30 (rx 121) → §3.3.25 (tx 123) + §3.3.4 (tx 003).
func (s *server) handleSalvoGo(f codec.Frame) (handlerResult, error) {
	p, err := codec.DecodeSalvoGo(f)
	if err != nil {
		return handlerResult{}, err
	}
	var (
		status   codec.SalvoGoDoneStatus
		tallies  []codec.Frame
		toSender []codec.Frame
	)
	switch p.Op {
	case codec.SalvoOpSet:
		applied := s.tree.salvoApply(p.SalvoID)
		if len(applied) == 0 {
			status = codec.SalvoDoneNone
		} else {
			status = codec.SalvoDoneSet
			tallies = make([]codec.Frame, 0, len(applied))
			toSender = make([]codec.Frame, 0, len(applied))
			for _, r := range applied {
				tally := codec.EncodeCrosspointTally(codec.CrosspointTallyParams{
					MatrixID:      r.matrix,
					LevelID:       r.level,
					DestinationID: r.dst,
					SourceID:      r.src,
				})
				tallies = append(tallies, tally)  // fan-out to other sessions
				toSender = append(toSender, tally) // also back to the salvo originator
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
	res := handlerResult{reply: &reply, tallies: tallies}
	if len(toSender) > 0 {
		frames := toSender // captured by closure
		res.streamToSender = func(emit func(codec.Frame) error) error {
			for _, fr := range frames {
				if err := emit(fr); err != nil {
					return err
				}
			}
			return nil
		}
	}
	return res, nil
}
