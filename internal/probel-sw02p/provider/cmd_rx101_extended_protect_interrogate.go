package probelsw02p

import (
	"acp/internal/probel-sw02p/codec"
)

// handleExtendedProtectInterrogate processes rx 101 Extended PROTECT
// INTERROGATE (§3.2.65). Controller queries the current protect
// status of a destination; matrix replies with tx 096 Extended
// PROTECT TALLY carrying the stored (State, Destination, Device)
// triple.
//
// Destinations with no protect entry reply with State=ProtectNone +
// Device=0 — that is the canonical "nothing protected here" shape
// and matches what a real router reports when the destination has
// never been the target of an rx 102 PROTECT CONNECT.
func (s *server) handleExtendedProtectInterrogate(f codec.Frame) (handlerResult, error) {
	p, err := codec.DecodeExtendedProtectInterrogate(f)
	if err != nil {
		return handlerResult{}, err
	}
	entry, _ := s.tree.protectLookup(p.Destination)
	reply := codec.EncodeExtendedProtectTally(codec.ExtendedProtectTallyParams{
		Protect:     entry.State,
		Destination: p.Destination,
		Device:      entry.OwnerDevice,
	})
	return handlerResult{reply: &reply}, nil
}
