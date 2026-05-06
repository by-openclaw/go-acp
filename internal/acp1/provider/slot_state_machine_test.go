package acp1

import (
	"context"
	"sync"
	"testing"
	"time"

	"dhs/internal/acp1/codec"
)

// readSlotStatus extracts the current numeric status byte for a slot
// from the rack-controller frame-status object.
func readSlotStatus(t *testing.T, s *server, slot uint8) uint8 {
	t.Helper()
	s.tree.mu.RLock()
	defer s.tree.mu.RUnlock()
	e := s.tree.entries[objectKey{slot: 0, group: codec.GroupFrame, id: 0}]
	if e == nil || e.param == nil {
		t.Fatalf("frame-status object absent")
	}
	statuses, ok := e.param.Value.([]any)
	if !ok || int(slot) >= len(statuses) {
		t.Fatalf("frame-status shape unexpected: %T (slot %d, len %d)", e.param.Value, slot, len(statuses))
	}
	switch v := statuses[slot].(type) {
	case int64:
		return uint8(v)
	case int:
		return uint8(v)
	case float64:
		return uint8(v)
	}
	t.Fatalf("status type %T", statuses[slot])
	return 0
}

func TestParseInsertTiming(t *testing.T) {
	cases := []struct {
		in   string
		want InsertTiming
		err  bool
	}{
		{"", InsertTimingReal, false},
		{"real", InsertTimingReal, false},
		{"fast", InsertTimingFast, false},
		{"slow", 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := ParseInsertTiming(tc.in)
			if tc.err {
				if err == nil {
					t.Fatalf("expected error for %q", tc.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestInsertTiming_Durations(t *testing.T) {
	if InsertTimingFast.powerupDuration() >= time.Second {
		t.Fatalf("fast powerup too slow: %v", InsertTimingFast.powerupDuration())
	}
	if InsertTimingFast.bootDuration() >= time.Second {
		t.Fatalf("fast boot too slow: %v", InsertTimingFast.bootDuration())
	}
	if InsertTimingReal.powerupDuration() < time.Second {
		t.Fatalf("real powerup too fast: %v", InsertTimingReal.powerupDuration())
	}
	if InsertTimingReal.bootDuration() < time.Second {
		t.Fatalf("real boot too fast: %v", InsertTimingReal.bootDuration())
	}
}

func TestCascadeInsert_Fast_ReachesPresent(t *testing.T) {
	s := newTestServer(t)
	s.SetInsertTiming(InsertTimingFast)

	// Initial: slot 1 starts as 2 (present, per fixture). Reset to no_card.
	if err := s.setSlotStatus(1, 0); err != nil {
		t.Fatalf("reset slot: %v", err)
	}

	s.CascadeInsert(context.Background(), 1)

	// Wait up to 500ms (fast = ~100ms total).
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if readSlotStatus(t, s, 1) == 2 /* present */ {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("cascade did not reach present: final state = %d", readSlotStatus(t, s, 1))
}

func TestCascadeInsert_PassesThroughEachPhase(t *testing.T) {
	s := newTestServer(t)
	s.SetInsertTiming(InsertTimingFast)
	if err := s.setSlotStatus(1, 0); err != nil {
		t.Fatalf("reset: %v", err)
	}

	// Capture the sequence by polling with high frequency. The poller
	// runs on its own goroutine, so guard the seen map with a mutex —
	// the read-back below would otherwise race the writer under -race.
	var seenMu sync.Mutex
	seen := map[uint8]bool{}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(2 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				st := readSlotStatus(t, s, 1)
				seenMu.Lock()
				seen[st] = true
				seenMu.Unlock()
			}
		}
	}()

	s.CascadeInsert(context.Background(), 1)
	time.Sleep(300 * time.Millisecond)
	close(stop)
	<-done

	// We expect to have observed at least powerup (1) and boot (5) and
	// present (2). Initial no_card (0) may also be present depending on
	// race but is not required.
	seenMu.Lock()
	defer seenMu.Unlock()
	if !seen[1] {
		t.Errorf("never observed powerup state")
	}
	if !seen[5] {
		t.Errorf("never observed boot state")
	}
	if !seen[2] {
		t.Errorf("never observed present state")
	}
}

func TestCascadeInsert_Cancellation(t *testing.T) {
	s := newTestServer(t)
	s.SetInsertTiming(InsertTimingReal) // slow enough to cancel mid-flight
	if err := s.setSlotStatus(1, 0); err != nil {
		t.Fatalf("reset: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.CascadeInsert(ctx, 1)

	// Let it advance to powerup (immediate) and start the powerup timer.
	time.Sleep(50 * time.Millisecond)
	cancel()

	// Wait past where boot should have fired (~1500ms). Status should
	// stay at powerup (1) since the cancel pre-empted the timer.
	time.Sleep(1800 * time.Millisecond)
	if got := readSlotStatus(t, s, 1); got >= 5 {
		t.Fatalf("cascade continued past cancel: state = %d", got)
	}
}

func TestCascadeExtract_Immediate(t *testing.T) {
	s := newTestServer(t)
	// fixture starts slot 1 at present (2).
	s.CascadeExtract(1)

	// Both transitions happen synchronously inside CascadeExtract.
	if got := readSlotStatus(t, s, 1); got != 0 {
		t.Fatalf("after extract: state = %d, want no_card (0)", got)
	}
}

func TestCascadeInsert_PreemptsExistingCascade(t *testing.T) {
	s := newTestServer(t)
	s.SetInsertTiming(InsertTimingReal)
	if err := s.setSlotStatus(1, 0); err != nil {
		t.Fatalf("reset: %v", err)
	}

	// Start one cascade.
	s.CascadeInsert(context.Background(), 1)
	// Wait for powerup phase.
	time.Sleep(50 * time.Millisecond)

	// Switch to fast and re-trigger.
	s.SetInsertTiming(InsertTimingFast)
	s.CascadeInsert(context.Background(), 1)

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if readSlotStatus(t, s, 1) == 2 /* present */ {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("preempted cascade did not reach present: state = %d", readSlotStatus(t, s, 1))
}

func TestSlotStateMachine_TimingMutation(t *testing.T) {
	m := newSlotStateMachine(InsertTimingReal)
	if m.timing != InsertTimingReal {
		t.Fatalf("default timing = %v, want real", m.timing)
	}
	m.SetTiming(InsertTimingFast)
	if m.timing != InsertTimingFast {
		t.Fatalf("after SetTiming(fast) = %v", m.timing)
	}
}
