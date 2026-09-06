package emberplus

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"dhs/internal/consumer"
	"dhs/internal/plugin"
)

// The health LOGIC is specified once, in internal/consumer. What is Ember+'s,
// and what this covers, is the dead-man window and the Session as the time
// source.
func TestPluginSatisfiesHealthChecker(t *testing.T) {
	var _ consumer.HealthChecker = (*Plugin)(nil)
}

func newHealthPlugin() *Plugin {
	return (&Factory{}).New(plugin.Deps{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}).(*Plugin)
}

func TestSessionHealthBeforeConnect(t *testing.T) {
	got := newHealthPlugin().SessionHealth(context.Background())
	if got.Connected || got.Live || got.Reachable {
		t.Errorf("nothing is open before Connect, got %+v", got)
	}
	if got.StaleAfter != emberStaleAfter {
		t.Errorf("StaleAfter = %v, want the Ember+ dead-man window %v", got.StaleAfter, emberStaleAfter)
	}
}

// touchRX is what the read loop calls on every decoded frame, so it is what
// liveness has to follow.
func TestSessionHealthFollowsTheSession(t *testing.T) {
	p := newHealthPlugin()
	s := NewSession(slog.New(slog.NewTextHandler(io.Discard, nil)))
	p.Opened("tcp", "10.0.0.1", 9000, sessionTimes{s})

	if got := p.SessionHealth(context.Background()); !got.Connected || got.Live {
		t.Errorf("open with nothing received yet, got %+v", got)
	}

	s.touchRX()
	got := p.SessionHealth(context.Background())
	if !got.Live || !got.Reachable {
		t.Errorf("a fresh frame makes the session live and reachable, got %+v", got)
	}
	if got.LastRx.IsZero() {
		t.Error("LastRx must come from the session")
	}
	// There is no single tx point in this session, so tx is reported as
	// unknown rather than invented.
	if !got.LastTx.IsZero() {
		t.Errorf("LastTx = %v, want unknown", got.LastTx)
	}
}

// rxAge collapses "nothing yet" to a zero duration, which reads as "just
// now" — which is why liveness needs the instant, not the age.
func TestSessionLastRxIsZeroBeforeAnyFrame(t *testing.T) {
	s := NewSession(slog.New(slog.NewTextHandler(io.Discard, nil)))
	if !s.LastRx().IsZero() {
		t.Error("LastRx must be zero before any frame")
	}
	if age := s.rxAge(); age != 0 {
		t.Errorf("rxAge = %v; the two answer different questions", age)
	}

	s.touchRX()
	if got := s.LastRx(); time.Since(got) > time.Minute {
		t.Errorf("LastRx = %v, want roughly now", got)
	}
}

func TestSessionHealthAfterDisconnect(t *testing.T) {
	p := newHealthPlugin()
	p.Opened("tcp", "10.0.0.1", 9000, sessionTimes{NewSession(nil)})
	p.Closed()
	if got := p.SessionHealth(context.Background()); got.Connected {
		t.Errorf("Connected must be false once the session is closed, got %+v", got)
	}
}
