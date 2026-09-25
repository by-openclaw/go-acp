package pollwatch

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"dhs/internal/consumer"
	"dhs/internal/consumer/monitor"
)

// fakeProto is a push-less device: values live in a map, GetValue
// reads them, nothing announces. Exactly the case the Poller is for.
type fakeProto struct {
	consumer.Protocol // nil: only GetValue is used by the monitor's reads
	mu                sync.Mutex
	vals              map[string]int64
	reads             atomic.Int64
}

func (f *fakeProto) GetValue(ctx context.Context, req consumer.ValueRequest) (consumer.Value, error) {
	f.reads.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.vals[req.Path]
	if !ok {
		return consumer.Value{}, consumer.ErrObjectNotFound
	}
	return consumer.Value{Kind: consumer.KindInt, Int: v}, nil
}

func (f *fakeProto) set(path string, v int64) {
	f.mu.Lock()
	f.vals[path] = v
	f.mu.Unlock()
}

func profileOf(paths ...string) ProfileFunc {
	return func(ctx context.Context, req consumer.ValueRequest) (*monitor.Profile, error) {
		p := &monitor.Profile{Model: "fake", Defaults: monitor.Defaults{Interval: monitor.Duration(30 * time.Millisecond), OnChange: true}}
		for _, x := range paths {
			p.Entries = append(p.Entries, monitor.Entry{Path: x, Slot: req.Slot})
		}
		return p, nil
	}
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestSubscribeDeliversChangesUntilUnsubscribe(t *testing.T) {
	dev := &fakeProto{vals: map[string]int64{"a.pkt_cnt": 1, "b.temp": 40}}
	p := New(dev, profileOf("a.pkt_cnt", "b.temp"), quiet(), nil)

	var mu sync.Mutex
	var got []consumer.Event
	req := consumer.ValueRequest{Slot: 0}
	if err := p.Subscribe(req, func(ev consumer.Event) { mu.Lock(); got = append(got, ev); mu.Unlock() }); err != nil {
		t.Fatal(err)
	}
	if p.Active() != 1 {
		t.Errorf("Active = %d", p.Active())
	}
	// first samples arrive, then a change on one address only
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	dev.set("a.pkt_cnt", 2)
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		last := consumer.Event{}
		if len(got) > 0 {
			last = got[len(got)-1]
		}
		mu.Unlock()
		if last.Path == "a.pkt_cnt" && last.Value.Int == 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	seen := map[string]int64{}
	for _, ev := range got {
		seen[ev.Path] = ev.Value.Int
	}
	mu.Unlock()
	if seen["a.pkt_cnt"] != 2 || seen["b.temp"] != 40 {
		t.Errorf("events = %v", seen)
	}
	if err := p.Subscribe(req, func(consumer.Event) {}); err == nil || !strings.Contains(err.Error(), "already subscribed") {
		t.Errorf("duplicate err = %v", err)
	}
	if err := p.Unsubscribe(req); err != nil || p.Active() != 0 {
		t.Errorf("Unsubscribe = %v, active %d", err, p.Active())
	}
	before := dev.reads.Load()
	time.Sleep(100 * time.Millisecond)
	if dev.reads.Load() != before {
		t.Error("polling must stop after Unsubscribe")
	}
	if err := p.Unsubscribe(req); err != nil {
		t.Errorf("unsubscribing an unknown scope is not an error: %v", err)
	}
}

func TestSubscribeErrors(t *testing.T) {
	dev := &fakeProto{vals: map[string]int64{}}
	p := New(dev, func(context.Context, consumer.ValueRequest) (*monitor.Profile, error) {
		return nil, errors.New("no walk")
	}, nil, nil)
	if err := p.Subscribe(consumer.ValueRequest{}, nil); err == nil || !strings.Contains(err.Error(), "nil event func") {
		t.Errorf("nil fn err = %v", err)
	}
	if err := p.Subscribe(consumer.ValueRequest{}, func(consumer.Event) {}); err == nil || !strings.Contains(err.Error(), "profile: no walk") {
		t.Errorf("profile err = %v", err)
	}
	// An empty profile is refused by the monitor's validation.
	p2 := New(dev, profileOf(), quiet(), nil)
	if err := p2.Subscribe(consumer.ValueRequest{}, func(consumer.Event) {}); err == nil || !strings.Contains(err.Error(), "no entries") {
		t.Errorf("empty profile err = %v", err)
	}
}

func TestCloseStopsEverything(t *testing.T) {
	dev := &fakeProto{vals: map[string]int64{"x": 1, "y": 2}}
	p := New(dev, profileOf("x"), quiet(), nil)
	for _, r := range []consumer.ValueRequest{{Slot: 0}, {Slot: 1}} {
		if err := p.Subscribe(r, func(consumer.Event) {}); err != nil {
			t.Fatal(err)
		}
	}
	if p.Active() != 2 {
		t.Fatalf("Active = %d", p.Active())
	}
	p.Close()
	if p.Active() != 0 {
		t.Errorf("Active after Close = %d", p.Active())
	}
	before := dev.reads.Load()
	time.Sleep(100 * time.Millisecond)
	if dev.reads.Load() != before {
		t.Error("polling must stop after Close")
	}
}
