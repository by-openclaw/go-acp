package acp1

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// InsertTiming selects the durations the slot state machine uses for
// the no_card -> powerup -> boot -> present cascade.
type InsertTiming int

const (
	// InsertTimingReal uses spec-typical durations (1-3s per phase) so
	// the emulator visually matches a real Synapse hot-plug.
	InsertTimingReal InsertTiming = iota

	// InsertTimingFast uses 50ms per phase so CI integration tests
	// finish quickly without losing the cascade structure.
	InsertTimingFast
)

func (t InsertTiming) String() string {
	switch t {
	case InsertTimingReal:
		return "real"
	case InsertTimingFast:
		return "fast"
	}
	return fmt.Sprintf("timing(%d)", t)
}

// ParseInsertTiming translates the producer CLI flag value.
func ParseInsertTiming(s string) (InsertTiming, error) {
	switch s {
	case "", "real":
		return InsertTimingReal, nil
	case "fast":
		return InsertTimingFast, nil
	}
	return InsertTimingReal, fmt.Errorf("unknown --insert-timing %q (use real / fast)", s)
}

func (t InsertTiming) powerupDuration() time.Duration {
	if t == InsertTimingFast {
		return 50 * time.Millisecond
	}
	return 1500 * time.Millisecond
}

func (t InsertTiming) bootDuration() time.Duration {
	if t == InsertTimingFast {
		return 50 * time.Millisecond
	}
	return 2500 * time.Millisecond
}

// slotStateMachine tracks per-slot pending transitions. Methods are
// safe for concurrent calls. Cancelling a slot's cascade stops any
// pending timer before it fires the next transition.
type slotStateMachine struct {
	mu      sync.Mutex
	pending map[uint8]context.CancelFunc
	timing  InsertTiming
}

func newSlotStateMachine(timing InsertTiming) *slotStateMachine {
	return &slotStateMachine{
		pending: map[uint8]context.CancelFunc{},
		timing:  timing,
	}
}

// SetTiming updates the cascade timing. Pending cascades continue with
// the timing they started with; new cascades use the new timing.
func (m *slotStateMachine) SetTiming(t InsertTiming) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.timing = t
}

// cancelPending stops any in-flight cascade for slot. Safe to call
// when nothing is pending.
func (m *slotStateMachine) cancelPending(slot uint8) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cancel, ok := m.pending[slot]; ok {
		cancel()
		delete(m.pending, slot)
	}
}

// trackCascade registers a cancel func for the given slot. Replaces
// any existing pending cascade.
func (m *slotStateMachine) trackCascade(slot uint8, cancel context.CancelFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if prev, ok := m.pending[slot]; ok {
		prev() // pre-empt any pending cascade for this slot
	}
	m.pending[slot] = cancel
}

func (m *slotStateMachine) clearCascade(slot uint8) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.pending, slot)
}

// CascadeInsert drives the no_card -> powerup -> boot -> present
// transition for one slot. Each phase emits a frame-status announce
// via setSlotStatus before the timer for the next phase fires.
//
// Pre-empts any in-flight cascade for the same slot.
//
// Returns immediately; the cascade runs on a background goroutine and
// completes after powerupDuration + bootDuration, OR aborts early if
// the parent context cancels (server stop, slot.extract during
// cascade, etc.).
func (s *server) CascadeInsert(parent context.Context, slot uint8) {
	machine := s.slotMachine
	if machine == nil {
		return
	}
	machine.cancelPending(slot)

	ctx, cancel := context.WithCancel(parent)
	machine.trackCascade(slot, cancel)

	timing := func() InsertTiming {
		machine.mu.Lock()
		defer machine.mu.Unlock()
		return machine.timing
	}()

	go func() {
		defer machine.clearCascade(slot)

		// Phase 1: no_card -> powerup (immediate).
		if err := s.setSlotStatus(slot, 1 /* powerup */); err != nil {
			s.logger.Warn("cascade powerup failed", "slot", slot, "err", err)
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(timing.powerupDuration()):
		}

		// Phase 2: powerup -> boot.
		if err := s.setSlotStatus(slot, 5 /* boot */); err != nil {
			s.logger.Warn("cascade boot failed", "slot", slot, "err", err)
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(timing.bootDuration()):
		}

		// Phase 3: boot -> present.
		if err := s.setSlotStatus(slot, 2 /* present */); err != nil {
			s.logger.Warn("cascade present failed", "slot", slot, "err", err)
			return
		}
	}()
}

// CascadeExtract drives the present -> removed -> no_card extraction.
// Both transitions fire immediately; spec p.20 has no required dwell
// time between them.
//
// Pre-empts any in-flight insert cascade for the same slot — pulling
// a card out mid-boot is a real-world scenario.
func (s *server) CascadeExtract(slot uint8) {
	machine := s.slotMachine
	if machine != nil {
		machine.cancelPending(slot)
	}
	if err := s.setSlotStatus(slot, 4 /* removed */); err != nil {
		s.logger.Warn("cascade removed failed", "slot", slot, "err", err)
		return
	}
	if err := s.setSlotStatus(slot, 0 /* no_card */); err != nil {
		s.logger.Warn("cascade no_card failed", "slot", slot, "err", err)
	}
}
