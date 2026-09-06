package emberplus

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"dhs/internal/consumer"
	"dhs/internal/emberplus/codec/s101"
	"dhs/internal/metrics"
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

// Every connector is supposed to expose live frame and byte counters; this
// one exposed none, so `--metrics-addr` served a scrape with no emberplus
// series in it at all.
func TestPluginExposesMetrics(t *testing.T) {
	if newHealthPlugin().Metrics() == nil {
		t.Fatal("Metrics must be non-nil — WithDefaults always fills it")
	}
}

// Frames are attributed by S101 command byte, which separates EmBER payloads
// from keep-alives — the distinction that matters when reading a scrape,
// since a link can be busy with keep-alives and carrying no data at all.
func TestSetMetricsRegistersTheS101Commands(t *testing.T) {
	met := metrics.NewConnector()
	s := NewSession(slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.SetMetrics(met)

	names := met.Snapshot().CmdNames
	for cmd, want := range map[byte]string{
		s101.CmdEmBER:         "ember",
		s101.CmdKeepAliveReq:  "keepalive-req",
		s101.CmdKeepAliveResp: "keepalive-resp",
	} {
		if got := names[cmd]; got != want {
			t.Errorf("S101 command 0x%02x registered as %q, want %q", cmd, got, want)
		}
	}
}

// A Session built directly by a test has no connector, and counting must
// stay a no-op rather than a nil dereference.
func TestSetMetricsIgnoresNil(t *testing.T) {
	s := NewSession(slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.SetMetrics(nil)
	if s.metricsConn() != nil {
		t.Error("a nil connector must not be stored")
	}
}

// Nothing is counted for a frame that never went out: sendEmBER on a session
// with no writer fails before the wire.
func TestSendEmBERCountsNothingWhenNotConnected(t *testing.T) {
	met := metrics.NewConnector()
	s := NewSession(slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.SetMetrics(met)

	if err := s.sendEmBER([]byte{0x01}); err == nil {
		t.Fatal("sendEmBER must fail with no writer")
	}
	if snap := met.Snapshot(); snap.TxFrames != 0 {
		t.Errorf("counted %d frames for a send that never happened", snap.TxFrames)
	}
}
