package acp1

import (
	"fmt"
)

// PreloadSlot installs a card on the target slot at producer startup,
// before Serve binds the listening socket — so by the time consumers
// (Cerebrum, VSM Studio, our own watch) connect, the slot is already
// at "present" with the schema's identity / control / status objects
// fully served.
//
// Unlike SlotLoad which models a hot-plug insertion (no_card → powerup
// → boot → present cascade with announces fired at each step),
// PreloadSlot does an immediate ReplaceSlot + setSlotStatus(present).
// No transient states. No insertion/extraction events on the wire.
//
// Why it matters: external controllers cache a per-device template
// keyed by the schema observed at first walk. A cascade between two
// cold-add cycles looks like "card removed and replaced" to the
// controller, which discards the cached template and re-walks. That
// breaks the user's working session every restart. The simulator
// avoids the issue by booting with the full controller card already
// at slot 0 — PreloadSlot is the equivalent for our producer.
//
// Returns:
//
//	ErrNoDMLibrary       no resolver attached
//	devicemodel.ErrNotFound    card path does not resolve
//	tree.ReplaceSlot err underlying type / range conversion failure
func (s *server) PreloadSlot(slot uint8, cardPath string) error {
	s.mu.Lock()
	r := s.dmLibrary
	s.mu.Unlock()
	if r == nil {
		return ErrNoDMLibrary
	}
	fp, err := parseCardPath(cardPath)
	if err != nil {
		return err
	}
	schema, err := r.Resolve(fp)
	if err != nil {
		return fmt.Errorf("dmlib resolve %s: %w", cardPath, err)
	}
	snap := pickSnapshot(schema, int(slot))
	if snap == nil {
		return fmt.Errorf("acp1 provider: schema %s has no slot data", cardPath)
	}
	if err := s.tree.ReplaceSlot(slot, snap); err != nil {
		return fmt.Errorf("acp1 provider: ReplaceSlot: %w", err)
	}
	// Set frame-status[slot] = 2 (present) directly. Cascade is
	// intentionally skipped — preload is a deployment-time install,
	// not a hot-plug event. setSlotStatus emits a frame-status
	// announce, but the bcast socket isn't bound yet so the announce
	// is a no-op; consumers connect AFTER preload completes and walk
	// the present state.
	if err := s.setSlotStatus(slot, 2); err != nil {
		return fmt.Errorf("acp1 provider: setSlotStatus: %w", err)
	}
	return nil
}
