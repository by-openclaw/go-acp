package tsl

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"dhs/internal/plugin"
)

// Every connector is supposed to expose live frame and byte counters; this
// one exposed none, so `--metrics-addr` served a scrape with no tsl
// series in it at all.
func TestPluginExposesMetrics(t *testing.T) {
	p := (&Factory{version: V31}).New(plugin.Deps{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}).(*Plugin)
	if p.Metrics() == nil {
		t.Fatal("Metrics must be non-nil — WithDefaults always fills it")
	}
}

// noteRx is the one tap both session kinds fire, so UDP and TCP report
// identically: it stamps liveness AND counts the frame.
func TestNoteRxCountsAndStampsLiveness(t *testing.T) {
	p := (&Factory{version: V31}).New(plugin.Deps{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}).(*Plugin)
	p.Opened("udp", "", 0, nil)

	p.noteRx(24)
	p.noteRx(8)

	snap := p.Metrics().Snapshot()
	if snap.RxFrames != 2 || snap.RxBytes != 32 {
		t.Errorf("rx = %d frames / %d bytes, want 2 / 32", snap.RxFrames, snap.RxBytes)
	}
	if got := p.SessionHealth(context.Background()); !got.Live {
		t.Errorf("a received packet makes the session live, got %+v", got)
	}
}

// A Plugin built as a bare struct literal has no connector, and the tap must
// stay a no-op rather than a nil dereference.
func TestNoteRxToleratesNoConnector(t *testing.T) {
	(&Plugin{}).noteRx(16)
}
