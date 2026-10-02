package main

import (
	"context"
	"testing"
	"time"

	"dhs/internal/consumer"
)

// A device that pushes its whole state at once says far more than any
// fixed buffer holds. Every event must come out, in order, with nobody
// reading while they go in.
func TestEventQueueKeepsEveryEventOfABurstInOrder(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q := newEventQueue(ctx)

	const burst = 20000 // a 128-entry channel kept the first 128 of these
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < burst; i++ {
			q.push(consumer.Event{ID: i})
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("push blocked with nobody reading")
	}
	for i := 0; i < burst; i++ {
		select {
		case ev := <-q.C():
			if ev.ID != i {
				t.Fatalf("event %d came out as %d", i, ev.ID)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("event %d never came out", i)
		}
	}
	// And it keeps working after it has drained.
	q.push(consumer.Event{ID: -1})
	select {
	case ev := <-q.C():
		if ev.ID != -1 {
			t.Fatalf("after draining: %d", ev.ID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("nothing came out after the queue had drained")
	}
}

func TestEventQueueStopsWithItsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	q := newEventQueue(ctx)
	q.push(consumer.Event{ID: 1})
	q.push(consumer.Event{ID: 2})
	if ev := <-q.C(); ev.ID != 1 {
		t.Fatalf("first = %d", ev.ID)
	}
	// Cancelled with an event still to hand over: the pump lets go.
	cancel()
	time.Sleep(20 * time.Millisecond)
	q.push(consumer.Event{ID: 3}) // must not block
}
