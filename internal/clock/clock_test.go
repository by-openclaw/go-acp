package clock

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestFakeNowAdvance(t *testing.T) {
	start := time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC)
	f := NewFake(start)

	if got := f.Now(); !got.Equal(start) {
		t.Fatalf("Now() = %v, want %v", got, start)
	}
	f.Advance(90 * time.Second)
	if got, want := f.Now(), start.Add(90*time.Second); !got.Equal(want) {
		t.Fatalf("after Advance, Now() = %v, want %v", got, want)
	}
}

func TestFakeNewFakeZeroStartIsStable(t *testing.T) {
	f := NewFake(time.Time{})
	want := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if got := f.Now(); !got.Equal(want) {
		t.Fatalf("zero start = %v, want fixed %v", got, want)
	}
}

func TestFakeAfterFiresOnlyWhenDue(t *testing.T) {
	f := NewFake(time.Time{})
	ch := f.After(30 * time.Second)

	f.Advance(29 * time.Second)
	select {
	case <-ch:
		t.Fatal("After fired early")
	default:
	}

	f.Advance(1 * time.Second)
	select {
	case <-ch:
	default:
		t.Fatal("After did not fire at its deadline")
	}
}

func TestFakeAfterIsOneShot(t *testing.T) {
	f := NewFake(time.Time{})
	ch := f.After(time.Second)
	f.Advance(time.Second)
	<-ch

	// A one-shot waiter must be dropped, not re-armed.
	if n := f.Waiters(); n != 0 {
		t.Fatalf("Waiters() = %d after one-shot fired, want 0", n)
	}
	f.Advance(time.Hour)
	select {
	case <-ch:
		t.Fatal("one-shot After fired twice")
	default:
	}
}

func TestFakeTickerRepeats(t *testing.T) {
	f := NewFake(time.Time{})
	tk := f.NewTicker(10 * time.Second)
	defer tk.Stop()

	for i := 1; i <= 3; i++ {
		f.Advance(10 * time.Second)
		select {
		case <-tk.C():
		default:
			t.Fatalf("ticker did not fire on tick %d", i)
		}
	}
}

func TestFakeTickerStopUnarms(t *testing.T) {
	f := NewFake(time.Time{})
	tk := f.NewTicker(5 * time.Second)

	if n := f.Waiters(); n != 1 {
		t.Fatalf("Waiters() = %d with one live ticker, want 1", n)
	}
	tk.Stop()
	if n := f.Waiters(); n != 0 {
		t.Fatalf("Waiters() = %d after Stop, want 0 — a leaked ticker is both a leak and a flake", n)
	}

	f.Advance(time.Minute)
	select {
	case <-tk.C():
		t.Fatal("stopped ticker still fired")
	default:
	}
}

// A ticker whose tick is never drained must drop ticks rather than block the
// advancing goroutine — same contract as *time.Ticker.
func TestFakeTickerDropsUndrainedTicks(t *testing.T) {
	f := NewFake(time.Time{})
	tk := f.NewTicker(time.Second)
	defer tk.Stop()

	f.Advance(10 * time.Second) // must not deadlock
	if n := len(tk.C()); n != 1 {
		t.Fatalf("buffered ticks = %d, want 1 (coalesced)", n)
	}
}

func TestFakeSleepHonoursContext(t *testing.T) {
	f := NewFake(time.Time{})
	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() { errCh <- f.Sleep(ctx, time.Hour) }()

	cancel()
	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Sleep returned %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Sleep did not return after context cancel")
	}
}

func TestFakeSetRejectsBackwardsTime(t *testing.T) {
	f := NewFake(time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC))
	defer func() {
		if recover() == nil {
			t.Fatal("Set going backwards should panic — it hides ordering bugs otherwise")
		}
	}()
	f.Set(time.Date(2026, 9, 4, 11, 0, 0, 0, time.UTC))
}

// The system clock must satisfy the same contract; a smoke test keeps the
// real implementation honest without sleeping meaningfully.
func TestSystemClock(t *testing.T) {
	c := System()
	if c.Now().IsZero() {
		t.Fatal("System().Now() returned zero time")
	}
	tk := c.NewTicker(time.Millisecond)
	defer tk.Stop()
	select {
	case <-tk.C():
	case <-time.After(2 * time.Second):
		t.Fatal("system ticker never fired")
	}
	if err := c.Sleep(context.Background(), time.Millisecond); err != nil {
		t.Fatalf("Sleep: %v", err)
	}
}
