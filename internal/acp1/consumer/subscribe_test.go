package acp1

import (
	"context"
	"log/slog"
	"net"
	"testing"
	"time"

	"dhs/internal/acp1/codec"
	"dhs/internal/consumer"
)

// subscribePlugin wires a Plugin with a real UDP Listener + a seeded walked
// tree so Subscribe's wrapper can decode typed values and resolve labels.
func subscribePlugin(t *testing.T) (*Plugin, int) {
	t.Helper()
	p, _, _ := newPluginWithClient(t)
	l, err := NewListener(slog.Default(), 0)
	if err != nil {
		t.Fatalf("NewListener: %v", err)
	}
	l.Start(context.Background())
	t.Cleanup(l.Stop)
	p.listener = l
	p.subHandles = map[subKey]SubHandle{}
	p.trees = newSlotTreeCache(defaultCacheConfig().MaxSize, defaultCacheConfig().TTL)
	p.SeedTreeFromCachedObjects(0, []consumer.Object{
		{Group: "control", ID: 0, Label: "Level", Kind: consumer.KindInt},
	})
	la := l.conn.LocalAddr().(*net.UDPAddr)
	return p, la.Port
}

func sendAnnounce(t *testing.T, port int, group codec.ObjGroup, id byte, val []byte) {
	t.Helper()
	conn, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	_, _ = conn.Write(buildReply(t, 0, codec.MTypeAnnounce, 0, group, id, val))
}

// awaitAnnounce sends an announcement and keeps sending it until the
// subscription reports one, or the deadline passes.
//
// One datagram is not a delivery. CI failed exactly that way on run
// 34073533688 -- "typed control announcement not delivered" -- on a
// docs-only change that touched nothing here. The single datagram the
// test sent never reached the listener: UDP drops a datagram whose
// receive buffer is momentarily full, and a runner building every
// package at once under -race is when that happens. Nothing was wrong
// with the subscription. Nothing had arrived at it.
//
// The resend is a TEST device's behaviour, not a real one's. A real ACP1
// device sends each announcement once -- unacknowledged, never repeated
// (the spec's retries cover GET/SET only) -- and a consumer that loses it
// learns the value at the next get or walk. What these tests check is that
// an announcement which DOES arrive is decoded and routed to the right
// subscriber; delivery over a busy loopback is not under test, so the test
// device repeats itself until one copy lands. The handler treats a repeat
// as the same announcement, so the event checked is the same either way.
func awaitAnnounce(t *testing.T, port int, group codec.ObjGroup, id byte, val []byte, events <-chan consumer.Event) consumer.Event {
	t.Helper()
	deadline := time.After(10 * time.Second)
	resend := time.NewTicker(100 * time.Millisecond)
	defer resend.Stop()
	sendAnnounce(t, port, group, id, val)
	for {
		select {
		case ev := <-events:
			return ev
		case <-resend.C:
			sendAnnounce(t, port, group, id, val)
		case <-deadline:
			t.Fatalf("announcement group %v id %d never delivered, resent every 100ms for 10s", group, id)
		}
	}
}

func TestSubscribe_NotConnected(t *testing.T) {
	p := &Plugin{}
	if err := p.Subscribe(consumer.ValueRequest{Slot: 0, Group: "control", ID: 0}, func(consumer.Event) {}); err != consumer.ErrNotConnected {
		t.Fatalf("err = %v, want ErrNotConnected", err)
	}
}

func TestSubscribe_TypedEvent(t *testing.T) {
	p, port := subscribePlugin(t)
	events := make(chan consumer.Event, 4)
	if err := p.Subscribe(consumer.ValueRequest{Slot: 0, Group: "control", ID: 0},
		func(ev consumer.Event) { events <- ev }); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	if ev := awaitAnnounce(t, port, codec.GroupControl, 0, []byte{0x00, 0x07}, events); ev.Label != "Level" || ev.Value.Int != 7 {
		t.Errorf("event = %+v, want Level=7", ev)
	}

	if err := p.Unsubscribe(consumer.ValueRequest{Slot: 0, Group: "control", ID: 0}); err != nil {
		t.Errorf("Unsubscribe: %v", err)
	}
	// Unsubscribe of a never-registered request is a no-op.
	if err := p.Unsubscribe(consumer.ValueRequest{Slot: 9, Group: "status", ID: 1}); err != nil {
		t.Errorf("Unsubscribe no-op: %v", err)
	}
}

func TestSubscribe_LabelResolution(t *testing.T) {
	p, port := subscribePlugin(t)
	events := make(chan consumer.Event, 4)
	// Label-addressed subscription resolves "Level" → control/0 via the tree.
	if err := p.Subscribe(consumer.ValueRequest{Slot: 0, Group: "control", Label: "Level"},
		func(ev consumer.Event) { events <- ev }); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if ev := awaitAnnounce(t, port, codec.GroupControl, 0, []byte{0x00, 0x09}, events); ev.Value.Int != 9 {
		t.Errorf("label-resolved event value = %d, want 9", ev.Value.Int)
	}
}

func TestSubscribe_FrameFallback(t *testing.T) {
	p, port := subscribePlugin(t)
	events := make(chan consumer.Event, 4)
	// Wildcard subscription; frame-status objects are never in the walked
	// tree so the wrapper uses decodeByGroup → KindFrame.
	if err := p.Subscribe(consumer.ValueRequest{Slot: -1, Group: "", ID: -1},
		func(ev consumer.Event) { events <- ev }); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if ev := awaitAnnounce(t, port, codec.GroupFrame, 0, []byte{2, 2, 0}, events); ev.Value.Kind != consumer.KindFrame {
		t.Errorf("frame event kind = %v, want KindFrame", ev.Value.Kind)
	}
}

func TestSubscribe_LabelResolutionFails(t *testing.T) {
	p, _ := subscribePlugin(t)
	// Unknown label with no matching tree entry → resolve error.
	err := p.Subscribe(consumer.ValueRequest{Slot: 0, Group: "control", Label: "Nonexistent"},
		func(consumer.Event) {})
	if err == nil {
		t.Fatal("Subscribe with unresolvable label: want error")
	}
}
