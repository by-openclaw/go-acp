package main

import (
	"context"
	"sync"

	"dhs/internal/consumer"
)

// eventQueue is the unbounded FIFO between a plugin's event callback
// and the watch loop that prints and evaluates.
//
// The callback runs on the plugin's receive path and must never block
// it; the loop prints at the terminal's pace. A fixed buffer between
// the two has to drop when a device says a lot at once — a pushed
// initial state is thousands of values in one frame — and a dropped
// event is a line the operator never sees and a verdict the alarm
// engine never computes. So the queue grows instead, and gives the
// memory back as it drains.
type eventQueue struct {
	mu    sync.Mutex
	items []consumer.Event
	wake  chan struct{}
	out   chan consumer.Event
}

// newEventQueue starts a queue that delivers until ctx ends.
func newEventQueue(ctx context.Context) *eventQueue {
	q := &eventQueue{wake: make(chan struct{}, 1), out: make(chan consumer.Event)}
	go q.pump(ctx)
	return q
}

// push adds one event. It never blocks and never drops.
func (q *eventQueue) push(ev consumer.Event) {
	q.mu.Lock()
	q.items = append(q.items, ev)
	q.mu.Unlock()
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

// C is where the events come out, in the order they went in.
func (q *eventQueue) C() <-chan consumer.Event { return q.out }

func (q *eventQueue) pump(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-q.wake:
		}
		for {
			q.mu.Lock()
			batch := q.items
			q.items = nil
			q.mu.Unlock()
			if len(batch) == 0 {
				break
			}
			for _, ev := range batch {
				select {
				case q.out <- ev:
				case <-ctx.Done():
					return
				}
			}
		}
	}
}
