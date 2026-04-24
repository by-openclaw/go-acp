package probelsw02p

import (
	"acp/internal/probel-sw02p/codec"
)

// handleGoGroupSalvo processes rx 36 GO GROUP SALVO (§3.2.37). Drains
// the matrix's SalvoID-keyed pending buffer, then either applies
// every slot (op=Set) or drops them (op=Clear). On set, N × tx 04
// CONNECTED broadcast to every session plus a trailing tx 38 GO DONE
// GROUP SALVO ACKNOWLEDGE. On clear, only tx 38.
//
// §3.2.39 adds a third status "No crosspoints to set / clear" (02) so
// a controller can tell whether its GO hit a non-empty group — we
// emit that whenever drainPendingGroup returns an empty slice, in
// both the set and clear paths.
//
// Spec deviation — same as handleGo / SW-P-08 issue #92: emit tx 04
// per applied slot even though §3.2.37 says "no individual CONNECTED
// messages are issued". Real controllers (Lawo VSM, Commie) rely on
// tx 04 broadcasts for their tally UI. Deviation observable via
// SalvoEmittedConnected.
func (s *server) handleGoGroupSalvo(f codec.Frame) (handlerResult, error) {
	p, err := codec.DecodeGoGroupSalvo(f)
	if err != nil {
		return handlerResult{}, err
	}

	pending := s.tree.drainPendingGroup(0, 0, p.SalvoID)

	switch p.Operation {
	case codec.GoOpSet:
		if len(pending) == 0 {
			return handlerResult{
				broadcast: []codec.Frame{
					codec.EncodeGoDoneGroupSalvoAck(codec.GoDoneGroupSalvoAckParams{
						Result: codec.GoGroupResultEmpty, SalvoID: p.SalvoID,
					}),
				},
			}, nil
		}
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
		broadcast = append(broadcast, codec.EncodeGoDoneGroupSalvoAck(codec.GoDoneGroupSalvoAckParams{
			Result:  codec.GoGroupResultSet,
			SalvoID: p.SalvoID,
		}))
		return handlerResult{broadcast: broadcast}, nil

	case codec.GoOpClear:
		result := codec.GoGroupResultCleared
		if len(pending) == 0 {
			result = codec.GoGroupResultEmpty
		}
		return handlerResult{
			broadcast: []codec.Frame{
				codec.EncodeGoDoneGroupSalvoAck(codec.GoDoneGroupSalvoAckParams{
					Result: result, SalvoID: p.SalvoID,
				}),
			},
		}, nil
	}

	// Unknown operation byte — absorb, emit neutral ack.
	s.profile.Note(HandlerDecodeFailed)
	return handlerResult{
		broadcast: []codec.Frame{
			codec.EncodeGoDoneGroupSalvoAck(codec.GoDoneGroupSalvoAckParams{
				Result: codec.GoGroupResultEmpty, SalvoID: p.SalvoID,
			}),
		},
	}, nil
}
