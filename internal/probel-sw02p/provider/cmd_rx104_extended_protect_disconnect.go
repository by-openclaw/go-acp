package probelsw02p

import (
	"acp/internal/probel-sw02p/codec"
)

// handleExtendedProtectDisconnect processes rx 104 Extended PROTECT
// DIS-CONNECT (§3.2.68). Controller asks the matrix to clear
// protection from a destination.
//
// Authority ladder (owner-only per memory/project_probel_extensions.md
// and §3.2.60):
//   - current=None → accept (no-op), emit tx 098 with State=None.
//   - current=ProbelOverride → reject (§3.2.60 "Cannot be altered
//     remotely"). Fire ProtectOverrideImmutable.
//   - current=Probel|OEM & Device == stored owner → accept, clear
//     entry, emit tx 098 with State=None.
//   - current=Probel|OEM & Device != stored owner → reject. Fire
//     ProtectUnauthorized.
//
// §3.2.62 requires tx 098 Extended PROTECT DIS-CONNECTED to
// broadcast on all ports on BOTH successful and unsuccessful
// attempts. Reject paths echo the unchanged state + owner so every
// controller observes the no-change outcome.
func (s *server) handleExtendedProtectDisconnect(f codec.Frame) (handlerResult, error) {
	p, err := codec.DecodeExtendedProtectDisconnect(f)
	if err != nil {
		return handlerResult{}, err
	}
	entry, result := s.tree.protectClear(p.Destination, p.Device)
	switch result {
	case protectApplyRejectedOwner:
		s.profile.Note(ProtectUnauthorized)
	case protectApplyRejectedOverride:
		s.profile.Note(ProtectOverrideImmutable)
	}
	br := codec.EncodeExtendedProtectDisconnected(codec.ExtendedProtectDisconnectedParams{
		Protect:     entry.State,
		Destination: p.Destination,
		Device:      entry.OwnerDevice,
	})
	return handlerResult{broadcast: []codec.Frame{br}}, nil
}
