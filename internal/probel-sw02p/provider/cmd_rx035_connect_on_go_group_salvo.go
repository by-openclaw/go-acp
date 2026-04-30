package probelsw02p

import (
	"acp/internal/probel-sw02p/codec"
)

// handleConnectOnGoGroupSalvo processes rx 35 CONNECT ON GO GROUP
// SALVO (§3.2.36). Each frame stages one crosspoint into the matrix's
// per-SalvoID pending buffer; a later rx 36 GO GROUP SALVO commits or
// clears the chosen group. The matrix replies with tx 37 CONNECT ON
// GO GROUP SALVO ACKNOWLEDGE echoing dst / src / SalvoID.
//
// Per §3.2.36 "Destination and source will always overwrite previous
// data" — re-staging the same dst+src within one SalvoID is legal;
// the pending buffer keeps both entries and the set path applies the
// latest one last (map semantics of sources).
func (s *server) handleConnectOnGoGroupSalvo(f codec.Frame) (handlerResult, error) {
	p, err := codec.DecodeConnectOnGoGroupSalvo(f)
	if err != nil {
		return handlerResult{}, err
	}
	s.tree.appendPendingGroup(0, 0, p.SalvoID, pendingSlot{
		Destination: p.Destination,
		Source:      p.Source,
	})
	ack := codec.EncodeConnectOnGoGroupSalvoAck(codec.ConnectOnGoGroupSalvoAckParams{
		Destination: p.Destination,
		Source:      p.Source,
		SalvoID:     p.SalvoID,
	})
	return handlerResult{reply: &ack}, nil
}
