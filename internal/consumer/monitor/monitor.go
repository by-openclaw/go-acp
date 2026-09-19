package monitor

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"dhs/internal/clock"
	"dhs/internal/consumer"
)

// Monitor supervises one actor per device and exposes a single
// subscribe seam for downstream consumers (ADR-0030).
type Monitor struct {
	clk clock.Clock
	log *slog.Logger
	bus *Bus

	mu      sync.Mutex
	devices map[string]*deviceRuntime
}

type deviceRuntime struct {
	w      *worker
	cancel context.CancelFunc
	done   chan struct{}
}

// Device is the configuration to place one device under the monitor.
type Device struct {
	Name    string
	Proto   consumer.Protocol
	Profile *Profile

	// MinGap paces wire operations so a fragile agent is not charged.
	// Zero disables pacing.
	MinGap time.Duration

	// AssertControl, when set, is the control-mode guard invoked by
	// AssertControl(device). It reclaims the device (e.g. re-force
	// remote/serial) so a front-panel operator cannot silently take over.
	AssertControl func(context.Context) error
}

// Option configures a Monitor.
type Option func(*Monitor)

// WithClock injects a clock; tests pass clock.NewFake.
func WithClock(c clock.Clock) Option { return func(m *Monitor) { m.clk = c } }

// WithLogger injects a logger.
func WithLogger(l *slog.Logger) Option { return func(m *Monitor) { m.log = l } }

// New builds a Monitor. Defaults: real clock, discard logger.
func New(opts ...Option) *Monitor {
	m := &Monitor{
		clk:     clock.System(),
		log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		bus:     newBus(),
		devices: make(map[string]*deviceRuntime),
	}
	for _, o := range opts {
		o(m)
	}
	return m
}

// Subscribe registers a downstream listener. buf bounds its channel;
// filter nil means all events. Returns an id for Unsubscribe.
func (m *Monitor) Subscribe(buf int, filter func(consumer.Event) bool) (int, <-chan consumer.Event) {
	return m.bus.Subscribe(buf, filter)
}

// Unsubscribe drops a listener.
func (m *Monitor) Unsubscribe(id int) { m.bus.Unsubscribe(id) }

// Drops reports events shed under backpressure across all subscribers.
func (m *Monitor) Drops() uint64 { return m.bus.Drops() }

// Add places a device under the monitor and starts its worker and
// scheduler. It fails on a duplicate name, a nil field, or an invalid
// profile. The device stops when parent is cancelled, Remove is called,
// or Stop is called.
func (m *Monitor) Add(parent context.Context, d Device) error {
	if d.Name == "" {
		return fmt.Errorf("monitor: device name required")
	}
	if d.Proto == nil {
		return fmt.Errorf("monitor: device %q has nil Protocol", d.Name)
	}
	if d.Profile == nil {
		return fmt.Errorf("monitor: device %q has nil Profile", d.Name)
	}
	if err := d.Profile.Validate(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.devices[d.Name]; ok {
		return fmt.Errorf("monitor: device %q already added", d.Name)
	}

	ctx, cancel := context.WithCancel(parent)
	buf := len(d.Profile.Entries)
	if buf < 8 {
		buf = 8
	}
	w := &worker{
		name:   d.Name,
		proto:  d.Proto,
		bus:    m.bus,
		clk:    m.clk,
		log:    m.log,
		minGap: d.MinGap,
		assert: d.AssertControl,
		ops:    make(chan command, 16),
		reads:  make(chan command, buf),
		last:   make(map[string]consumer.Value),
	}
	s := newScheduler(d.Profile, m.clk, w.reads)

	done := make(chan struct{})
	m.devices[d.Name] = &deviceRuntime{w: w, cancel: cancel, done: done}

	go func() {
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); w.run(ctx) }()
		go func() { defer wg.Done(); s.run(ctx) }()
		wg.Wait()
		close(done)
	}()
	return nil
}

// Set performs a confirmed operator write. It jumps ahead of scheduled
// reads and returns the device-confirmed value.
func (m *Monitor) Set(ctx context.Context, device string, req consumer.ValueRequest, val consumer.Value) (consumer.Value, error) {
	rt, err := m.runtime(device)
	if err != nil {
		return consumer.Value{}, err
	}
	res := make(chan writeResult, 1)
	c := command{kind: cmdWrite, key: addrKey(req), req: req, val: val, result: res}
	select {
	case rt.w.ops <- c:
	case <-ctx.Done():
		return consumer.Value{}, ctx.Err()
	}
	select {
	case r := <-res:
		return r.val, r.err
	case <-ctx.Done():
		return consumer.Value{}, ctx.Err()
	}
}

// AssertControl enqueues the device's control-mode guard on the
// priority queue. It returns once the command is queued.
func (m *Monitor) AssertControl(ctx context.Context, device string) error {
	rt, err := m.runtime(device)
	if err != nil {
		return err
	}
	select {
	case rt.w.ops <- command{kind: cmdAssert}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Stats returns a snapshot of a device's counters.
func (m *Monitor) Stats(device string) (Stats, bool) {
	rt, err := m.runtime(device)
	if err != nil {
		return Stats{}, false
	}
	return Stats{
		Reads:   rt.w.mReads.Load(),
		Writes:  rt.w.mWrites.Load(),
		Changes: rt.w.mChanges.Load(),
		Errors:  rt.w.mErrors.Load(),
	}, true
}

// Stats are per-device counters.
type Stats struct {
	Reads, Writes, Changes, Errors uint64
}

// Remove stops one device and waits for its goroutines to exit.
func (m *Monitor) Remove(device string) {
	m.mu.Lock()
	rt, ok := m.devices[device]
	if ok {
		delete(m.devices, device)
	}
	m.mu.Unlock()
	if ok {
		rt.cancel()
		<-rt.done
	}
}

// Stop stops every device and waits for all goroutines to exit.
func (m *Monitor) Stop() {
	m.mu.Lock()
	rts := make([]*deviceRuntime, 0, len(m.devices))
	for _, rt := range m.devices {
		rts = append(rts, rt)
	}
	m.devices = make(map[string]*deviceRuntime)
	m.mu.Unlock()
	for _, rt := range rts {
		rt.cancel()
	}
	for _, rt := range rts {
		<-rt.done
	}
}

func (m *Monitor) runtime(device string) (*deviceRuntime, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rt, ok := m.devices[device]
	if !ok {
		return nil, fmt.Errorf("monitor: unknown device %q", device)
	}
	return rt, nil
}
