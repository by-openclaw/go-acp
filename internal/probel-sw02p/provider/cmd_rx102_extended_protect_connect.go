package probelsw02p

import (
	"acp/internal/probel-sw02p/codec"
)

// handleExtendedProtectConnect processes rx 102 Extended PROTECT
// CONNECT (§3.2.66). Controller / remote device asks the matrix to
// protect a destination on behalf of a given Device Number.
//
// Owner-only authority rule (memory/project_probel_extensions.md +
// §3.2.60 table):
//   - current=ProbelOverride → reject (spec: "Cannot be altered
//     remotely"). Fire ProtectOverrideImmutable.
//   - current=None → accept, owner = Device, state = ProtectProBel.
//   - current=Probel|OEM & Device == stored owner → accept, state
//     may be updated (idempotent on identical state).
//   - current=Probel|OEM & Device != stored owner → reject. Fire
//     ProtectUnauthorized.
//
// §3.2.61 requires tx 097 Extended PROTECT CONNECTED to broadcast
// on all ports on BOTH successful and unsuccessful attempts — the
// Protect details byte in tx 097 reports the actual state the
// destination ended up in. Reject paths therefore emit tx 097 with
// the unchanged prior state so every controller observes the
// no-change outcome.
func (s *server) handleExtendedProtectConnect(f codec.Frame) (handlerResult, error) {
	p, err := codec.DecodeExtendedProtectConnect(f)
	if err != nil {
		return handlerResult{}, err
	}
	entry, result := s.tree.protectApply(p.Destination, p.Device, codec.ProtectProBel)
	switch result {
	case protectApplyRejectedOwner:
		s.profile.Note(ProtectUnauthorized)
	case protectApplyRejectedOverride:
		s.profile.Note(ProtectOverrideImmutable)
	}
	br := codec.EncodeExtendedProtectConnected(codec.ExtendedProtectConnectedParams{
		Protect:     entry.State,
		Destination: p.Destination,
		Device:      entry.OwnerDevice,
	})
	return handlerResult{broadcast: []codec.Frame{br}}, nil
}
