package acp1

import (
	"fmt"
	"sort"

	"dhs/internal/acp1/codec"
	"dhs/internal/export/canonical"
)

// MarkTreeSlotsPresent marks every slot that carries card objects in the
// served tree as "present" in the rack-controller frame-status object,
// synthesising that object if absent. It derives the populated slots from the
// tree itself rather than an explicit list, so a multi-card frame served via
// --tree (a hand-authored tree.json with several slots) reports a correct
// frame-status without a manifest — fixing the case where consumers saw an
// empty rack despite per-slot identities being served. Slots with no card
// objects stay no_card. Idempotent: a no-card frame leaves frame-status alone.
func (s *server) MarkTreeSlotsPresent() error {
	s.tree.mu.RLock()
	slots := make([]uint8, 0, len(s.tree.slots))
	for sl, counts := range s.tree.slots {
		if counts.hasCard() {
			slots = append(slots, sl)
		}
	}
	s.tree.mu.RUnlock()
	sort.Slice(slots, func(i, j int) bool { return slots[i] < slots[j] })
	return s.MarkSlotsPresent(slots)
}

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
