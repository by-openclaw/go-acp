package events_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dhs/internal/amwa/codec/is07"
	_ "dhs/internal/amwa/codec/is07/v10"
	"dhs/internal/amwa/session/events"
)

// TestSubscriptionSendsCurrentState: a client that has just
// subscribed has seen no change yet, and a tally can go hours without
// one. IS-07 §5.2 makes the subscription response carry the value.
func TestSubscriptionSendsCurrentState(t *testing.T) {
	const src = "11111111-1111-4111-8111-111111111111"
	want := is07.EventBoolean{
		EventCommon: is07.EventCommon{
			MessageType: is07.MessageTypeState,
			Identity:    is07.Identity{SourceID: src},
			Timing:      is07.Timing{CreationTimestamp: "100:0"},
			EventType:   "boolean",
		},
		Payload: is07.PayloadBoolean{Value: true},
	}

	pub := events.NewPublisher(events.PublisherOptions{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		StateOf: func(id string) (is07.Message, bool) {
			if id != src {
				return nil, false
			}
			return want, true
		},
	})
	defer func() { _ = pub.Close() }()

	srv := httptest.NewServer(pub.Handler())
	defer srv.Close()

	sub := events.NewSubscriber(events.SubscriberOptions{HeartbeatInterval: 0})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	got := make(chan is07.Message, 4)
	if err := sub.Connect(ctx, wsURL(srv.URL)); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = sub.Close() }()
	go func() { _ = sub.Run(ctx, func(m is07.Message) { got <- m }) }()

	if err := sub.Subscribe([]string{src}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	select {
	case m := <-got:
		b, isBool := m.(is07.EventBoolean)
		if !isBool {
			t.Fatalf("first message is %T, want the source's current state", m)
		}
		if !b.Payload.Value || b.Identity.SourceID != src {
			raw, _ := json.Marshal(b)
			t.Fatalf("state does not match what the Node holds: %s", raw)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no state arrived after subscribing; a new subscriber would wait for a change that may never come")
	}
}

// TestIdleConnectionIsClosed: IS-07 §5.2 puts the heartbeat on the
// receiver and the reaping on the sender. A sender that never reaps
// accumulates a socket per crashed consumer until it runs out of file
// descriptors -- and then the NEXT consumer, the working one, cannot
// connect.
func TestIdleConnectionIsClosed(t *testing.T) {
	pub := events.NewPublisher(events.PublisherOptions{
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		IdleTimeout: 300 * time.Millisecond,
	})
	defer func() { _ = pub.Close() }()

	srv := httptest.NewServer(pub.Handler())
	defer srv.Close()

	sub := events.NewSubscriber(events.SubscriberOptions{HeartbeatInterval: 0})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sub.Connect(ctx, wsURL(srv.URL)); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = sub.Close() }()
	go func() { _ = sub.Run(ctx, func(is07.Message) {}) }()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if pub.SubscriberCount() == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("a connection that sent no health command was still held open")
}

func wsURL(httpURL string) string {
	return strings.Replace(httpURL, "http://", "ws://", 1)
}
