package probelsw02p

import (
	"acp/internal/probel-sw02p/codec"
)

// handleExtendedConnectOnGoGroupSalvo processes rx 71 Extended CONNECT
// ON GO GROUP SALVO (§3.2.53). Same semantics as rx 35 — stage one
// crosspoint into the SalvoID-keyed pending buffer, emit tx 72 ack —
// but the wire form uses separate Destination/Source Multipliers
// (§3.2.47/§3.2.48) so dst/src ranges go up to 16383 instead of 1023.
//
// The pending buffer slot type (pendingSlot) is wide enough for the
// extended range (uint16), so rx 71 and rx 35 share the same
// pendingGroups map unchanged — the later rx 36 GO GROUP SALVO fires
// both narrow and extended stages of the same SalvoID together.
func (s *server) handleExtendedConnectOnGoGroupSalvo(f codec.Frame) (handlerResult, error) {
	p, err := codec.DecodeExtendedConnectOnGoGroupSalvo(f)
	if err != nil {
		return handlerResult{}, err
	}
	s.tree.appendPendingGroup(0, 0, p.SalvoID, pendingSlot{
		Destination: p.Destination,
		Source:      p.Source,
	})
	// Request and ack share an identical layout (§3.2.53 == §3.2.54):
	// same 5 bytes, same field widths, different cmd ID. A direct
	// struct conversion avoids hand-copying each field and is what
	// staticcheck flags as the idiomatic form.
	ack := codec.EncodeExtendedConnectOnGoGroupSalvoAck(codec.ExtendedConnectOnGoGroupSalvoAckParams(p))
	return handlerResult{reply: &ack}, nil
}
