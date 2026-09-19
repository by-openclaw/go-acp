package monitor

import (
	"context"
	"fmt"
	"testing"
	"time"

	"dhs/internal/consumer"
)

func TestBusSubscribeClampsBuffer(t *testing.T) {
	b := newBus()
	_, ch := b.Subscribe(0, nil) // buf < 1 clamps to 1
	b.Publish(consumer.Event{Label: "a"})
	if ev := <-ch; ev.Label != "a" {
		t.Errorf("got %q, want a", ev.Label)
	}
}

func TestValueFloatEqualAndString(t *testing.T) {
	a := consumer.Value{Kind: consumer.KindFloat, Float: 1.5}
	b := consumer.Value{Kind: consumer.KindFloat, Float: 1.5}
	c := consumer.Value{Kind: consumer.KindFloat, Float: 2.5}
	if !valueEqual(a, b) {
		t.Error("equal floats should compare equal")
	}
	if valueEqual(a, c) {
		t.Error("different floats should compare unequal")
	}
	if got := valueString(a); got != "1.5" {
		t.Errorf("float string = %q, want 1.5", got)
	}
}

func TestDurationUnmarshalInvalidJSON(t *testing.T) {
	var d Duration
	if err := d.UnmarshalJSON([]byte(`{`)); err == nil {
		t.Error("invalid JSON should error")
	}
}

func TestAddBufferForLargeProfile(t *testing.T) {
	m := New(WithLogger(discardLog()))
	defer m.Stop()
	entries := make([]Entry, 10)
	for i := range entries {
		entries[i] = Entry{OID: fmt.Sprintf("1.%d", i), Interval: Duration(time.Hour)}
	}
	p := &Profile{Entries: entries}
	if err := m.Add(context.Background(), Device{Name: "big", Proto: newFakeProto(), Profile: p}); err != nil {
		t.Fatalf("add large profile: %v", err)
	}
	if _, ok := m.Stats("big"); !ok {
		t.Error("large-profile device should be live")
	}
}

func TestWorkerRunTopPriorityOp(t *testing.T) {
	w := newTestWorker(newFakeProto())
	w.ops = make(chan command, 2)
	w.reads = make(chan command, 1)

	sig := make(chan struct{}, 1)
	w.assert = func(ctx context.Context) error { sig <- struct{}{}; return nil }
	w.ops <- command{kind: cmdAssert} // preloaded so the top select takes the ops branch

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.run(ctx)

	select {
	case <-sig:
	case <-time.After(2 * time.Second):
		t.Fatal("preloaded priority op was not executed")
	}
}

func TestSetReturnsWhenContextCancelledWaitingConfirm(t *testing.T) {
	fp := newFakeProto()
	fp.setBlock = make(chan struct{}) // the write hangs on the wire

	req := consumer.ValueRequest{Path: "ctrl"}
	fp.set(addrKey(req), intVal(1))

	m := New(WithLogger(discardLog()))
	p := &Profile{Defaults: Defaults{Interval: Duration(time.Hour)}, Entries: []Entry{{OID: "other"}}}
	if err := m.Add(context.Background(), Device{Name: "d", Proto: fp, Profile: p}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() {
		_, err := m.Set(ctx, "d", req, intVal(9))
		errc <- err
	}()

	// The worker is now blocked inside SetValue; cancelling the caller's
	// context must free Set without waiting for the confirm.
	for i := 0; i < 2000 && fp.setCount() == 0; i++ {
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case err := <-errc:
		if err == nil {
			t.Fatal("Set should return the cancellation error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Set did not return after context cancel")
	}

	// Unblock the wire BEFORE Stop so the worker goroutine can exit,
	// otherwise Stop would deadlock waiting on a worker stuck in SetValue.
	close(fp.setBlock)
	m.Stop()
}
