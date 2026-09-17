package events_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"dhs/internal/amwa/codec/is07"
	_ "dhs/internal/amwa/codec/is07/v10"
	"dhs/internal/amwa/session/events"
)

const (
	srcA = "1f1e1d1c-1b1a-4019-8817-161514131211"
	srcB = "2f2e2d2c-2b2a-4029-8827-262524232221"
)

// startPublisher mounts a Publisher behind an httptest.Server and
// returns the WS URL plus a teardown.
func startPublisher(t *testing.T, hb time.Duration) (*events.Publisher, string, func()) {
	t.Helper()
	pub := events.NewPublisher(events.PublisherOptions{HeartbeatInterval: hb})
	mux := http.NewServeMux()
	mux.Handle("/x-nmos/events/v1.0/ws", pub.Handler())
	srv := httptest.NewServer(mux)
	wsURL := strings.Replace(srv.URL, "http://", "ws://", 1) + "/x-nmos/events/v1.0/ws"
	teardown := func() {
		_ = pub.Close()
		srv.Close()
	}
	return pub, wsURL, teardown
}

func TestSubscriberReceivesMatchingEvents(t *testing.T) {
	pub, wsURL, stop := startPublisher(t, 0) // no auto-heartbeat for clean assertions
	defer stop()

	sub := events.NewSubscriber(events.SubscriberOptions{HeartbeatInterval: 0})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := sub.Connect(ctx, wsURL); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = sub.Close() }()

	if err := sub.Subscribe([]string{srcA}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	// Wait until the publisher has APPLIED our subscription filter —
	// not merely accepted the socket. SubscriberCount()==1 plus a fixed
	// sleep was a timing guess: on a loaded runner the publish landed
	// before serve() read the command_subscription frame, matched an
	// empty source set, and the test saw 0 frames (windows-latest flake,
	// ADR-0029 determinism rule).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && !pub.SubscribedTo(srcA) {
		time.Sleep(10 * time.Millisecond)
	}
	if !pub.SubscribedTo(srcA) {
		t.Fatalf("publisher never applied the subscription filter")
	}

	got := make(chan is07.Message, 4)
	var runErr error
	var runWG sync.WaitGroup
	runWG.Add(1)
	go func() {
		defer runWG.Done()
		runErr = sub.Run(ctx, func(m is07.Message) { got <- m })
	}()

	emit := func(src string, val bool) {
		err := pub.Publish(is07.EventBoolean{
			EventCommon: is07.EventCommon{
				Identity:  is07.Identity{SourceID: src},
				Timing:    is07.Timing{CreationTimestamp: "1:0"},
				EventType: "boolean/tally",
			},
			Payload: is07.PayloadBoolean{Value: val},
		})
		if err != nil {
			t.Errorf("Publish: %v", err)
		}
	}

	// Match: publish to srcA twice.
	emit(srcA, true)
	emit(srcA, false)
	// No-match: publish to srcB once — should NOT reach subscriber.
	emit(srcB, true)

	// Collect 2 frames within 1s; should timeout if more than 2 arrive.
	timeout := time.After(1 * time.Second)
	var received []is07.Message
collect:
	for {
		select {
		case m := <-got:
			received = append(received, m)
			if len(received) == 2 {
				// Drain a bit longer to catch any leaked srcB frame.
				select {
				case extra := <-got:
					received = append(received, extra)
				case <-time.After(150 * time.Millisecond):
				}
				break collect
			}
		case <-timeout:
			break collect
		}
	}
	if len(received) != 2 {
		t.Fatalf("expected exactly 2 srcA frames, got %d: %+v", len(received), received)
	}
	for i, m := range received {
		evt, ok := m.(is07.EventBoolean)
		if !ok {
			t.Fatalf("frame %d is %T, want EventBoolean", i, m)
		}
		if evt.Identity.SourceID != srcA {
			t.Fatalf("frame %d source %s, want %s", i, evt.Identity.SourceID, srcA)
		}
	}

	// Tear down — Run should observe Close and return nil.
	if err := sub.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	runWG.Wait()
	if runErr != nil {
		t.Fatalf("Run returned %v", runErr)
	}
}

func TestPublisherHealthHeartbeatToSubscriber(t *testing.T) {
	pub, wsURL, stop := startPublisher(t, 50*time.Millisecond) // fast heartbeat
	defer stop()

	sub := events.NewSubscriber(events.SubscriberOptions{HeartbeatInterval: 0})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := sub.Connect(ctx, wsURL); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = sub.Close() }()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if pub.SubscriberCount() == 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	var hbCount atomic.Int32
	go func() {
		_ = sub.Run(ctx, func(m is07.Message) {
			if _, ok := m.(is07.MessageHealth); ok {
				hbCount.Add(1)
			}
		})
	}()

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if hbCount.Load() >= 3 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if hbCount.Load() < 3 {
		t.Fatalf("expected >=3 heartbeats in 2s, got %d", hbCount.Load())
	}
}

func TestPublisherRespondsToCommandHealth(t *testing.T) {
	pub, wsURL, stop := startPublisher(t, 0) // no background heartbeat
	defer stop()
	_ = pub

	sub := events.NewSubscriber(events.SubscriberOptions{HeartbeatInterval: 100 * time.Millisecond})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := sub.Connect(ctx, wsURL); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = sub.Close() }()

	var hbCount atomic.Int32
	go func() {
		_ = sub.Run(ctx, func(m is07.Message) {
			if h, ok := m.(is07.MessageHealth); ok {
				if h.Timing.OriginTimestamp == "" {
					t.Errorf("origin_timestamp missing on health response")
				}
				hbCount.Add(1)
			}
		})
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if hbCount.Load() >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if hbCount.Load() < 2 {
		t.Fatalf("expected >=2 health replies, got %d", hbCount.Load())
	}
}

func TestPublishRejectsNonStateMessage(t *testing.T) {
	pub := events.NewPublisher(events.PublisherOptions{HeartbeatInterval: 0})
	defer func() { _ = pub.Close() }()
	err := pub.Publish(is07.MessageHealth{})
	if err == nil {
		t.Fatalf("Publish(MessageHealth) should error")
	}
}

func TestSubscriberReplaceSubscriptionSet(t *testing.T) {
	pub, wsURL, stop := startPublisher(t, 0)
	defer stop()

	sub := events.NewSubscriber(events.SubscriberOptions{HeartbeatInterval: 0})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := sub.Connect(ctx, wsURL); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = sub.Close() }()

	got := make(chan is07.Message, 8)
	go func() {
		_ = sub.Run(ctx, func(m is07.Message) { got <- m })
	}()

	if err := sub.Subscribe([]string{srcA}); err != nil {
		t.Fatalf("Subscribe A: %v", err)
	}
	// Wait for the A filter to be APPLIED (content-aware — see
	// SubscribedTo), then replace with B and wait for the swap. Fixed
	// sleeps here were the same scheduling guess as the
	// ReceivesMatchingEvents flake: an emit racing the un-applied
	// replacement drops the frame.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && !pub.SubscribedTo(srcA) {
		time.Sleep(10 * time.Millisecond)
	}
	if !pub.SubscribedTo(srcA) {
		t.Fatal("subscription A never applied")
	}

	if err := sub.Subscribe([]string{srcB}); err != nil {
		t.Fatalf("Subscribe B: %v", err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && (!pub.SubscribedTo(srcB) || pub.SubscribedTo(srcA)) {
		time.Sleep(10 * time.Millisecond)
	}
	if !pub.SubscribedTo(srcB) || pub.SubscribedTo(srcA) {
		t.Fatal("subscription replacement A→B never applied")
	}

	emitB := is07.EventBoolean{
		EventCommon: is07.EventCommon{
			Identity:  is07.Identity{SourceID: srcB},
			Timing:    is07.Timing{CreationTimestamp: "1:0"},
			EventType: "boolean",
		},
		Payload: is07.PayloadBoolean{Value: true},
	}
	emitA := is07.EventBoolean{
		EventCommon: is07.EventCommon{
			Identity:  is07.Identity{SourceID: srcA},
			Timing:    is07.Timing{CreationTimestamp: "1:0"},
			EventType: "boolean",
		},
		Payload: is07.PayloadBoolean{Value: true},
	}
	if err := pub.Publish(emitB); err != nil {
		t.Fatalf("Publish B: %v", err)
	}
	if err := pub.Publish(emitA); err != nil {
		t.Fatalf("Publish A: %v", err)
	}

	timeout := time.After(1 * time.Second)
	select {
	case m := <-got:
		evt := m.(is07.EventBoolean)
		if evt.Identity.SourceID != srcB {
			t.Fatalf("first frame src %s, want %s (only B subscribed)",
				evt.Identity.SourceID, srcB)
		}
	case <-timeout:
		t.Fatalf("never got frame for srcB after re-subscribe")
	}
	// The srcA frame should NOT arrive — we drained subscription to {B}.
	select {
	case m := <-got:
		t.Fatalf("unexpected frame after re-subscribe: %+v", m)
	case <-time.After(300 * time.Millisecond):
	}
}

// Compile-time assertion: Publisher satisfies http.Handler via its
// Handler() returning the right type.
var _ = func() http.Handler {
	p := events.NewPublisher(events.PublisherOptions{})
	return p.Handler()
}

func TestDialBadURLs(t *testing.T) {
	cases := []string{
		"http://example.com/",        // wrong scheme
		"ws://[::1",                  // malformed
		"ws://127.0.0.1:1/no-server", // no listener
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	for _, u := range cases {
		t.Run(u, func(t *testing.T) {
			s := events.NewSubscriber(events.SubscriberOptions{})
			err := s.Connect(ctx, u)
			if err == nil {
				_ = s.Close()
				t.Fatalf("expected error dialing %s", u)
			}
		})
	}
}
