package consumer

import (
	"testing"
	"time"

	"dhs/internal/clock"
	"dhs/internal/plugin"
)

// The injected clock is what Clock() hands back after Init, so keepalive
// probers and poll loops run on test time; a Base never Init'ed still
// answers with a usable (system) clock rather than nil.
func TestClockInjectedAndDefault(t *testing.T) {
	fake := clock.NewFake(time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC))
	var b Base
	b.Init(plugin.Deps{Clock: fake}, 0)
	if b.Clock() != fake {
		t.Fatalf("Clock() = %T, want the injected fake", b.Clock())
	}

	var zero Base
	if zero.Clock() == nil {
		t.Fatal("a zero Base must default to the system clock, not nil")
	}
	if zero.Clock().Now().IsZero() {
		t.Error("the default clock must tell real time")
	}
}
