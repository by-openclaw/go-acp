package plugin

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"dhs/internal/clock"
	"dhs/internal/metrics"
	"dhs/internal/transport"
)

// A connector always receives a usable set, whatever the caller left out —
// otherwise every constructor would carry its own nil checks, which is the
// duplication this struct exists to remove.
func TestWithDefaultsFillsEverything(t *testing.T) {
	got := Deps{}.WithDefaults()
	if got.Logger == nil {
		t.Error("Logger must default")
	}
	if got.Net == nil {
		t.Error("Net must default")
	}
	if got.Clock == nil {
		t.Error("Clock must default")
	}
	if got.Metrics == nil {
		t.Error("Metrics must default")
	}
}

// What the caller supplied is kept — a test that injects a fake clock or a
// recording transport must get that one back, not a fresh default.
func TestWithDefaultsKeepsWhatWasGiven(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	net := transport.New(transport.Config{NoDelay: true})
	clk := clock.NewFake(time.Unix(0, 0))
	met := metrics.NewConnector()

	got := Deps{Logger: log, Net: net, Clock: clk, Metrics: met}.WithDefaults()
	if got.Logger != log {
		t.Error("Logger was replaced")
	}
	if got.Net != net {
		t.Error("Net was replaced")
	}
	if got.Clock != clk {
		t.Error("Clock was replaced")
	}
	if got.Metrics != met {
		t.Error("Metrics was replaced")
	}
}

// Each field defaults on its own, so supplying one does not suppress the
// others.
func TestWithDefaultsFillsFieldsIndependently(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	got := Deps{Clock: clk}.WithDefaults()
	if got.Clock != clk {
		t.Error("the supplied Clock was replaced")
	}
	if got.Logger == nil || got.Net == nil || got.Metrics == nil {
		t.Error("the remaining fields must still default")
	}
}

// WithDefaults returns a copy: filling defaults must not mutate the caller's
// value, or a Deps reused across two connectors would silently share whatever
// the first one had filled in.
func TestWithDefaultsDoesNotMutateTheReceiver(t *testing.T) {
	original := Deps{}
	_ = original.WithDefaults()
	if original.Logger != nil || original.Net != nil ||
		original.Clock != nil || original.Metrics != nil {
		t.Error("WithDefaults mutated the value it was called on")
	}
}
