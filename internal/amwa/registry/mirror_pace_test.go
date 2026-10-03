package registry

import (
	"context"
	"testing"
	"time"

	"dhs/internal/clock"
)

// The pacer hands out a burst at once, then one token per interval;
// a cancelled context ends the wait.
func TestPacerSpreadsABurstOverTheRate(t *testing.T) {
	clk := clock.NewFake(time.Time{})
	p := newPacer(clk, 10) // 10/s: a token every 100 ms, burst 10
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		if err := p.wait(ctx); err != nil {
			t.Fatalf("token %d of the burst: %v", i, err)
		}
	}
	// The eleventh waits for the clock.
	done := make(chan error, 1)
	go func() { done <- p.wait(ctx) }()
	select {
	case err := <-done:
		t.Fatalf("the eleventh request must wait, got %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	for clk.Waiters() == 0 {
		time.Sleep(time.Millisecond)
	}
	clk.Advance(100 * time.Millisecond)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("after one interval: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the eleventh request never got its token")
	}
	// Refill is capped at the burst.
	clk.Advance(time.Hour)
	for i := 0; i < 10; i++ {
		if err := p.wait(ctx); err != nil {
			t.Fatalf("burst after an idle hour, token %d: %v", i, err)
		}
	}
	if p.tokens >= 1 {
		t.Fatalf("the bucket must be empty after a burst, has %.2f", p.tokens)
	}

	// A cancelled wait returns the context's error.
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if err := p.wait(cctx); err == nil {
		t.Fatal("a cancelled context must end the wait")
	}

	// Zero and negative rates take the default.
	if d := newPacer(clk, 0); d.rate != DefaultTargetPace {
		t.Fatalf("rate 0 = %v, want the default %d", d.rate, DefaultTargetPace)
	}
}

// A mirror that is stopping does not wait for a token: postResource
// returns on the context, with no request made.
func TestPostResourceReturnsWhenTheMirrorStops(t *testing.T) {
	m, err := NewMirror(MirrorOptions{Source: "http://127.0.0.1:1", Target: "http://127.0.0.1:2", TargetPace: 1})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	// Drain the one-token burst, then cancel: the next POST must not
	// be attempted (the target address answers nothing anyway, and a
	// request would be counted as a failure).
	if err := m.pace.wait(ctx); err != nil {
		t.Fatal(err)
	}
	cancel()
	m.postResource(ctx, "nodes", "v1.3", "11111111-1111-4111-8111-111111111111", []byte(`{"id":"x"}`), false)
	if st := m.Stats(); st.Failures != 0 || st.Forwarded != 0 {
		t.Fatalf("a stopping mirror made a request: %+v", st)
	}
}
