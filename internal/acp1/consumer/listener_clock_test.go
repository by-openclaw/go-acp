package acp1

import (
	"testing"
	"time"

	"dhs/internal/clock"
)

// The listener paces its receive-retry on an injected clock: SetClock wins
// once called, and a listener built without one falls back to the system
// clock rather than nil.
func TestListenerClockInjectedAndDefault(t *testing.T) {
	l := &Listener{}
	if l.clock() == nil {
		t.Fatal("a listener without SetClock must use the system clock, not nil")
	}
	fake := clock.NewFake(time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC))
	l.SetClock(fake)
	if l.clock() != fake {
		t.Fatalf("clock() = %T, want the injected fake", l.clock())
	}
}
