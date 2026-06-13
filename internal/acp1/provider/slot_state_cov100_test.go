package acp1

import (
	"context"
	"io"
	"log/slog"
	"math/rand"
	"testing"
	"time"

	"dhs/internal/acp1/codec"
)

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// emptyTreeServer builds a server with a logger + slot machine but a tree
// that has NO frame-status object, so every setSlotStatus call fails.
func emptyTreeServer() *server {
	return &server{
		logger:      discardLogger(),
		slotMachine: newSlotStateMachine(InsertTimingFast),
		tree:        &tree{entries: map[objectKey]*entry{}, slots: map[uint8]*slotCounts{}},
	}
}

// TestCascadeInsert_NilMachine: a server with no slot machine returns early.
func TestCascadeInsert_NilMachine(t *testing.T) {
	s := &server{logger: discardLogger(),
		tree: &tree{entries: map[objectKey]*entry{}, slots: map[uint8]*slotCounts{}}}
	s.slotMachine = nil
	s.CascadeInsert(context.Background(), 1) // must not panic
}

// TestCascadeInsert_SetStatusFails: with no frame object, the cascade's
// powerup setSlotStatus fails and the goroutine logs + returns.
func TestCascadeInsert_SetStatusFails(t *testing.T) {
	s := emptyTreeServer()
	s.CascadeInsert(context.Background(), 1)
	time.Sleep(50 * time.Millisecond) // let the goroutine run + fail
}

// TestCascadeExtract_SetStatusFails: with no frame object, both extract
// transitions fail and log.
func TestCascadeExtract_SetStatusFails(t *testing.T) {
	s := emptyTreeServer()
	s.CascadeExtract(1)
}

// TestTrackCascade_PreemptsPrevious: registering a second cancel for the
// same slot cancels the first.
func TestTrackCascade_PreemptsPrevious(t *testing.T) {
	m := newSlotStateMachine(InsertTimingFast)
	preempted := false
	m.trackCascade(3, func() { preempted = true })
	m.trackCascade(3, func() {}) // second registration pre-empts the first
	if !preempted {
		t.Fatal("first cascade cancel was not invoked on pre-empt")
	}
}

// TestCascadeInsert_CancelDuringBoot: cancel the parent context after
// powerup completes but during the boot wait → the boot-phase ctx.Done arm.
func TestCascadeInsert_CancelDuringBoot(t *testing.T) {
	s := newTestServer(t)
	// Real timing: powerup 1.5s, boot 2.5s. Use a machine with a custom
	// timing where powerup is short and boot is long so we can cancel
	// reliably during the boot wait.
	s.slotMachine = newSlotStateMachine(InsertTimingFast) // powerup 50ms, boot 50ms
	_ = s.setSlotStatus(1, 0)
	ctx, cancel := context.WithCancel(context.Background())
	s.CascadeInsert(ctx, 1)
	// Cancel shortly after powerup (50ms) but the boot wait window — fast
	// timing makes this a tight race, so cancel a hair after powerup fires.
	time.Sleep(55 * time.Millisecond)
	cancel()
	time.Sleep(60 * time.Millisecond)
}

// corruptFrame replaces the frame-status Value with a non-[]any so every
// subsequent setSlotStatus fails.
func corruptFrame(s *server) {
	s.tree.mu.Lock()
	e := s.tree.entries[objectKey{slot: 0, group: codec.GroupFrame, id: 0}]
	if e != nil && e.param != nil {
		e.param.Value = "corrupted"
	}
	s.tree.mu.Unlock()
}

// TestCascadeInsert_BootPhaseFails: corrupt the frame after powerup succeeds
// but before the boot phase fires, so the boot setSlotStatus fails.
func TestCascadeInsert_BootPhaseFails(t *testing.T) {
	s := newTestServer(t)
	s.slotMachine = newSlotStateMachine(InsertTimingFast) // powerup 50ms, boot 50ms
	_ = s.setSlotStatus(1, 0)
	s.CascadeInsert(context.Background(), 1)
	// Powerup fires immediately; corrupt during the ~50ms powerup wait so
	// the boot-phase setSlotStatus (at ~50ms) fails.
	time.Sleep(20 * time.Millisecond)
	corruptFrame(s)
	time.Sleep(80 * time.Millisecond)
}

// TestCascadeInsert_PresentPhaseFails: corrupt the frame after boot but
// before present, so the present setSlotStatus fails.
func TestCascadeInsert_PresentPhaseFails(t *testing.T) {
	s := newTestServer(t)
	s.slotMachine = newSlotStateMachine(InsertTimingFast)
	_ = s.setSlotStatus(1, 0)
	s.CascadeInsert(context.Background(), 1)
	// powerup ~0ms, boot ~50ms, present ~100ms. Corrupt at ~75ms.
	time.Sleep(75 * time.Millisecond)
	corruptFrame(s)
	time.Sleep(60 * time.Millisecond)
}

// TestCascadeExtract_NoCardPhaseFails: the no_card phase fails after the
// removed phase succeeds, via a hook that fails on its 2nd call.
func TestCascadeExtract_NoCardPhaseFails(t *testing.T) {
	s := emptyTreeServer()
	calls := 0
	s.setStatusHook = func(slot, state uint8) error {
		calls++
		if calls == 1 {
			return nil // removed succeeds
		}
		return errSyntheticStatus // no_card fails
	}
	s.CascadeExtract(1)
	if calls < 2 {
		t.Fatalf("expected 2 setStatus calls, got %d", calls)
	}
}

// TestFrameStatusTick_SetFailsViaHook: a hook that always fails drives
// frameStatusTick's set-failure return.
func TestFrameStatusTick_SetFailsViaHook(t *testing.T) {
	s := newTestServer(t) // has a 4-slot frame so n>0
	s.setStatusHook = func(uint8, uint8) error { return errSyntheticStatus }
	r := rand.New(rand.NewSource(2))
	if _, _, ok := s.frameStatusTick(r); ok {
		t.Fatal("frameStatusTick with failing setStatus: want ok=false")
	}
}

var errSyntheticStatus = &syntheticStatusErr{}

type syntheticStatusErr struct{}

func (*syntheticStatusErr) Error() string { return "synthetic setStatus failure" }

// TestSetSlotStatus_BadValueType: a frame object whose Value isn't []any
// makes setSlotStatus fail.
func TestSetSlotStatus_BadValueType(t *testing.T) {
	s := newTestServer(t)
	s.tree.mu.Lock()
	e := s.tree.entries[objectKey{slot: 0, group: codec.GroupFrame, id: 0}]
	e.param.Value = "not-a-slice"
	s.tree.mu.Unlock()
	if err := s.setSlotStatus(1, 2); err == nil {
		t.Fatal("frame value not []any: want error")
	}
}

// TestSetSlotStatus_IntAndFloatElements: a frame array carrying int and
// float64 elements exercises both type switches in the announce body
// builder.
func TestSetSlotStatus_IntAndFloatElements(t *testing.T) {
	s := newTestServer(t)
	s.tree.mu.Lock()
	e := s.tree.entries[objectKey{slot: 0, group: codec.GroupFrame, id: 0}]
	// int at index 0 and float64 at index 2; mutate slot 3 so neither is
	// overwritten and both type-switch arms run in the announce builder.
	e.param.Value = []any{int(1), int64(0), float64(2), int64(0)}
	s.tree.mu.Unlock()
	if err := s.setSlotStatus(3, 2); err != nil {
		t.Fatalf("setSlotStatus: %v", err)
	}
}
