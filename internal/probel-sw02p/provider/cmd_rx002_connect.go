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
func (s *server) handleConnect(f codec.Frame) (handlerResult, error) {
	p, err := codec.DecodeConnect(f)
	if err != nil {
		return handlerResult{}, err
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
