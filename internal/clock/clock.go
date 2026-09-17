// Package clock is the injectable time source for everything in dhs that
// waits, ticks or rotates.
//
// Why this exists: the 24/7 watch path is built out of timers — the read
// deadline, the keep-alive probe, the reconnect backoff, the midnight log
// roll. Testing that behaviour against the real clock means sleeping in tests,
// and sleeping in tests is the single largest source of CI flake across OS
// runners (a Windows runner under load routinely overshoots a 50 ms sleep by
// an order of magnitude). Injecting the clock instead lets a test advance time
// instantly and deterministically: no sleeps, no tolerance windows, no
// "-race is flaky on Windows" tickets.
//
// Stdlib-only, no external dependency (ADR-0005). Real code takes System();
// tests take NewFake().
package clock

import (
	"context"
	"sync"
	"time"
)

// Clock is the seam. Production passes System(); tests pass a *Fake.
type Clock interface {
	// Now returns the current time.
	Now() time.Time

	// After returns a channel that receives once, after d elapses.
	After(d time.Duration) <-chan time.Time

	// NewTicker returns a Ticker that fires every d until stopped.
	NewTicker(d time.Duration) Ticker

	// Sleep blocks for d, or until ctx is done. Returns ctx.Err() when the
	// context ended first — callers use this to make every wait cancellable.
	Sleep(ctx context.Context, d time.Duration) error
}

// Ticker mirrors *time.Ticker, narrowed to what dhs uses.
type Ticker interface {
	C() <-chan time.Time
	Stop()
}

// ---- real clock -------------------------------------------------------------

type systemClock struct{}

// System returns the real-time clock backed by the time package.
func System() Clock { return systemClock{} }

func (systemClock) Now() time.Time { return time.Now() }

func (systemClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

func (systemClock) NewTicker(d time.Duration) Ticker {
	return &systemTicker{t: time.NewTicker(d)}
}

func (systemClock) Sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

type systemTicker struct{ t *time.Ticker }

func (s *systemTicker) C() <-chan time.Time { return s.t.C }
func (s *systemTicker) Stop()               { s.t.Stop() }

// ---- fake clock -------------------------------------------------------------

// Fake is a deterministic Clock for tests. Time only moves when the test moves
// it with Advance or Set. Every waiter armed at or before the new time fires
// before Advance returns, so a test can assert on the effect immediately
// without polling or sleeping.
//
// Safe for concurrent use: the code under test typically arms timers from its
// own goroutines while the test advances from the main one.
type Fake struct {
	mu      sync.Mutex
	now     time.Time
	waiters []*waiter
}

type waiter struct {
	at     time.Time
	ch     chan time.Time
	period time.Duration // non-zero for tickers
	// stopped guards a ticker that was stopped while still registered.
	stopped bool
}

// NewFake returns a Fake started at the given instant. A zero start is
// replaced by a fixed, readable date so golden output stays stable.
func NewFake(start time.Time) *Fake {
	if start.IsZero() {
		start = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	return &Fake{now: start}
}

func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *Fake) After(d time.Duration) <-chan time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	w := &waiter{at: f.now.Add(d), ch: make(chan time.Time, 1)}
	f.waiters = append(f.waiters, w)
	return w.ch
}

func (f *Fake) NewTicker(d time.Duration) Ticker {
	if d <= 0 {
		panic("clock: NewTicker requires a positive period")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	w := &waiter{at: f.now.Add(d), ch: make(chan time.Time, 1), period: d}
	f.waiters = append(f.waiters, w)
	return &fakeTicker{f: f, w: w}
}

func (f *Fake) Sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	ch := f.After(d)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-ch:
		return nil
	}
}

// Advance moves the clock forward by d, firing every waiter whose deadline has
// arrived. Tickers re-arm for their next period (and fire repeatedly if d
// spans several periods).
func (f *Fake) Advance(d time.Duration) {
	f.Set(f.Now().Add(d))
}

// Set moves the clock to t (must not go backwards) and fires due waiters.
func (f *Fake) Set(t time.Time) {
	f.mu.Lock()
	if t.Before(f.now) {
		f.mu.Unlock()
		panic("clock: Fake time must not move backwards")
	}
	f.now = t

	var fire []chan time.Time
	var fireAt []time.Time
	kept := f.waiters[:0]
	for _, w := range f.waiters {
		if w.stopped {
			continue
		}
		if w.period == 0 {
			if !w.at.After(t) {
				fire = append(fire, w.ch)
				fireAt = append(fireAt, w.at)
				continue // one-shot: drop it
			}
			kept = append(kept, w)
			continue
		}
		// Ticker: fire once per elapsed period, then re-arm.
		for !w.at.After(t) {
			fire = append(fire, w.ch)
			fireAt = append(fireAt, w.at)
			w.at = w.at.Add(w.period)
		}
		kept = append(kept, w)
	}
	f.waiters = kept
	f.mu.Unlock()

	// Deliver outside the lock so a receiver may arm new timers.
	// Non-blocking: a ticker whose previous tick was never drained drops
	// this one, exactly as *time.Ticker does.
	for i, ch := range fire {
		select {
		case ch <- fireAt[i]:
		default:
		}
	}
}

// Waiters reports how many timers are currently armed. Tests use it to assert
// that a component cleaned up after itself (no leaked tickers).
func (f *Fake) Waiters() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, w := range f.waiters {
		if !w.stopped {
			n++
		}
	}
	return n
}

type fakeTicker struct {
	f *Fake
	w *waiter
}

func (t *fakeTicker) C() <-chan time.Time { return t.w.ch }

func (t *fakeTicker) Stop() {
	t.f.mu.Lock()
	defer t.f.mu.Unlock()
	t.w.stopped = true
}
