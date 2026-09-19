package monitor

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"dhs/internal/clock"
	"dhs/internal/consumer"
)

func discardLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func recvEvent(t *testing.T, ch <-chan consumer.Event) consumer.Event {
	t.Helper()
	select {
	case ev := <-ch:
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
		return consumer.Event{}
	}
}

// recvEventWhere reads events until one satisfies pred, tolerating
// interleaved scheduled reads.
func recvEventWhere(t *testing.T, ch <-chan consumer.Event, pred func(consumer.Event) bool) consumer.Event {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-ch:
			if pred(ev) {
				return ev
			}
		case <-deadline:
			t.Fatal("timed out waiting for matching event")
			return consumer.Event{}
		}
	}
}

func assertNoEvent(t *testing.T, ch <-chan consumer.Event) {
	t.Helper()
	select {
	case ev := <-ch:
		t.Fatalf("unexpected event: %+v", ev)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestObserveChangeDetection(t *testing.T) {
	fk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	b := newBus()
	_, ch := b.Subscribe(8, nil)
	w := &worker{name: "d", bus: b, clk: fk, log: discardLog(), last: make(map[string]consumer.Value)}

	req := consumer.ValueRequest{Path: "x"}
	key := addrKey(req)

	w.observe(key, req, intVal(1), true, "live") // first sight → emit
	ev1 := recvEvent(t, ch)
	if ev1.Value.Int != 1 || ev1.Freshness != "live" {
		t.Errorf("ev1 = %+v, want value 1 live", ev1)
	}
	if len(ev1.Changes) != 0 {
		t.Errorf("first sight should carry no Changes, got %+v", ev1.Changes)
	}

	w.observe(key, req, intVal(1), true, "live") // unchanged → suppressed
	assertNoEvent(t, ch)

	w.observe(key, req, intVal(2), true, "live") // changed → emit with delta
	ev2 := recvEvent(t, ch)
	if ev2.Value.Int != 2 {
		t.Errorf("ev2 value = %d, want 2", ev2.Value.Int)
	}
	if len(ev2.Changes) != 1 || ev2.Changes[0].Old != "1" || ev2.Changes[0].New != "2" {
		t.Errorf("ev2 Changes = %+v, want [{value 1 2}]", ev2.Changes)
	}
	if got := w.mChanges.Load(); got != 2 {
		t.Errorf("mChanges = %d, want 2", got)
	}
}

func TestMonitorPollsAndEmits(t *testing.T) {
	fk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	fp := newFakeProto()
	req := consumer.ValueRequest{Path: "1.3.6.1.4.1.1773.1.1.10"}
	fp.set(addrKey(req), intVal(5))

	m := New(WithClock(fk), WithLogger(discardLog()))
	defer m.Stop()
	_, evc := m.Subscribe(64, nil)

	p := &Profile{
		Defaults: Defaults{Interval: Duration(time.Second), OnChange: true},
		Entries:  []Entry{{OID: req.Path}},
	}
	if err := m.Add(context.Background(), Device{Name: "ird", Proto: fp, Profile: p}); err != nil {
		t.Fatal(err)
	}

	fk.Advance(time.Second)
	ev := recvEventWhere(t, evc, func(e consumer.Event) bool { return e.Value.Int == 5 })
	if ev.Path != req.Path {
		t.Errorf("event path = %q, want %q", ev.Path, req.Path)
	}
}

func TestMonitorOnChangeStaysSilentWithoutMovement(t *testing.T) {
	fk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	fp := newFakeProto()
	req := consumer.ValueRequest{Path: "1.1.1"}
	fp.set(addrKey(req), intVal(7))

	m := New(WithClock(fk), WithLogger(discardLog()))
	defer m.Stop()
	_, evc := m.Subscribe(64, nil)

	p := &Profile{
		Defaults: Defaults{Interval: Duration(time.Second), OnChange: true},
		Entries:  []Entry{{OID: req.Path}},
	}
	if err := m.Add(context.Background(), Device{Name: "d", Proto: fp, Profile: p}); err != nil {
		t.Fatal(err)
	}

	fk.Advance(3 * time.Second) // several polls, value never moves
	recvEventWhere(t, evc, func(e consumer.Event) bool { return e.Value.Int == 7 })
	assertNoEvent(t, evc) // no duplicate for an unchanged value
}

func TestMonitorConfirmedWrite(t *testing.T) {
	fk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	fp := newFakeProto()
	req := consumer.ValueRequest{Path: "ctrl"}
	fp.set(addrKey(req), intVal(1))

	m := New(WithClock(fk), WithLogger(discardLog()))
	defer m.Stop()
	_, evc := m.Subscribe(64, nil)

	// Long interval so no scheduled read competes with the write.
	p := &Profile{
		Defaults: Defaults{Interval: Duration(time.Hour), OnChange: true},
		Entries:  []Entry{{OID: "other"}},
	}
	if err := m.Add(context.Background(), Device{Name: "d", Proto: fp, Profile: p}); err != nil {
		t.Fatal(err)
	}

	got, err := m.Set(context.Background(), "d", req, intVal(9))
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got.Int != 9 {
		t.Errorf("confirmed value = %d, want 9", got.Int)
	}
	if fp.setCount() != 1 {
		t.Errorf("SetValue calls = %d, want 1", fp.setCount())
	}
	if fp.getCount() < 1 {
		t.Errorf("expected a confirm read-back, GetValue calls = %d", fp.getCount())
	}
	ev := recvEventWhere(t, evc, func(e consumer.Event) bool { return e.Value.Int == 9 })
	if ev.Path != "ctrl" {
		t.Errorf("write event path = %q, want ctrl", ev.Path)
	}
}

func TestMonitorWriteErrorPropagates(t *testing.T) {
	fp := newFakeProto()
	req := consumer.ValueRequest{Path: "ctrl"}
	fp.setErr[addrKey(req)] = errors.New("device refused")

	m := New(WithClock(clock.NewFake(time.Time{})), WithLogger(discardLog()))
	defer m.Stop()

	p := &Profile{Defaults: Defaults{Interval: Duration(time.Hour)}, Entries: []Entry{{OID: "other"}}}
	if err := m.Add(context.Background(), Device{Name: "d", Proto: fp, Profile: p}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Set(context.Background(), "d", req, intVal(9)); err == nil {
		t.Fatal("expected write error, got nil")
	}
}

func TestMonitorAddValidations(t *testing.T) {
	m := New(WithLogger(discardLog()))
	defer m.Stop()
	p := &Profile{Defaults: Defaults{Interval: Duration(time.Second)}, Entries: []Entry{{OID: "x"}}}

	if err := m.Add(context.Background(), Device{Name: "", Proto: newFakeProto(), Profile: p}); err == nil {
		t.Error("empty name should fail")
	}
	if err := m.Add(context.Background(), Device{Name: "d", Proto: nil, Profile: p}); err == nil {
		t.Error("nil proto should fail")
	}
	if err := m.Add(context.Background(), Device{Name: "d", Proto: newFakeProto(), Profile: nil}); err == nil {
		t.Error("nil profile should fail")
	}
	if err := m.Add(context.Background(), Device{Name: "d", Proto: newFakeProto(), Profile: p}); err != nil {
		t.Fatalf("valid add: %v", err)
	}
	if err := m.Add(context.Background(), Device{Name: "d", Proto: newFakeProto(), Profile: p}); err == nil {
		t.Error("duplicate name should fail")
	}
}

func TestMonitorStopReturns(t *testing.T) {
	m := New(WithClock(clock.NewFake(time.Time{})), WithLogger(discardLog()))
	p := &Profile{Defaults: Defaults{Interval: Duration(time.Second)}, Entries: []Entry{{OID: "x"}}}
	for _, name := range []string{"a", "b", "c"} {
		if err := m.Add(context.Background(), Device{Name: name, Proto: newFakeProto(), Profile: p}); err != nil {
			t.Fatal(err)
		}
	}
	done := make(chan struct{})
	go func() { m.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return")
	}
	if _, ok := m.Stats("a"); ok {
		t.Error("device a should be gone after Stop")
	}
}
