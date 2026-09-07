package rollcall

import (
	"sync"
	"time"
)

// Compliance events name the ways a peer can depart from the specification
// while still being usable.
//
// The posture is the repository's: absorb the deviation and keep running, but
// never work around one silently. A caller sees the count and the summary, and
// an operator chasing a fault has something specific to take to a vendor.
const (
	// EventLongStringsRefused means a peer advertised the long-string
	// service and then refused a call that asked for it. The two statements
	// cannot both be true. We fall back a generation, which works, and say
	// so, because the fallback is otherwise invisible and changes which
	// commands are reachable.
	EventLongStringsRefused = "rollcall_long_strings_refused"

	// EventMenuSpanOverruns means a container line claimed a subtree longer
	// than the menu that contains it. The span is clamped to what is there,
	// so the tree is still built, but the line count and the claim disagree.
	EventMenuSpanOverruns = "rollcall_menu_span_overruns"

	// EventMenuCountMismatch means a device delivered a different number of
	// menu lines than it announced. The lines that arrived are kept.
	EventMenuCountMismatch = "rollcall_menu_count_mismatch"

	// EventDuplicateCommand means two menu lines on one port claimed the
	// same command number. Both are kept, and the first keeps the label,
	// because a value written to either would reach the same place.
	EventDuplicateCommand = "rollcall_duplicate_command"

	// EventAccessGated means a menu line was replaced by the placeholder a
	// server substitutes for a line above the session's user level. It is
	// not a fault: it is how the protocol hides a line. It is reported so
	// that "the menu is shorter than the manual says" has an answer.
	EventAccessGated = "rollcall_access_gated"

	// EventValueModeEmpty means a value reply set neither the value, the
	// string nor the data flag, so it carries nothing. The read returns an
	// empty value of the object's kind rather than failing.
	EventValueModeEmpty = "rollcall_value_mode_empty"

	// EventUnsolicitedSession means a push arrived on a session index we
	// hold, naming a command the walked menu does not contain. It is
	// delivered, because the device knows its own commands better than a
	// cached walk does.
	EventUnsolicitedCommand = "rollcall_unsolicited_command"

	// EventUnlistedNode means a slot was addressed that the device's own
	// enumeration does not mention. It is addressed anyway: a gateway ages a
	// map entry out after sixty seconds of silence, so a node missing from the
	// list is one that has gone quiet rather than one that never existed.
	EventUnlistedNode = "rollcall_unlisted_node"

	// EventDisplayLineOutOfRange means a status line arrived for a line
	// number outside the four the display service defines and outside the
	// two negative priorities. It is kept under its own number.
	EventDisplayLineOutOfRange = "rollcall_display_line_out_of_range"
)

// ComplianceEvent is one recorded deviation.
type ComplianceEvent struct {
	Name   string
	Detail string
	At     time.Time
	Count  int
}

// compliance collects deviations without unbounded growth: one entry per kind,
// with a count and the most recent detail. A device that misbehaves on every
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
// Repeats are counted rather than logged, because a device that deviates on
// every line of a large menu would otherwise bury everything else in the log.
func (p *Plugin) fire(name, detail string) {
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
func (p *Plugin) ComplianceEvents() []ComplianceEvent { return p.comp.snapshot() }
