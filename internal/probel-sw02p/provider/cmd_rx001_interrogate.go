package probelsw02p

import (
	"acp/internal/probel-sw02p/codec"
)

// handleInterrogate processes rx 01 INTERROGATE (§3.2.3). Controller
// queries which source is currently routed to a destination; the
// matrix replies with tx 03 TALLY carrying the answer.
//
// SW-P-02 has no matrix / level byte — the scaffold tree stores all
// routes on (matrix=0, level=0). When the queried destination has no
// recorded route the reply encodes the §3.2.5 reserved sentinel
// (source = 1023 = codec.DestOutOfRangeSource). The BadSource /
// Crosspoint-Update-Disabled flag on the reply is currently always
// clear — per-device bad-source tracking is out of scope for this
// plugin until a real router reports it.
func (s *server) handleInterrogate(f codec.Frame) (handlerResult, error) {
	p, err := codec.DecodeInterrogate(f)
	if err != nil {
		return handlerResult{}, err
	}
	src, ok := s.tree.lookupSource(0, 0, p.Destination)
	if !ok {
		src = codec.DestOutOfRangeSource
	}
	reply := codec.EncodeTally(codec.TallyParams{
		Destination: p.Destination,
		Source:      src,
	})
	return handlerResult{reply: &reply}, nil
}
