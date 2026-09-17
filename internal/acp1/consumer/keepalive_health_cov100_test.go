package acp1

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"dhs/internal/consumer"
)

// TestSessionLive_PrimedZeroRx covers SessionLive's last.IsZero arm: a
// configured timeout but no rx ever recorded → not live.
func TestSessionLive_PrimedZeroRx(t *testing.T) {
	p := &Plugin{
		tsSink: &timestampSink{},
		kaCfg:  consumer.KeepAliveConfig{Interval: time.Second, Timeout: 3 * time.Second},
	}
	if p.SessionLive() {
		t.Fatal("no rx recorded: SessionLive should be false")
	}
}

// TestStartKeepAlive_FullyDisabled covers the early return when both the
// prober and watchdog are disabled.
func TestStartKeepAlive_FullyDisabled(t *testing.T) {
	p := &Plugin{
		logger: slog.Default(),
		kaCfg:  consumer.KeepAliveConfig{Interval: consumer.DisableInterval, Timeout: consumer.DisableTimeout},
	}
	p.mu.Lock()
	p.startKeepAlive(context.Background())
	p.mu.Unlock()
	if p.ka != nil {
		t.Fatal("fully-disabled keepalive should not allocate state")
	}
}

// TestKeepAliveProber_NilClientExits drives the prober's c==nil exit arm.
func TestKeepAliveProber_NilClientExits(t *testing.T) {
	p := &Plugin{logger: slog.Default()}
	p.ka = &keepAliveState{done: make(chan struct{})}
	p.ka.stopped.Add(1)
	// client is nil → first tick takes the c==nil return.
	done := make(chan struct{})
	go func() {
		p.keepAliveProber(context.Background(), 5*time.Millisecond)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("prober did not exit on nil client")
	}
}

// TestKeepAliveProber_CtxCancelExits drives the prober's ctx.Done arm.
func TestKeepAliveProber_CtxCancelExits(t *testing.T) {
	ft := &fakeTransport{}
	p := &Plugin{logger: slog.Default(), client: NewClient(ft, nil, ClientConfig{})}
	p.ka = &keepAliveState{done: make(chan struct{})}
	p.ka.stopped.Add(1)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		p.keepAliveProber(ctx, time.Hour) // long interval; only ctx wakes it
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("prober did not exit on ctx cancel")
	}
}

// TestKeepAliveWatchdog_Transitions drives the went-silent then live-again
// log transitions of the watchdog loop.
func TestKeepAliveWatchdog_Transitions(t *testing.T) {
	p := &Plugin{
		logger: slog.Default(),
		tsSink: &timestampSink{},
		kaCfg:  consumer.KeepAliveConfig{Interval: 30 * time.Millisecond, Timeout: 90 * time.Millisecond},
	}
	p.tsSink.recordRx() // start live
	p.ka = &keepAliveState{done: make(chan struct{})}
	p.ka.stopped.Add(1)
	// The watchdog clamps its tick to a 1s floor (tick = timeout/3, min 1s),
	// so the SessionLive timeout must be > 1s for the loop to observe a
	// transition between ticks. kaCfg.Timeout=1500ms drives SessionLive;
	// the watchdog ticks every 1s.
	p.kaCfg = consumer.KeepAliveConfig{Interval: 500 * time.Millisecond, Timeout: 1500 * time.Millisecond}
	go p.keepAliveWatchdog(1500 * time.Millisecond)

	// Stay silent past the 1.5s timeout → "went silent" transition.
	time.Sleep(2500 * time.Millisecond)
	// Touch rx → "live again" transition on the next 1s tick.
	p.tsSink.recordRx()
	time.Sleep(1500 * time.Millisecond)

	close(p.ka.done)
	p.ka.stopped.Wait()
}
