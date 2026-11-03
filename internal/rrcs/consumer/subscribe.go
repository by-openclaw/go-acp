package consumer

import (
	"context"
	"strconv"
	"strings"
	"sync"

	dhsc "dhs/internal/consumer"
)

// Subscriptions. The plugin opens no socket (sockets stay out of the
// protocol packages): the host binds a listener, serves EventHandler on
// it, and names its port with SetCallback. Subscribe then registers for
// every event once (§8.15.1 RegisterForAllEvents, "Allows to receive any
// RRCS event"); RRCS sends to the source address of that request.

type subscriber struct {
	path string
	fn   dhsc.EventFunc
}

type subscriptions struct {
	mu         sync.Mutex
	handler    *Listener
	port       int
	path       string
	subs       []subscriber
	registered bool
}

// EventHandler is what the host serves where RRCS sends its events.
func (p *Plugin) EventHandler() *Listener {
	p.events.mu.Lock()
	defer p.events.mu.Unlock()
	if p.events.handler == nil {
		p.events.handler = &Listener{AnyPath: true, OnEvent: p.onEvent}
	}
	return p.events.handler
}

// SetCallback names the port (and URL path, empty for the default) the
// host serves EventHandler on.
func (p *Plugin) SetCallback(port int, path string) {
	p.events.mu.Lock()
	p.events.port, p.events.path = port, path
	p.events.mu.Unlock()
}

// Subscribe asks for every event once and adds the callback. A request
// with a path narrows the callback to the lines under that path; an
// empty path receives everything.
func (p *Plugin) Subscribe(req dhsc.ValueRequest, fn dhsc.EventFunc) error {
	p.mu.Lock()
	client := p.client
	p.mu.Unlock()
	if client == nil {
		return dhsc.ErrNotConnected
	}
	p.events.mu.Lock()
	port, path := p.events.port, p.events.path
	registered := p.events.registered
	p.events.subs = append(p.events.subs, subscriber{path: req.Path, fn: fn})
	p.events.mu.Unlock()
	if port == 0 {
		return dhsc.ErrNotImplemented // no callback named: SetCallback first
	}
	if registered {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), DefaultTimeout)
	defer cancel()
	if _, err := client.Register(ctx, Registration{Port: port, Path: path}); err != nil {
		return err
	}
	p.events.mu.Lock()
	p.events.registered = true
	p.events.mu.Unlock()
	return nil
}

// Unsubscribe removes the callbacks of a path; the last one removes the
// registration.
func (p *Plugin) Unsubscribe(req dhsc.ValueRequest) error {
	p.mu.Lock()
	client := p.client
	p.mu.Unlock()
	p.events.mu.Lock()
	kept := p.events.subs[:0]
	for _, s := range p.events.subs {
		if s.path != req.Path {
			kept = append(kept, s)
		}
	}
	p.events.subs = kept
	last := len(kept) == 0 && p.events.registered
	port, path := p.events.port, p.events.path
	if last {
		p.events.registered = false
	}
	p.events.mu.Unlock()
	if !last || client == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), DefaultTimeout)
	defer cancel()
	_, err := client.Unregister(ctx, Registration{Port: port, Path: path})
	return err
}

// onEvent turns one notification into the values it carries and hands
// each to the subscribers of its path.
func (p *Plugin) onEvent(e Event) {
	lines := Decode(nil, e)
	p.events.mu.Lock()
	subs := append([]subscriber(nil), p.events.subs...)
	p.events.mu.Unlock()
	for _, l := range lines {
		ev := lineEvent(l, e)
		for _, s := range subs {
			if s.path == "" || l.Path == s.path || strings.HasPrefix(l.Path, s.path+".") {
				s.fn(ev)
			}
		}
	}
}

// lineEvent is the shared event of one decoded line.
func lineEvent(l ChangeLine, e Event) dhsc.Event {
	ev := dhsc.Event{
		Path: l.Path + "." + l.Label, OID: "", Label: l.Label,
		Description: strings.TrimSpace(l.Name + " " + l.Detail),
		Unit:        l.Unit, Timestamp: e.Time, Freshness: "live",
	}
	if l.OID != 0 {
		ev.OID = strconv.Itoa(l.OID)
		ev.ID = l.OID
	}
	kind, _ := l.kindEnum()
	switch kind {
	case "bool":
		ev.Value = dhsc.Value{Kind: dhsc.KindBool, Bool: l.Value == "true"}
	case "int":
		n, _ := strconv.ParseInt(l.Value, 10, 64)
		ev.Value = dhsc.Value{Kind: dhsc.KindInt, Int: n}
	default:
		ev.Value = dhsc.Value{Kind: dhsc.KindString, Str: l.Value}
	}
	return ev
}
