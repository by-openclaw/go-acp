package probelsw02p

import (
	"acp/internal/probel-sw02p/codec"
)

// handleExtendedInterrogate processes rx 65 Extended INTERROGATE
// (§3.2.47). Functionally identical to rx 01 handleInterrogate but
// uses the tx 67 Extended TALLY reply shape so dst/src fit the 16383
// extended range. Unrouted destinations encode the narrow §3.2.5
// sentinel (Source = codec.DestOutOfRangeSource = 1023) for
// consistency; §3.2.49 does not spec its own extended sentinel.
func (s *server) handleExtendedInterrogate(f codec.Frame) (handlerResult, error) {
	p, err := codec.DecodeExtendedInterrogate(f)
	if err != nil {
		return handlerResult{}, err
	}
	src, ok := s.tree.lookupSource(0, 0, p.Destination)
	if !ok {
		src = codec.DestOutOfRangeSource
	}
	reply := codec.EncodeExtendedTally(codec.ExtendedTallyParams{
		Destination: p.Destination,
		Source:      src,
	})
	return handlerResult{reply: &reply}, nil
}
