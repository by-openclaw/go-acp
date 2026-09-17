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

// Every connector is supposed to expose live document and byte counters;
// this one exposed none, so `--metrics-addr` served a scrape with no
// cerebrum-nb series in it at all.
func TestPluginExposesMetrics(t *testing.T) {
	if newHealthPlugin().Metrics() == nil {
		t.Fatal("Metrics must be non-nil — WithDefaults always fills it")
	}
}

// A Session built directly by a test has no connector, and counting must
// stay a no-op rather than a nil dereference.
func TestSessionWithoutAConnector(t *testing.T) {
	s := &Session{}
	if s.met != nil {
		t.Error("a bare Session has no connector")
	}
	s.noteRX() // must not panic
}

// Both directions are counted against a fake Cerebrum: the XML document
// roundTrip writes, and the one readLoop reads back. Counting the ws TEXT
// payload rather than the RFC 6455 framing keeps the numbers the same wire
// truth --capture records.
func TestSessionCountsBothDirections(t *testing.T) {
	fs := newFakeCerebrum(t, func(fc *fakeConn) {
		drainClient(fc, func(frame []byte) ([]byte, bool) {
			// Any well-formed document will do; the counters are about
			// bytes on the wire, not about what the reply means.
			return []byte(`<CEREBRUM><NACK MTID="1" CODE="1"/></CEREBRUM>`), true
		})
	})

	p := (&Factory{}).New(plugin.Deps{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}).(*Plugin)
	ctx, cancel := ctx2s(t)
	defer cancel()
	if err := p.Connect(ctx, fs.host(), fs.port()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = p.Disconnect() })

	// The verb's own outcome is irrelevant — a NACK reply is a perfectly
	// good round trip as far as the wire is concerned.
	_ = p.Session().ObtainDatastore(ctx, "ROUTER")

	snap := p.Metrics().Snapshot()
	if snap.TxFrames == 0 || snap.TxBytes == 0 {
		t.Errorf("nothing counted on the write path: tx=%d/%d", snap.TxFrames, snap.TxBytes)
	}
	if snap.RxFrames == 0 || snap.RxBytes == 0 {
		t.Errorf("nothing counted on the read path: rx=%d/%d", snap.RxFrames, snap.RxBytes)
	}
}
