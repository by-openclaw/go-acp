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
func (s *server) handleExtendedConnect(f codec.Frame) (handlerResult, error) {
	p, err := codec.DecodeExtendedConnect(f)
	if err != nil {
		return handlerResult{}, err
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
