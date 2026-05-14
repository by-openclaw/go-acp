package acp1

import (
	"fmt"

	"dhs/internal/acp1/codec"
	"dhs/internal/export/canonical"
)

// MarkSlotsPresent synthesises (if absent) the rack-controller's
// frame-status object and marks each named slot as "present" (state=2).
//
// Used by the manifest-driven boot path: the canonical tree built
// from `.cache/manifest/<device>.json` + `.cache/dm/<proto>/` carries
// the per-slot DM trees but no Frame group at slot 0 — frame-status
// is runtime rack-controller state, not a card schema. Without this
// object SynapseSetUp and other ACP1 consumers see "no slot populated"
// and the rack appears empty.
//
// PreloadSlot is the equivalent for the legacy --dm-library +
// --preload flow; it embeds a ReplaceSlot. Manifest mode already
// replaced the slot via newTree, so we only need the frame-status
// half.
func (s *server) MarkSlotsPresent(slots []uint8) error {
	if len(slots) == 0 {
		return nil
	}
	maxSlot := uint8(0)
	for _, sl := range slots {
		if sl > maxSlot {
			maxSlot = sl
		}
	}
	s.tree.mu.Lock()
	frameKey := objectKey{slot: 0, group: codec.GroupFrame, id: 0}
	if _, ok := s.tree.entries[frameKey]; !ok {
		// Synthesise frame-status object with one byte per slot,
		// sized to cover the highest slot in the manifest. All
		// slots start as 0 (no_card); MarkSlotsPresent then flips
		// each declared slot to 2 (present).
		statuses := make([]any, int(maxSlot)+1)
		for i := range statuses {
			statuses[i] = int64(0)
		}
		access := "read"
		fr := &canonical.Parameter{
			Header: canonical.Header{
				Number:     0,
				Identifier: "frame-status",
				Path:       "device.slot-0.frame",
				OID:        "1.0.6.0",
				IsOnline:   true,
				Access:     access,
			},
			Type:  canonical.ParamOctets,
			Value: statuses,
		}
		fmtHint := "frame"
		fr.Format = &fmtHint
		s.tree.entries[frameKey] = &entry{
			key:     frameKey,
			param:   fr,
			acpType: codec.TypeFrame,
			access:  codec.AccessRead,
		}
		// Slot 0 must exist in t.slots for handleRoot to work; if
		// the manifest didn't declare slot 0 we still need it so
		// frame-status answers.
		if _, ok := s.tree.slots[0]; !ok {
			s.tree.slots[0] = &slotCounts{}
		}
	}
	s.tree.mu.Unlock()

	for _, sl := range slots {
		if err := s.setSlotStatus(sl, 2); err != nil {
			return fmt.Errorf("MarkSlotsPresent slot %d: %w", sl, err)
		}
	}
	return nil
}
