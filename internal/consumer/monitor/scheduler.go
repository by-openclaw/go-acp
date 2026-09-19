package monitor

import (
	"context"
	"sync"
	"time"

	"dhs/internal/clock"
	"dhs/internal/consumer"
)

// scheduler is the single next-due poll scheduler (ADR-0030 §3). One
// goroutine holds at most one armed timer: it sleeps until the nearest
// due entry, enqueues every entry now due, reschedules each by its
// interval, then sleeps again. First fire is offset by a deterministic
// per-address jitter so same-cadence reads spread across the window.
type scheduler struct {
	clk     clock.Clock
	reads   chan<- command
	entries []*schedEntry

	mu      sync.Mutex
	pending map[string]bool
}

type schedEntry struct {
	key      string
	req      consumer.ValueRequest
	interval time.Duration
	onChange bool
	due      time.Time
}

func newScheduler(p *Profile, clk clock.Clock, reads chan<- command) *scheduler {
	s := &scheduler{clk: clk, reads: reads, pending: make(map[string]bool)}
	now := clk.Now()
	for _, e := range p.Entries {
		req := e.req()
		key := addrKey(req)
		iv := p.effInterval(e)
		s.entries = append(s.entries, &schedEntry{
			key:      key,
			req:      req,
			interval: iv,
			onChange: p.effOnChange(e),
			due:      now.Add(jitter(key, iv)),
		})
	}
	return s
}

func (s *scheduler) run(ctx context.Context) {
	if len(s.entries) == 0 {
		return
	}
	for {
		next := s.drainDue(ctx, s.clk.Now())
		if ctx.Err() != nil {
			return
		}
		d := next.Sub(s.clk.Now())
		if d < 0 {
			d = 0
		}
		if err := s.clk.Sleep(ctx, d); err != nil {
			return
		}
	}
}

// drainDue enqueues every entry due at or before now, rescheduling each
// by its interval (looping so a large clock jump fires every elapsed
// period), and returns the nearest future due time.
func (s *scheduler) drainDue(ctx context.Context, now time.Time) time.Time {
	for _, e := range s.entries {
		for !e.due.After(now) {
			s.enqueue(ctx, e)
			e.due = e.due.Add(e.interval)
		}
	}
	min := s.entries[0].due
	for _, e := range s.entries[1:] {
		if e.due.Before(min) {
			min = e.due
		}
	}
	return min
}

// enqueue submits a read, coalescing: if the same address is already
// queued or in flight, the duplicate is skipped (latest-wins under a
// slow device). The worker clears the pending flag when it finishes.
func (s *scheduler) enqueue(ctx context.Context, e *schedEntry) {
	s.mu.Lock()
	if s.pending[e.key] {
		s.mu.Unlock()
		return
	}
	s.pending[e.key] = true
	s.mu.Unlock()

	c := command{
		kind:     cmdRead,
		key:      e.key,
		req:      e.req,
		onChange: e.onChange,
		done: func() {
			s.mu.Lock()
			delete(s.pending, e.key)
			s.mu.Unlock()
		},
	}
	select {
	case s.reads <- c:
	case <-ctx.Done():
		s.mu.Lock()
		delete(s.pending, e.key)
		s.mu.Unlock()
	}
}
