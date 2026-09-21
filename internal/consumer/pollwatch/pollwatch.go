// Package pollwatch gives a connector without a push channel a working
// Subscribe: "poll if no push" (ADR-0030 applied at the plugin edge).
//
// A REST device (Riedel MuoN/FusioN, EVS Neuron CCM) never announces;
// the only way to see a value move is to read it again. Instead of
// every such connector growing its own timer loop, it embeds a Poller
// and answers Subscribe/Unsubscribe with it. The Poller places the
// plugin under the neutral monitor — per-address intervals from a
// profile the plugin supplies, deterministic jitter, one serial worker
// per subscription, change detection — and hands each delta to the
// subscriber's EventFunc. The operator's `watch` verb is unchanged.
package pollwatch

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"dhs/internal/clock"
	"dhs/internal/consumer"
	"dhs/internal/consumer/monitor"
)

// ProfileFunc builds the poll plan for one Subscribe request: which
// addresses, at which interval. The plugin knows its model (its
// dictionary carries the intervals) and the request's scope (slot,
// path filter); the Poller knows neither.
type ProfileFunc func(ctx context.Context, req consumer.ValueRequest) (*monitor.Profile, error)

// Poller answers Subscribe/Unsubscribe by polling.
type Poller struct {
	proto   consumer.Protocol
	profile ProfileFunc
	log     *slog.Logger
	clk     clock.Clock

	mu   sync.Mutex
	subs map[string]*subscription
}

type subscription struct {
	mon    *monitor.Monitor
	cancel context.CancelFunc
	done   chan struct{}
}

// New builds a Poller for proto. log and clk nil take the defaults.
func New(proto consumer.Protocol, profile ProfileFunc, log *slog.Logger, clk clock.Clock) *Poller {
	if log == nil {
		log = slog.Default()
	}
	if clk == nil {
		clk = clock.System()
	}
	return &Poller{proto: proto, profile: profile, log: log, clk: clk, subs: map[string]*subscription{}}
}

// key is the identity of a request: two Subscribes for the same scope
// are the same subscription.
func key(r consumer.ValueRequest) string {
	return fmt.Sprintf("s=%d|p=%s|l=%s|g=%s|id=%d", r.Slot, r.Path, r.Label, r.Group, r.ID)
}

// Subscribe starts polling the scope of req and delivers every change
// to fn until Unsubscribe. The profile is built once, at Subscribe.
func (p *Poller) Subscribe(req consumer.ValueRequest, fn consumer.EventFunc) error {
	if fn == nil {
		return fmt.Errorf("pollwatch: nil event func")
	}
	k := key(req)
	p.mu.Lock()
	if _, dup := p.subs[k]; dup {
		p.mu.Unlock()
		return fmt.Errorf("pollwatch: already subscribed to %s", k)
	}
	p.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	prof, err := p.profile(ctx, req)
	if err != nil {
		cancel()
		return fmt.Errorf("pollwatch: profile: %w", err)
	}
	mon := monitor.New(monitor.WithClock(p.clk), monitor.WithLogger(p.log))
	if err := mon.Add(ctx, monitor.Device{Name: k, Proto: p.proto, Profile: prof}); err != nil {
		cancel()
		return fmt.Errorf("pollwatch: %w", err)
	}
	id, ch := mon.Subscribe(256, nil)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer mon.Unsubscribe(id)
		for {
			select {
			case <-ctx.Done():
				return
			case ev := <-ch:
				fn(ev)
			}
		}
	}()

	p.mu.Lock()
	p.subs[k] = &subscription{mon: mon, cancel: cancel, done: done}
	p.mu.Unlock()
	p.log.Info("pollwatch: subscribed", slog.String("scope", k), slog.Int("addresses", len(prof.Entries)))
	return nil
}

// Unsubscribe stops the scope's polling. Unknown scopes are not an
// error: watch tears down defensively.
func (p *Poller) Unsubscribe(req consumer.ValueRequest) error {
	k := key(req)
	p.mu.Lock()
	s, ok := p.subs[k]
	delete(p.subs, k)
	p.mu.Unlock()
	if !ok {
		return nil
	}
	s.cancel()
	s.mon.Stop()
	<-s.done
	return nil
}

// Close stops every subscription (plugin Disconnect).
func (p *Poller) Close() {
	p.mu.Lock()
	subs := p.subs
	p.subs = map[string]*subscription{}
	p.mu.Unlock()
	for _, s := range subs {
		s.cancel()
		s.mon.Stop()
		<-s.done
	}
}

// Active reports how many scopes are being polled.
func (p *Poller) Active() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.subs)
}
