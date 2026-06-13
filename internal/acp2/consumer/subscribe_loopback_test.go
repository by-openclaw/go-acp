package acp2

import (
	"context"
	"testing"
	"time"

	"dhs/internal/consumer"

	"dhs/internal/acp2/codec"
)

// subscribe_loopback_test.go drives Plugin.Subscribe / Unsubscribe and
// the announce dispatch path. The harness fires one ACP2 announce right
// after the handshake; the test asserts the registered callback
// receives a decoded Event.

// announceMsg builds an announce message for obj-id with a u32 value
// property (the wire shape the device emits on a value change).
func announceMsg(objID uint32, val uint32) *codec.ACP2Message {
	valProp := codec.MakeValueProperty(codec.PIDValue, codec.NumTypeU32, beBytes(val))
	return &codec.ACP2Message{
		Type:  codec.ACP2TypeAnnounce,
		PID:   codec.PIDValue,
		ObjID: objID,
		Idx:   0,
		// Properties is read by the announce closure when the message is
		// fed directly via feedAnnounce (bypassing the wire decode);
		// Body carries the same bytes for the on-wire announce path.
		Properties: []codec.Property{valProp},
		Body:       objectBody(objID, []codec.Property{valProp}),
	}
}

func beBytes(v uint32) []byte {
	return []byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}
}

func TestSubscribe_NotConnected(t *testing.T) {
	p := &Plugin{logger: testLogger()}
	err := p.Subscribe(consumer.ValueRequest{ID: 3}, func(consumer.Event) {})
	if err != consumer.ErrNotConnected {
		t.Errorf("err = %v, want ErrNotConnected", err)
	}
}

func TestSubscribe_ReceivesAnnounce(t *testing.T) {
	srv, host, port := newFakeServer(t)
	// Fire an announce for obj-id 3 = 55 after the handshake completes.
	srv.announceFn = func(send func(slot uint8, msg *codec.ACP2Message)) {
		send(1, announceMsg(3, 55))
	}
	defer srv.stop()

	p := &Plugin{logger: testLogger()}
	p.SetKeepAlive(consumer.KeepAliveConfig{
		Interval: consumer.DisableInterval,
		Timeout:  consumer.DisableTimeout,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := p.Connect(ctx, host, port); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = p.Disconnect() }()

	ch := make(chan consumer.Event, 4)
	if err := p.Subscribe(consumer.ValueRequest{Slot: -1, ID: -1}, func(ev consumer.Event) {
		ch <- ev
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	// The handshake announce may have already fired before Subscribe was
	// registered (it races Connect's return). Trigger a fresh announce
	// deterministically by re-arming announceFn and issuing a request
	// that the server answers — but simpler: directly push an announce
	// through the live session from the server side via a second
	// connection is not available. Instead, drive an announce by calling
	// SubscribeAnnounces on the session and feeding a synthetic frame is
	// white-box. We assert on the handshake announce if it arrived;
	// otherwise we fall back to injecting through the session.
	select {
	case ev := <-ch:
		if ev.ID != 3 {
			t.Errorf("event ID = %d, want 3", ev.ID)
		}
		if ev.Value.Kind != consumer.KindUint || ev.Value.Uint != 55 {
			t.Errorf("event value = %+v, want uint 55", ev.Value)
		}
		return
	case <-time.After(200 * time.Millisecond):
		// Handshake announce raced past Subscribe — inject one via the
		// session's announce fan-out to prove the closure + dispatch.
	}

	p.mu.Lock()
	s := p.session
	p.mu.Unlock()
	// Feed an announce straight into the session fan-out (same path the
	// readLoop uses) to deterministically exercise the closure.
	feedAnnounce(s, 1, announceMsg(3, 55))

	select {
	case ev := <-ch:
		if ev.ID != 3 || ev.Value.Uint != 55 {
			t.Errorf("event = %+v, want ID 3 value 55", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("announce not delivered to subscriber")
	}
}

// feedAnnounce invokes every registered announce subscriber with msg,
// mimicking what session.handleACP2Frame does on an inbound announce.
// White-box: reaches the unexported annSubs to make the dispatch
// assertion deterministic regardless of handshake-announce timing.
func feedAnnounce(s *Session, slot uint8, msg *codec.ACP2Message) {
	s.annMu.Lock()
	subs := make([]AnnounceFunc, 0, len(s.annSubs))
	for _, fn := range s.annSubs {
		subs = append(subs, fn)
	}
	s.annMu.Unlock()
	for _, fn := range subs {
		fn(slot, msg)
	}
}

func TestSubscribe_FilterBySlot(t *testing.T) {
	srv, host, port := newFakeServer(t)
	defer srv.stop()

	p := connectPlugin(t, srv, host, port)

	ch := make(chan consumer.Event, 4)
	if err := p.Subscribe(consumer.ValueRequest{Slot: 2, ID: -1}, func(ev consumer.Event) {
		ch <- ev
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	p.mu.Lock()
	s := p.session
	p.mu.Unlock()

	// Announce on slot 1 must be filtered out (sub wants slot 2).
	feedAnnounce(s, 1, announceMsg(3, 10))
	// Announce on slot 2 must pass.
	feedAnnounce(s, 2, announceMsg(3, 20))

	select {
	case ev := <-ch:
		if ev.Slot != 2 {
			t.Errorf("delivered slot = %d, want 2", ev.Slot)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("slot-2 announce not delivered")
	}
	// No second event (slot-1 was filtered).
	select {
	case ev := <-ch:
		t.Errorf("unexpected second event: %+v", ev)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestUnsubscribe(t *testing.T) {
	srv, host, port := newFakeServer(t)
	defer srv.stop()

	p := connectPlugin(t, srv, host, port)
	req := consumer.ValueRequest{Slot: -1, ID: -1}

	ch := make(chan consumer.Event, 4)
	if err := p.Subscribe(req, func(ev consumer.Event) { ch <- ev }); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := p.Unsubscribe(req); err != nil {
		t.Fatalf("Unsubscribe: %v", err)
	}

	p.mu.Lock()
	s := p.session
	p.mu.Unlock()

	feedAnnounce(s, 1, announceMsg(3, 99))
	select {
	case ev := <-ch:
		t.Errorf("event delivered after Unsubscribe: %+v", ev)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestUnsubscribe_Unknown(t *testing.T) {
	srv, host, port := newFakeServer(t)
	defer srv.stop()

	p := connectPlugin(t, srv, host, port)
	// Unsubscribe a request that was never subscribed — no-op, no error.
	if err := p.Unsubscribe(consumer.ValueRequest{ID: 1234}); err != nil {
		t.Errorf("Unsubscribe unknown: %v", err)
	}
}
