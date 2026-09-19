package monitor

import (
	"context"
	"testing"
	"time"

	"dhs/internal/clock"
	"dhs/internal/consumer"
)

func TestSchedulerCoalescesPendingReads(t *testing.T) {
	reads := make(chan command, 4)
	s := &scheduler{clk: clock.System(), reads: reads, pending: make(map[string]bool)}
	e := &schedEntry{key: "k", req: consumer.ValueRequest{Path: "1.1"}, interval: time.Second}
	ctx := context.Background()

	s.enqueue(ctx, e) // fills channel, marks pending
	s.enqueue(ctx, e) // same key still pending → coalesced
	if len(reads) != 1 {
		t.Fatalf("after two enqueues of one key, reads len = %d, want 1 (coalesced)", len(reads))
	}

	c := <-reads // worker takes it
	c.done()     // ...and clears pending when finished
	s.enqueue(ctx, e)
	if len(reads) != 1 {
		t.Fatalf("re-enqueue after done: reads len = %d, want 1", len(reads))
	}
}

func TestSchedulerFirstDueRespectsJitter(t *testing.T) {
	fk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	reads := make(chan command, 4)
	p := &Profile{
		Defaults: Defaults{Interval: Duration(time.Second)},
		Entries:  []Entry{{OID: "1.2.3"}},
	}
	s := newScheduler(p, fk, reads)
	key := addrKey(consumer.ValueRequest{Path: "1.2.3"})
	wantFirst := fk.Now().Add(jitter(key, time.Second))
	if !s.entries[0].due.Equal(wantFirst) {
		t.Errorf("first due = %v, want start+jitter %v", s.entries[0].due, wantFirst)
	}
}
