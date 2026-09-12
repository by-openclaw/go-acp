package rollcall

import (
	"sync"
	"time"
)

// Compliance events name the ways a client can depart from the specification
// while still being served.
//
// The posture is the repository's, with the roles exchanged: a provider absorbs
// what a client gets wrong and keeps serving it, but never silently. A count
// and a summary is what an operator takes to whoever wrote the panel.
const (
	// EventUnservedService means a client asked for a service this device does
	// not supply. Services are all-or-nothing, so the call is refused rather
	// than granted in part; the event says which service, because "it will not
	// connect" is otherwise all anybody has to go on.
	EventUnservedService = "rollcall_unserved_service"

	// EventUnknownCommand means a client read or wrote a command number that
	// is not in the menu this card serves. It has either cached a menu that
	// has since changed or invented a number.
	EventUnknownCommand = "rollcall_unknown_command"

	// EventUnknownFileHandle means a file operation named a handle this link
	// never issued, or one it issued and has since closed.
	EventUnknownFileHandle = "rollcall_unknown_file_handle"

	// EventPushDropped means a subscriber fell so far behind that a change was
	// dropped rather than queued. Every push must be acknowledged before the
	// next is sent, so a client that stops acknowledging stops the stream; the
	// values it missed are still readable, and the event says it missed them.
	EventPushDropped = "rollcall_push_dropped"

	// EventMixedGeneration means a request arrived whose wire generation is
	// not the one its session negotiated: a 32-bit message on a 16-bit
	// session, or the reverse. The two generations use different message
	// numbers for the same job, so a client that mixes them is reading its own
	// replies in a shape it did not ask for. We answer it anyway — the message
	// itself is well formed and refusing would break a client that works — and
	// count it, because without this a mixed message left no trace at all.
	EventMixedGeneration = "rollcall_mixed_generation"

	// EventInvalidUserLevel means a call named a user level outside the four
	// the specification defines. It is refused: the level decides which menu
	// lines are shown, so guessing one would show a client something it may
	// not be entitled to.
	EventInvalidUserLevel = "rollcall_invalid_user_level"
)

// ComplianceEvent is one recorded deviation.
type ComplianceEvent struct {
	Name   string
	Detail string
	At     time.Time
	Count  int
}

// compliance collects deviations without unbounded growth: one entry per kind,
// with a count and the most recent detail. A client that misbehaves on every
// one of sixty-five thousand destinations must not fill memory with the
// evidence.
type compliance struct {
	mu     sync.Mutex
	events map[string]*ComplianceEvent
	order  []string
}

func (c *compliance) fire(name, detail string, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.events == nil {
		c.events = make(map[string]*ComplianceEvent)
	}
	e, ok := c.events[name]
	if !ok {
		e = &ComplianceEvent{Name: name}
		c.events[name] = e
		c.order = append(c.order, name)
	}
	e.Detail = detail
	e.At = now
	e.Count++
}

// snapshot returns the events in the order they were first seen.
func (c *compliance) snapshot() []ComplianceEvent {
	c.mu.Lock()
	defer c.mu.Unlock()

	out := make([]ComplianceEvent, 0, len(c.order))
	for _, name := range c.order {
		out = append(out, *c.events[name])
	}
	return out
}

// fire records a deviation and logs it once per kind at warning level.
//
// Repeats are counted rather than logged, because a client that deviates on
// every line of a large menu would otherwise bury everything else in the log.
func (p *Provider) fire(name, detail string) {
	first := false
	p.comp.mu.Lock()
	if p.comp.events == nil || p.comp.events[name] == nil {
		first = true
	}
	p.comp.mu.Unlock()

	p.comp.fire(name, detail, p.clk.Now())
	if first {
		p.log.Warn("rollcall: compliance", "event", name, "detail", detail)
	}
}

// ComplianceEvents returns the deviations seen so far, most-recently-updated
// detail per kind, in the order the kinds were first seen.
func (p *Provider) ComplianceEvents() []ComplianceEvent { return p.comp.snapshot() }
