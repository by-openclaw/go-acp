package monitor

import (
	"sync"
	"sync/atomic"

	"dhs/internal/consumer"
)

// Bus is the in-process event bus (ADR-0030 §5): Go channels plus a
// subscriber registry, not a broker. Each subscriber owns a bounded
// channel; on overflow the oldest event is dropped so the newest state
// always gets through (latest-wins backpressure).
type Bus struct {
	mu    sync.Mutex
	next  int
	subs  map[int]*subscription
	drops atomic.Uint64
}

type subscription struct {
	ch     chan consumer.Event
	filter func(consumer.Event) bool
}

func newBus() *Bus { return &Bus{subs: make(map[int]*subscription)} }

// Subscribe registers a listener with a bounded buffer and an optional
// filter (nil means all events). It returns a subscription id for
// Unsubscribe and the receive-only channel.
func (b *Bus) Subscribe(buf int, filter func(consumer.Event) bool) (int, <-chan consumer.Event) {
	if buf < 1 {
		buf = 1
	}
	ch := make(chan consumer.Event, buf)
	b.mu.Lock()
	defer b.mu.Unlock()
	id := b.next
	b.next++
	b.subs[id] = &subscription{ch: ch, filter: filter}
	return id, ch
}

// Unsubscribe removes a listener and closes its channel. Safe to call
// with an unknown id.
func (b *Bus) Unsubscribe(id int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if s, ok := b.subs[id]; ok {
		close(s.ch)
		delete(b.subs, id)
	}
}

// Publish fans an event out to every matching subscriber. Never blocks:
// a full subscriber loses its oldest event, and the drop is counted.
func (b *Bus) Publish(ev consumer.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, s := range b.subs {
		if s.filter != nil && !s.filter(ev) {
			continue
		}
		select {
		case s.ch <- ev:
		default:
			// latest-wins: drop the oldest, then enqueue the newest.
			select {
			case <-s.ch:
			default:
			}
			select {
			case s.ch <- ev:
			default:
			}
			b.drops.Add(1)
		}
	}
}

// Drops reports how many events were shed under backpressure.
func (b *Bus) Drops() uint64 { return b.drops.Load() }
