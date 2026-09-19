package monitor

import (
	"testing"

	"dhs/internal/consumer"
)

func TestBusPublishToSubscriber(t *testing.T) {
	b := newBus()
	_, ch := b.Subscribe(4, nil)
	b.Publish(consumer.Event{Label: "a"})
	ev := <-ch
	if ev.Label != "a" {
		t.Errorf("got label %q, want a", ev.Label)
	}
}

func TestBusFilter(t *testing.T) {
	b := newBus()
	_, ch := b.Subscribe(4, func(e consumer.Event) bool { return e.Label == "keep" })
	b.Publish(consumer.Event{Label: "drop"})
	b.Publish(consumer.Event{Label: "keep"})
	ev := <-ch
	if ev.Label != "keep" {
		t.Errorf("filter let through %q, want keep", ev.Label)
	}
	select {
	case extra := <-ch:
		t.Errorf("unexpected second event %q", extra.Label)
	default:
	}
}

func TestBusLatestWinsDropsOldest(t *testing.T) {
	b := newBus()
	_, ch := b.Subscribe(1, nil)
	b.Publish(consumer.Event{Label: "1"})
	b.Publish(consumer.Event{Label: "2"})
	b.Publish(consumer.Event{Label: "3"})
	ev := <-ch
	if ev.Label != "3" {
		t.Errorf("latest-wins should keep newest, got %q want 3", ev.Label)
	}
	if b.Drops() != 2 {
		t.Errorf("Drops = %d, want 2", b.Drops())
	}
}

func TestBusUnsubscribeClosesChannel(t *testing.T) {
	b := newBus()
	id, ch := b.Subscribe(1, nil)
	b.Unsubscribe(id)
	if _, ok := <-ch; ok {
		t.Errorf("channel should be closed after Unsubscribe")
	}
	// A publish after unsubscribe must not panic.
	b.Publish(consumer.Event{Label: "x"})
}
