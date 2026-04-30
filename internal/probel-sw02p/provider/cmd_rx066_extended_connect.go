package probelsw02p

import (
	"acp/internal/probel-sw02p/codec"
)

// handleExtendedConnect processes rx 66 Extended CONNECT (§3.2.48).
// Extended-addressing equivalent of handleConnect — the route applies
// immediately on (matrix=0, level=0) and the matrix broadcasts tx 68
// Extended CONNECTED on all ports (§3.2.50). Out-of-range slots are
// lenient-dropped with no broadcast so a rogue controller cannot
// fabricate a tally.
//
// Protect gating (§3.2.60): same rule as the narrow rx 02 path —
// reject when the destination's current protect state is non-None,
// fire ProtectBlocksConnect, and (when an existing route is
// recorded) emit a tx 68 state-echo broadcast so the controller sees
// that the crosspoint did not change. See cmd_rx002_connect.go for
// the rationale.
func (s *server) handleExtendedConnect(f codec.Frame) (handlerResult, error) {
	p, err := codec.DecodeExtendedConnect(f)
	if err != nil {
		return handlerResult{}, err
	}
	if entry, ok := s.tree.protectLookup(p.Destination); ok && entry.State != codec.ProtectNone {
		s.profile.Note(ProtectBlocksConnect)
		if curSrc, hasRoute := s.tree.lookupSource(0, 0, p.Destination); hasRoute {
			s.profile.Note(ProtectBlocksConnectStateEchoed)
			echo := codec.EncodeExtendedConnected(codec.ExtendedConnectedParams{
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
	br := codec.EncodeExtendedConnected(codec.ExtendedConnectedParams{
		Destination: p.Destination,
		Source:      p.Source,
	})
	return handlerResult{broadcast: []codec.Frame{br}}, nil
}
