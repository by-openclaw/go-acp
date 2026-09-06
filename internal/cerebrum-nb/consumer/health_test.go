package cerebrumnb

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"dhs/internal/consumer"
	"dhs/internal/plugin"
)

// The health LOGIC is specified once, in internal/consumer. What is
// Cerebrum's, and what this covers, is the stale window — the keep-alive
// timeout it already judges a dead link by — and the Session as the time
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
	if got.StaleAfter != defaultKeepAliveTimeout {
		t.Errorf("StaleAfter = %v, want the keep-alive timeout %v",
			got.StaleAfter, defaultKeepAliveTimeout)
	}
}

// noteRX is what the read loop calls on every inbound frame, so it is what
// liveness has to follow.
func TestSessionHealthFollowsTheSession(t *testing.T) {
	p := newHealthPlugin()
	s := &Session{}
	p.Opened("tcp", "10.6.250.5", 40009, sessionTimes{s})

	if got := p.SessionHealth(context.Background()); !got.Connected || got.Live {
		t.Errorf("open with nothing received yet, got %+v", got)
	}

	s.noteRX()
	got := p.SessionHealth(context.Background())
	if !got.Live || !got.Reachable {
		t.Errorf("a fresh frame makes the session live and reachable, got %+v", got)
	}
	if got.LastRx.IsZero() {
		t.Error("LastRx must come from the session")
	}
	// The session stamps rx only; there is no single write path, so tx is
	// reported as unknown rather than invented.
	if !got.LastTx.IsZero() {
		t.Errorf("LastTx = %v, want unknown", got.LastTx)
	}
}

func TestSessionHealthAfterDisconnect(t *testing.T) {
	p := newHealthPlugin()
	p.Opened("tcp", "10.6.250.5", 40009, sessionTimes{&Session{}})
	p.Closed()
	if got := p.SessionHealth(context.Background()); got.Connected {
		t.Errorf("Connected must be false once the session is closed, got %+v", got)
	}
}
