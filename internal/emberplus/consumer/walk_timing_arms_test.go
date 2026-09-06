package emberplus

// The arms of the injectable timings themselves. They exist so the rest of
// the suite can run fast; these prove the production defaults are still what
// a real device gets, without any test waiting for them.

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"
)

func TestEffectiveWriteTimeout(t *testing.T) {
	if got := (&Plugin{}).effectiveWriteTimeout(); got != defaultWriteTimeout {
		t.Errorf("zero writeTimeout = %s, want the %s default", got, defaultWriteTimeout)
	}
	p := &Plugin{writeTimeout: 40 * time.Millisecond}
	if got := p.effectiveWriteTimeout(); got != 40*time.Millisecond {
		t.Errorf("explicit writeTimeout = %s, want 40ms", got)
	}
}

// Walk pauses before its settle loop to let the first burst of elements
// arrive. A cancelled walk must stop there rather than sitting out the delay
// first — the difference between Ctrl-C being instant and taking half a
// second on every connector.
func TestWalkCancelDuringInitialDelay(t *testing.T) {
	addr, stop := startLoopbackProvider(t)
	defer stop()
	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)

	p := (&Factory{}).New(discardLogger()).(*Plugin)
	// Long enough that only the cancellation can end the delay.
	p.walkInitialDelay = 10 * time.Second

	if err := p.Connect(context.Background(), host, port); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = p.Disconnect() }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	_, err := p.Walk(ctx, 0)
	if err == nil {
		t.Fatal("Walk returned no error for a cancelled context")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("Walk took %s to notice a cancelled context", elapsed)
	}
}
