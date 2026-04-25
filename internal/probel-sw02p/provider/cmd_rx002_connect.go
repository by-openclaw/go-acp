package probelsw02p

import (
	"acp/internal/probel-sw02p/codec"
)

// handleConnect processes rx 02 CONNECT (§3.2.4). Controller requests
// a route and the matrix applies it immediately, then broadcasts
// tx 04 CROSSPOINT CONNECTED on all ports (§3.2.6) to confirm.
//
// SW-P-02 has no matrix / level byte — the route is recorded on
// (matrix=0, level=0). applyConnectLenient silently drops the slot if
// the canonical tree has declared dst/src counts that exclude the
// requested indices; in that case no tx 04 is emitted (matching the
// spec's "A device will make the route and respond" — no route, no
// response).
//
// Protect gating (§3.2.60): if the destination is currently
// Pro-Bel-protected, OEM-protected, or Override-protected, the
// connect is rejected — rx 02 carries no Device Number so the caller
// is anonymous and cannot satisfy the owner-only authority rule. The
// existing route stays intact and ProtectBlocksConnect is recorded.
//
// State echo (deviation, see ProtectBlocksConnectStateEchoed): when
// the rejected destination already has a route, the handler emits a
// tx 04 broadcast with the EXISTING (dst, src) so every connected
// controller sees that the crosspoint did not change to what was
// requested. This matches the salvo deviation precedent and gives
// real-world controllers immediate feedback that their rx 02 did not
// take effect, instead of forcing them to wait for the next periodic
// rx 01 INTERROGATE poll. If no existing route is recorded, no echo
// fires (there's no truthful tx 04 to send).
func (s *server) handleConnect(f codec.Frame) (handlerResult, error) {
	p, err := codec.DecodeConnect(f)
	if err != nil {
		return handlerResult{}, err
	}
	if entry, ok := s.tree.protectLookup(p.Destination); ok && entry.State != codec.ProtectNone {
		s.profile.Note(ProtectBlocksConnect)
		if curSrc, hasRoute := s.tree.lookupSource(0, 0, p.Destination); hasRoute {
			s.profile.Note(ProtectBlocksConnectStateEchoed)
			echo := codec.EncodeConnected(codec.ConnectedParams{
				Destination: p.Destination,
				Source:      curSrc,
			})
			return handlerResult{broadcast: []codec.Frame{echo}}, nil
		}
		return handlerResult{}, nil
	}
	if !s.tree.applyConnectLenient(0, 0, p.Destination, p.Source) {
		return handlerResult{}, nil
	}
	br := codec.EncodeConnected(codec.ConnectedParams{
		Destination: p.Destination,
		Source:      p.Source,
	})
	return handlerResult{broadcast: []codec.Frame{br}}, nil
}
