package monitor

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"dhs/internal/clock"
	"dhs/internal/consumer"
)

func TestDurationJSONRoundTrip(t *testing.T) {
	b, err := json.Marshal(Duration(90 * time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `"1m30s"` {
		t.Errorf("marshal = %s, want \"1m30s\"", b)
	}
	var d Duration
	if err := json.Unmarshal([]byte(`"250ms"`), &d); err != nil {
		t.Fatal(err)
	}
	if time.Duration(d) != 250*time.Millisecond {
		t.Errorf("unmarshal = %v, want 250ms", time.Duration(d))
	}
	// A bare number is nanoseconds.
	if err := json.Unmarshal([]byte(`1000`), &d); err != nil {
		t.Fatal(err)
	}
	if time.Duration(d) != 1000*time.Nanosecond {
		t.Errorf("numeric unmarshal = %v, want 1µs", time.Duration(d))
	}
	// A non-string, non-number is rejected.
	if err := json.Unmarshal([]byte(`true`), &d); err == nil {
		t.Error("bool duration should be rejected")
	}
}

func TestValueStringRawAndEmpty(t *testing.T) {
	if got := valueString(consumer.Value{Raw: []byte{0xde, 0xad}}); got != "dead" {
		t.Errorf("raw value string = %q, want dead", got)
	}
	if got := valueString(consumer.Value{}); got != "" {
		t.Errorf("empty value string = %q, want empty", got)
	}
}

func TestWorkerReadErrorCountsAndStaysSilent(t *testing.T) {
	fk := clock.NewFake(time.Time{})
	b := newBus()
	_, ch := b.Subscribe(4, nil)
	fp := newFakeProto()
	req := consumer.ValueRequest{Path: "y"}
	fp.getErr[addrKey(req)] = errors.New("no response")

	w := &worker{name: "d", proto: fp, bus: b, clk: fk, log: discardLog(), last: make(map[string]consumer.Value)}
	w.doRead(context.Background(), command{kind: cmdRead, key: addrKey(req), req: req})

	if w.mErrors.Load() != 1 {
		t.Errorf("mErrors = %d, want 1", w.mErrors.Load())
	}
	select {
	case ev := <-ch:
		t.Fatalf("read error must not emit, got %+v", ev)
	default:
	}
}

func TestWorkerRateGapWaits(t *testing.T) {
	fk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	b := newBus()
	_, ch := b.Subscribe(4, nil)
	fp := newFakeProto()
	req := consumer.ValueRequest{Path: "z"}
	fp.set(addrKey(req), intVal(3))

	const gap = 50 * time.Millisecond
	w := &worker{
		name: "d", proto: fp, bus: b, clk: fk, log: discardLog(),
		minGap: gap, lastOp: fk.Now(), last: make(map[string]consumer.Value),
	}

	done := make(chan struct{})
	go func() {
		w.exec(context.Background(), command{kind: cmdRead, key: addrKey(req), req: req})
		close(done)
	}()

	// Wait for exec to arm its rate-gap sleep, then release it.
	for i := 0; i < 2000 && fk.Waiters() == 0; i++ {
		time.Sleep(time.Millisecond)
	}
	select {
	case <-done:
		t.Fatal("exec finished before the rate gap elapsed")
	default:
	}
	fk.Advance(gap)
	<-done

	ev := recvEvent(t, ch)
	if ev.Value.Int != 3 {
		t.Errorf("event value = %d, want 3", ev.Value.Int)
	}
}

func TestMonitorAssertControlInvokesHook(t *testing.T) {
	m := New(WithLogger(discardLog()))
	defer m.Stop()

	called := make(chan struct{}, 1)
	p := &Profile{Defaults: Defaults{Interval: Duration(time.Hour)}, Entries: []Entry{{OID: "x"}}}
	dev := Device{
		Name: "d", Proto: newFakeProto(), Profile: p,
		AssertControl: func(ctx context.Context) error { called <- struct{}{}; return nil },
	}
	if err := m.Add(context.Background(), dev); err != nil {
		t.Fatal(err)
	}
	if err := m.AssertControl(context.Background(), "d"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("control assert hook not invoked")
	}

	// Unknown device errors on both control paths.
	if err := m.AssertControl(context.Background(), "nope"); err == nil {
		t.Error("AssertControl on unknown device should error")
	}
	if _, err := m.Set(context.Background(), "nope", consumer.ValueRequest{Path: "x"}, intVal(1)); err == nil {
		t.Error("Set on unknown device should error")
	}
	if _, ok := m.Stats("nope"); ok {
		t.Error("Stats on unknown device should report not-ok")
	}
}

func TestMonitorRemoveAndBusPassthrough(t *testing.T) {
	m := New(WithLogger(discardLog()))
	p := &Profile{Defaults: Defaults{Interval: Duration(time.Hour)}, Entries: []Entry{{OID: "x"}}}
	if err := m.Add(context.Background(), Device{Name: "d", Proto: newFakeProto(), Profile: p}); err != nil {
		t.Fatal(err)
	}

	id, _ := m.Subscribe(2, nil)
	m.Unsubscribe(id)
	_ = m.Drops()

	m.Remove("d")
	if _, ok := m.Stats("d"); ok {
		t.Error("device should be gone after Remove")
	}
	m.Remove("d") // second remove is a no-op
	m.Stop()
}
