package acp1

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"dhs/internal/consumer"
)

func TestPlugin_SatisfiesHealthChecker(t *testing.T) {
	var _ consumer.HealthChecker = (*Plugin)(nil)
}

func TestSessionHealth_DefaultsWhenNotConnected(t *testing.T) {
	p := &Plugin{logger: slog.Default()}

	// Use a context with a sub-millisecond deadline to fail-fast on
	// the Reachable probe (no host, no port set anyway).
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	h := p.SessionHealth(ctx)
	if h.Reachable {
		t.Errorf("Reachable = true on disconnected plugin")
	}
	if h.Connected {
		t.Errorf("Connected = true on disconnected plugin")
	}
	if h.Live {
		t.Errorf("Live = true on disconnected plugin")
	}
	if !h.LastRx.IsZero() || !h.LastTx.IsZero() {
		t.Errorf("Timestamps non-zero on disconnected plugin: rx=%v tx=%v", h.LastRx, h.LastTx)
	}
	if h.StaleAfter != acp1StaleAfter {
		t.Errorf("StaleAfter = %v, want %v", h.StaleAfter, acp1StaleAfter)
	}
}

func TestSessionHealth_LiveAfterRecentRx(t *testing.T) {
	p := &Plugin{
		logger: slog.Default(),
		tsSink: &timestampSink{},
	}
	p.tsSink.recordRx()
	p.tsSink.recordTx()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	h := p.SessionHealth(ctx)
	if !h.Live {
		t.Errorf("Live = false despite fresh rx; LastRx=%v", h.LastRx)
	}
	if h.LastRx.IsZero() {
		t.Errorf("LastRx zero after recordRx()")
	}
	if h.LastTx.IsZero() {
		t.Errorf("LastTx zero after recordTx()")
	}
}

func TestSessionHealth_StaleAfterWindow(t *testing.T) {
	p := &Plugin{
		logger: slog.Default(),
		tsSink: &timestampSink{},
	}
	// Forge a stale rx timestamp 100s ago > 90s threshold.
	p.tsSink.lastRxNS.Store(time.Now().Add(-100 * time.Second).UnixNano())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	h := p.SessionHealth(ctx)
	if h.Live {
		t.Errorf("Live = true despite stale rx (100s ago)")
	}
	if h.LastRx.IsZero() {
		t.Errorf("LastRx zero")
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

func TestProbeReachable_NoListener(t *testing.T) {
	// Pick a definitely-closed port: bind+release then probe.
	// This is racy in theory; in practice an ephemeral port immediately
	// after release is closed.
	const closedPort = 1 // privileged + unlikely to listen
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if probeReachable(ctx, "127.0.0.1", closedPort) {
		t.Fatalf("probe returned reachable for closed port")
	}
}

func TestItoaPort(t *testing.T) {
	cases := []struct {
		in   int
		want string
	}{
		{0, "0"},
		{1, "1"},
		{2071, "2071"},
		{65535, "65535"},
	}
	for _, tc := range cases {
		if got := itoaPort(tc.in); got != tc.want {
			t.Errorf("itoaPort(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
