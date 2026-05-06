package acp1

import (
	"fmt"

	"dhs/internal/acp1/codec"
)

// parseSlotStatus translates an admin-supplied state string into the
// numeric SlotStatus byte. Names follow the SlotState semantic vocabulary
// from #249 — "powerup" not "power_up", "boot" not "boot_mode" — so the
// admin CLI matches what watch / health renders.
func parseSlotStatus(s string) (uint8, error) {
	switch s {
	case "no_card":
		return 0, nil
	case "powerup":
		return 1, nil
	case "present":
		return 2, nil
	case "error":
		return 3, nil
	case "removed":
		return 4, nil
	case "boot":
		return 5, nil
	}
	return 0, fmt.Errorf("unknown slot state %q (use no_card / powerup / boot / present / error / removed)", s)
}

// setSlotStatus mutates the rack-controller's frame-status array for one
// slot and broadcasts an announce so subscribed consumers see the
// transition. Bypasses the normal SetValue path because frame-status is
// read-only over the wire — admin-driven mutation is intentional.
func (s *server) setSlotStatus(slot uint8, state uint8) error {
	if state > 5 {
		return fmt.Errorf("slot state %d out of range 0..5", state)
	}
	frameKey := objectKey{slot: 0, group: codec.GroupFrame, id: 0}

	s.tree.mu.Lock()
	e, ok := s.tree.entries[frameKey]
	if !ok || e.param == nil {
		s.tree.mu.Unlock()
		return fmt.Errorf("frame-status object (slot=0, group=frame, id=0) absent in tree")
	}

	statuses, ok := e.param.Value.([]any)
	if !ok {
		s.tree.mu.Unlock()
		return fmt.Errorf("frame-status value is %T, want []any", e.param.Value)
	}
	if int(slot) >= len(statuses) {
		s.tree.mu.Unlock()
		return fmt.Errorf("slot %d out of range (frame-status has %d slots)", slot, len(statuses))
	}
	statuses[slot] = int64(state)
	e.param.Value = statuses
	// Build the announce body while still holding the lock so the
	// slice contents are sampled atomically. Two concurrent
	// setSlotStatus calls (e.g. an in-flight cascade goroutine and a
	// synchronous CascadeExtract from SlotUnload) would otherwise
	// race on the shared backing array even though each takes
	// tree.mu — the loser's slice reference outlives the winner's
	// unlock.
	body := make([]byte, 1+len(statuses))
	body[0] = byte(len(statuses))
	for i, st := range statuses {
		switch v := st.(type) {
		case int64:
			body[1+i] = byte(v)
		case int:
			body[1+i] = byte(v)
		case float64:
			body[1+i] = byte(v)
		}
	}
	s.tree.mu.Unlock()
	ann := &codec.Message{
		MTID:     0,
		MType:    codec.MTypeAnnounce,
		MAddr:    0,
		MCode:    0,
		ObjGroup: codec.GroupFrame,
		ObjID:    0,
		Value:    body,
	}
	s.broadcastAnnounce(ann)
	return nil
}
