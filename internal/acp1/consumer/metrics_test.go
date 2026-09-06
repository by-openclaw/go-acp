package acp1

import (
	"context"
	"errors"
	"testing"

	"dhs/internal/metrics"
	"dhs/internal/plugin"
)

// Every connector is supposed to expose live frame and byte counters; this
// one exposed none, so `--metrics-addr` served a scrape with no acp1 series
// in it at all.
func TestPluginExposesMetrics(t *testing.T) {
	p := (&Factory{}).New(plugin.Deps{Logger: discardLogger()}).(*Plugin)
	if p.Metrics() == nil {
		t.Fatal("Metrics must be non-nil — WithDefaults always fills it")
	}
}

// The hooks are built in one place so all three transports count and
// timestamp identically; whichever one a session resolves to, the counters
// mean the same thing.
func TestClientHooksCountAndTimestampBothWays(t *testing.T) {
	p := &Plugin{tsSink: &timestampSink{}, met: metrics.NewConnector()}
	cfg := p.clientHooks()

	cfg.OnRx(41)
	cfg.OnTx(7)

	snap := p.met.Snapshot()
	if snap.RxFrames != 1 || snap.RxBytes != 41 {
		t.Errorf("rx = %d frames / %d bytes, want 1 / 41", snap.RxFrames, snap.RxBytes)
	}
	if snap.TxFrames != 1 || snap.TxBytes != 7 {
		t.Errorf("tx = %d frames / %d bytes, want 1 / 7", snap.TxFrames, snap.TxBytes)
	}
	if p.tsSink.lastRx().IsZero() || p.tsSink.lastTx().IsZero() {
		t.Error("the hooks must stamp the liveness sink as well as count")
	}
}

// A Plugin built as a bare struct literal — which is how most of this
// package's tests build one — has neither a sink nor a connector, and the
// hooks must stay a no-op rather than a nil dereference.
func TestClientHooksToleratesNoSinkOrConnector(t *testing.T) {
	cfg := (&Plugin{}).clientHooks()
	cfg.OnRx(10)
	cfg.OnTx(10)
}

// errTransport fails whichever direction the test asks it to.
type errTransport struct{ sendErr, recvErr error }

func (e errTransport) Send(context.Context, []byte) error { return e.sendErr }
func (e errTransport) Receive(context.Context, int) ([]byte, error) {
	if e.recvErr != nil {
		return nil, e.recvErr
	}
	return []byte{1, 2, 3}, nil
}
func (e errTransport) Close() error { return nil }

// The UDP tap counts a frame only once it is actually on the wire. Counting
// a failed Send would report traffic that never left.
func TestTimestampingTransportIgnoresAFailedSend(t *testing.T) {
	met := metrics.NewConnector()
	tr := &timestampingTransport{
		inner: errTransport{sendErr: errors.New("no route")},
		sink:  &timestampSink{},
		met:   met,
	}

	if err := tr.Send(context.Background(), []byte{1, 2, 3}); err == nil {
		t.Fatal("Send must surface the transport error")
	}
	if snap := met.Snapshot(); snap.TxFrames != 0 || snap.TxBytes != 0 {
		t.Errorf("a failed send counted %d frames / %d bytes", snap.TxFrames, snap.TxBytes)
	}
	if !tr.sink.lastTx().IsZero() {
		t.Error("a failed send is not wire activity")
	}
}

func TestTimestampingTransportCountsBothDirections(t *testing.T) {
	met := metrics.NewConnector()
	tr := &timestampingTransport{inner: errTransport{}, sink: &timestampSink{}, met: met}

	if err := tr.Send(context.Background(), []byte{1, 2, 3, 4}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if _, err := tr.Receive(context.Background(), 16); err != nil {
		t.Fatalf("Receive: %v", err)
	}

	snap := met.Snapshot()
	if snap.TxFrames != 1 || snap.TxBytes != 4 {
		t.Errorf("tx = %d / %d, want 1 / 4", snap.TxFrames, snap.TxBytes)
	}
	if snap.RxFrames != 1 || snap.RxBytes != 3 {
		t.Errorf("rx = %d / %d, want 1 / 3", snap.RxFrames, snap.RxBytes)
	}
}

// A read that fails is not wire activity either.
func TestTimestampingTransportIgnoresAFailedReceive(t *testing.T) {
	met := metrics.NewConnector()
	tr := &timestampingTransport{
		inner: errTransport{recvErr: errors.New("timeout")},
		sink:  &timestampSink{},
		met:   met,
	}

	if _, err := tr.Receive(context.Background(), 16); err == nil {
		t.Fatal("Receive must surface the transport error")
	}
	if snap := met.Snapshot(); snap.RxFrames != 0 {
		t.Errorf("a failed receive counted %d frames", snap.RxFrames)
	}
}

// The tap works without a connector: the sink is still stamped, so liveness
// survives a Plugin that was never given metrics.
func TestTimestampingTransportWithoutAConnector(t *testing.T) {
	tr := &timestampingTransport{inner: errTransport{}, sink: &timestampSink{}}
	if err := tr.Send(context.Background(), []byte{1}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if _, err := tr.Receive(context.Background(), 16); err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if tr.sink.lastRx().IsZero() || tr.sink.lastTx().IsZero() {
		t.Error("the sink must be stamped even with no connector")
	}
}
