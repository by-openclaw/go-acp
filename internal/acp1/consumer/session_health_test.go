package acp1

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"dhs/internal/consumer"
	"dhs/internal/plugin"
)

// The health LOGIC is specified once, in internal/consumer. What is ACP1's,
// and what these cover, is the stale window, the timestampSink as the time
// source, and which network Connect reports — the last being the thing that
// was wrong before: a live UDP session used to report Reachable=false.

func TestPlugin_SatisfiesHealthChecker(t *testing.T) {
	var _ consumer.HealthChecker = (*Plugin)(nil)
}

func newHealthPlugin() *Plugin {
	return (&Factory{}).New(plugin.Deps{Logger: slog.Default()}).(*Plugin)
}

func TestSessionHealthBeforeConnect(t *testing.T) {
	got := newHealthPlugin().SessionHealth(context.Background())
	if got.Reachable || got.Connected || got.Live {
		t.Errorf("nothing is open before Connect, got %+v", got)
	}
	if !got.LastRx.IsZero() || !got.LastTx.IsZero() {
		t.Errorf("timestamps must be zero, got rx=%v tx=%v", got.LastRx, got.LastTx)
	}
	if got.StaleAfter != acp1StaleAfter {
		t.Errorf("StaleAfter = %v, want the ACP1 window %v", got.StaleAfter, acp1StaleAfter)
	}
}

func TestSessionHealthReadsTheTimestampSink(t *testing.T) {
	p := newHealthPlugin()
	p.tsSink = &timestampSink{}
	p.Opened("udp", "10.6.250.105", 2071, p.tsSink)

	p.tsSink.recordRx()
	p.tsSink.recordTx()

	got := p.SessionHealth(context.Background())
	if !got.Live || got.LastRx.IsZero() || got.LastTx.IsZero() {
		t.Errorf("a fresh sink makes the session live with both instants, got %+v", got)
	}
	// The regression: a UDP session that is receiving is reachable. The old
	// code dialled TCP here and reported false.
	if !got.Reachable {
		t.Error("a UDP session receiving frames is reachable")
	}
}

func TestSessionHealthGoesStale(t *testing.T) {
	p := newHealthPlugin()
	p.tsSink = &timestampSink{}
	p.Opened("udp", "10.6.250.105", 2071, p.tsSink)
	p.tsSink.lastRxNS.Store(time.Now().Add(-100 * time.Second).UnixNano())

	got := p.SessionHealth(context.Background())
	if got.Live {
		t.Errorf("rx 100s ago is past the %v window", acp1StaleAfter)
	}
	if got.LastRx.IsZero() {
		t.Error("the instant is still reported, it is just old")
	}
}

// Whether a connect attempt means anything depends on the transport, which
// is what Connect passes through.
func TestTransportKindNetwork(t *testing.T) {
	for kind, want := range map[TransportKind]string{
		TransportUDP:       "udp",
		TransportTCPDirect: "tcp",
		TransportAN2:       "tcp",
		TransportAuto:      "udp",
	} {
		if got := kind.network(); got != want {
			t.Errorf("%v.network() = %q, want %q", kind, got, want)
		}
	}
}

func TestTimestampSink_Atomic(t *testing.T) {
	// Multiple goroutines hammer recordRx; final state is non-zero.
	s := &timestampSink{}
	done := make(chan struct{})
	for i := 0; i < 8; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 100; j++ {
				s.recordRx()
				s.recordTx()
			}
		}()
	}
	for i := 0; i < 8; i++ {
		<-done
	}
	if s.lastRx().IsZero() || s.lastTx().IsZero() {
		t.Fatal("timestamps zero after concurrent updates")
	}
}

func TestTimestampingTransport_TapsBothDirections(t *testing.T) {
	sink := &timestampSink{}
	stub := &stubTimestampInner{rxData: []byte{0x01, 0x02}}
	wrapped := &timestampingTransport{inner: stub, sink: sink}

	if err := wrapped.Send(context.Background(), []byte{0xAA}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if sink.lastTx().IsZero() {
		t.Fatal("lastTx not updated by Send")
	}

	got, err := wrapped.Receive(context.Background(), 16)
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("rx data = %v", got)
	}
	if sink.lastRx().IsZero() {
		t.Fatal("lastRx not updated by Receive")
	}
}

type stubTimestampInner struct {
	rxData []byte
	served bool
}

func (s *stubTimestampInner) Send(_ context.Context, _ []byte) error { return nil }
func (s *stubTimestampInner) Receive(_ context.Context, _ int) ([]byte, error) {
	if s.served {
		return nil, context.DeadlineExceeded
	}
	s.served = true
	return s.rxData, nil
}
func (s *stubTimestampInner) Close() error { return nil }
