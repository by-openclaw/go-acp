package monitor

import (
	"context"
	"testing"
	"time"

	"dhs/internal/clock"
	"dhs/internal/consumer"
)

func TestStatsSuccess(t *testing.T) {
	m := New(WithLogger(discardLog()))
	defer m.Stop()
	p := &Profile{Defaults: Defaults{Interval: Duration(time.Hour)}, Entries: []Entry{{OID: "x"}}}
	if err := m.Add(context.Background(), Device{Name: "d", Proto: newFakeProto(), Profile: p}); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Stats("d"); !ok {
		t.Error("Stats should report ok for a live device")
	}
}

func TestExecSleepCancelled(t *testing.T) {
	fp := newFakeProto()
	req := consumer.ValueRequest{Path: "z"}
	w := newTestWorker(fp)
	w.minGap = time.Hour
	w.lastOp = w.clk.Now() // full gap still to wait

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	res := make(chan writeResult, 1)
	w.exec(ctx, command{kind: cmdWrite, key: addrKey(req), req: req, val: intVal(1), result: res})
	r := <-res
	if r.err == nil {
		t.Fatal("exec should surface the cancelled rate-gap sleep as an error")
	}
	if fp.setCount() != 0 {
		t.Errorf("no wire op should run when the gap sleep is cancelled, sets = %d", fp.setCount())
	}
}

func TestEnqueueCtxCancelledClearsPending(t *testing.T) {
	reads := make(chan command) // unbuffered, no receiver
	s := &scheduler{clk: clock.System(), reads: reads, pending: make(map[string]bool)}
	e := &schedEntry{key: "k", req: consumer.ValueRequest{Path: "1.1"}, interval: time.Second}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.enqueue(ctx, e)

	if s.pending["k"] {
		t.Error("pending must be cleared when enqueue is cancelled")
	}
	if len(reads) != 0 {
		t.Errorf("nothing should be queued on cancel, len = %d", len(reads))
	}
}

func TestSchedulerRunCancels(t *testing.T) {
	fk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	reads := make(chan command, 8)
	p := &Profile{Defaults: Defaults{Interval: Duration(time.Second)}, Entries: []Entry{{OID: "a"}, {OID: "b"}}}
	s := newScheduler(p, fk, reads)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.run(ctx); close(done) }()

	for i := 0; i < 2000 && fk.Waiters() == 0; i++ {
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler.run did not return after cancel")
	}
}

func TestWorkerRunReturnsOnCancelledContext(t *testing.T) {
	w := newTestWorker(newFakeProto())
	w.ops = make(chan command, 1)
	w.reads = make(chan command, 1)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() { w.run(ctx); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("worker.run did not return on a cancelled context")
	}
}

func TestDrainDueCatchUpAcrossPeriods(t *testing.T) {
	fk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	reads := make(chan command, 64)
	p := &Profile{Defaults: Defaults{Interval: Duration(time.Second)}, Entries: []Entry{{OID: "a"}, {OID: "b"}}}
	s := newScheduler(p, fk, reads)

	now := fk.Now().Add(4 * time.Second) // jump several periods at once
	next := s.drainDue(context.Background(), now)

	if !next.After(now) {
		t.Errorf("next due %v must be strictly after now %v", next, now)
	}
	// With no worker to clear pending, each key coalesces to a single
	// queued read even though the inner loop advanced its due across
	// several periods. Two entries → two reads; dues moved past now.
	if len(reads) != 2 {
		t.Errorf("catch-up enqueued %d reads, want 2 (one per coalesced key)", len(reads))
	}
	for _, e := range s.entries {
		if !e.due.After(now) {
			t.Errorf("entry %s due %v not advanced past now %v", e.key, e.due, now)
		}
	}
}
