package probelsw02p

import (
	"acp/internal/probel-sw02p/codec"
)

// handleGo processes rx 06 GO (§3.2.8). Drains the matrix's pending
// salvo buffer, then either applies every slot (op=set) or drops them
// (op=clear). In the set path the handler broadcasts one tx 04
// CONNECTED per applied slot to every connected session plus a
// trailing tx 13 GO DONE ACKNOWLEDGE; in the clear path only tx 13
// is emitted.
//
// Spec deviation (intentional, same as SW-P-08 issue #92):
//
//	§3.2.8 notes "No individual CONNECTED messages are issued" and
//	asks controllers to re-INTERROGATE the matrix after GO. Real
//	controllers (Lawo VSM, Commie) never implement that listener
//	path — they keep their tally UI in sync exclusively from tx 04
//	CONNECTED broadcasts per §3.2.6 ("issued on ALL ports of the
//	interface device"). To match the de-facto contract, handleGo
//	emits tx 04 per slot on commit and fires
//	SalvoEmittedConnected per slot so every deviation is auditable.
func (s *server) handleGo(f codec.Frame) (handlerResult, error) {
	p, err := codec.DecodeGo(f)
	if err != nil {
		return handlerResult{}, err
	}

	pending := s.tree.drainPending(0, 0)

	switch p.Operation {
	case codec.GoOpSet:
		broadcast := make([]codec.Frame, 0, len(pending)+1)
		for _, slot := range pending {
			if !s.tree.applyConnectLenient(0, 0, slot.Destination, slot.Source) {
				continue
			}
			s.profile.Note(SalvoEmittedConnected)
			broadcast = append(broadcast, codec.EncodeConnected(codec.ConnectedParams{
				Destination: slot.Destination,
				Source:      slot.Source,
			}))
		}
		broadcast = append(broadcast, codec.EncodeGoDoneAck(codec.GoDoneAckParams{
			Operation: codec.GoOpSet,
		}))
		return handlerResult{broadcast: broadcast}, nil

	case codec.GoOpClear:
		// No tree mutation on clear — pending was already drained and
		// we just discard the slots. §3.2.15 asks for the ack on all
		// ports; fan-out via broadcast keeps every controller in sync.
		return handlerResult{
			broadcast: []codec.Frame{
				codec.EncodeGoDoneAck(codec.GoDoneAckParams{Operation: codec.GoOpClear}),
			},
		}, nil
	}

	// Unknown operation byte — spec allows 0x00 and 0x01 only. Absorb
	// via the compliance profile (spec-strict, no-workaround) and
	// reply with a neutral GoDoneAck(Set=0) so the controller does
	// not hang waiting for a response.
	s.profile.Note(HandlerDecodeFailed)
	return handlerResult{
		broadcast: []codec.Frame{
			codec.EncodeGoDoneAck(codec.GoDoneAckParams{Operation: codec.GoOpSet}),
		},
	}, nil
}
