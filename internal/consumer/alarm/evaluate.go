package alarm

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"dhs/internal/clock"
	"dhs/internal/consumer"
)

// The evaluator is a pure function over samples the caller already
// has: it reads no device, holds no connection and starts no
// goroutine. One comparison per event, one small state record per
// object a row covers — a value nobody wrote a rule for costs
// nothing.

// Transition is one change of verdict on one object: what it was,
// what it is now, and why.
type Transition struct {
	Device string
	Slot   int
	Path   string
	Label  string
	Unit   string

	// Text is the row's operator text ("SFP temperature").
	Text string

	// Value is the sample that caused the transition; Prev the one
	// before it.
	Value string
	Prev  string

	// Severity is the new verdict, Prior the one it replaces. A
	// transition to Normal is a clear; Severity > Prior is a
	// worsening.
	Severity Severity
	Prior    Severity

	// Band names the rule that fired: "high major", "low minor",
	// "value", "stalled", "drift", "unreadable", "normal".
	Band string

	// Flapping is set when this object crossed the row's flap cap;
	// the object is then silenced until it settles.
	Flapping bool

	At time.Time
}

// String renders a transition the way an operator reads it.
func (t Transition) String() string {
	verb := "raised"
	switch {
	case t.Severity == Normal:
		verb = "cleared"
	case t.Severity < t.Prior:
		verb = "eased"
	}
	if t.Flapping {
		verb = "flapping"
	}
	unit := ""
	if t.Unit != "" {
		unit = " " + t.Unit
	}
	text := ""
	if t.Text != "" {
		text = " — " + t.Text
	}
	return fmt.Sprintf("%s %s %s (%s): %s%s (was %s)%s",
		t.Severity, verb, t.Path, t.Band, t.Value, unit, t.Prev, text)
}

// Evaluator applies one template to a stream of events.
type Evaluator struct {
	tpl *Template
	clk clock.Clock

	mu    sync.Mutex
	state map[string]*objState
}

type objState struct {
	known bool

	// Identity of the object, kept so a sweep — which has no event in
	// hand — can name it, and its row, so the rule is not looked up
	// again on every tick.
	dev   string
	slot  int
	path  string
	label string
	unit  string
	row   *Row

	sev  Severity // adopted verdict
	band string   // adopted band

	cand     Severity // candidate waiting out its hold
	candBand string
	since    time.Time

	value   string
	num     float64
	haveNum bool
	moved   time.Time // last time a counter value changed

	flaps      []time.Time
	mutedUntil time.Time
}

// New builds an evaluator. clk nil uses the system clock.
func New(t *Template, clk clock.Clock) *Evaluator {
	if clk == nil {
		clk = clock.System()
	}
	return &Evaluator{tpl: t, clk: clk, state: map[string]*objState{}}
}

// Template returns the template in force.
func (e *Evaluator) Template() *Template { return e.tpl }

func key(device string, slot int, path string) string {
	return device + "|" + strconv.Itoa(slot) + "|" + path
}

// Eval reads one sample and returns the transition it caused, or nil
// when the verdict did not change (or is still waiting out its hold,
// or the object is silenced as flapping). An object no row covers
// always returns nil: it is Info, a change like any other.
func (e *Evaluator) Eval(device string, ev consumer.Event) *Transition {
	row := e.tpl.RowFor(ev.Path)
	if row == nil {
		return nil
	}
	now := e.clk.Now()
	k := key(device, ev.Slot, ev.Path)

	e.mu.Lock()
	defer e.mu.Unlock()
	st, ok := e.state[k]
	if !ok {
		st = &objState{sev: Normal, band: "normal", moved: now}
		e.state[k] = st
	}
	st.dev, st.slot, st.path, st.row = device, ev.Slot, ev.Path, row
	if ev.Label != "" {
		st.label = ev.Label
	}
	if ev.Unit != "" {
		st.unit = ev.Unit
	}

	value := stringOf(ev.Value)
	cand, band := e.classify(row, st, ev.Value, value, now)
	prev := st.value
	st.value = value
	return e.settle(st, cand, band, prev, now)
}

// settle applies the hold and the flap discipline to a candidate
// verdict and returns the transition it caused, or nil. It is the
// half of the decision that does not need a sample, which is why a
// sweep can call it too.
func (e *Evaluator) settle(st *objState, cand Severity, band, prev string, now time.Time) *Transition {
	// A candidate that equals the adopted verdict cancels any pending
	// change: the object came back before its hold elapsed. Starting
	// normal is not news either.
	if cand == st.sev {
		st.cand, st.candBand, st.since = 0, "", time.Time{}
		st.known = true
		return nil
	}

	// A new candidate starts its hold; the same candidate continues it.
	if st.cand != cand || st.candBand != band {
		st.cand, st.candBand, st.since = cand, band, now
	}
	if hold := st.row.hold(cand < st.sev); hold > 0 && now.Sub(st.since) < hold {
		return nil
	}

	prior := st.sev
	st.sev, st.band = cand, band
	st.cand, st.candBand, st.since = 0, "", time.Time{}
	st.known = true

	// Flap discipline: count adopted transitions in a rolling minute.
	// While an object is silenced its verdict keeps tracking the
	// device; only the reporting stops.
	st.flaps = append(pruneOlder(st.flaps, now.Add(-time.Minute)), now)
	if now.Before(st.mutedUntil) {
		return nil
	}
	flapping := st.row.FlapCap > 0 && len(st.flaps) > st.row.FlapCap
	if flapping {
		quiet := 3 * st.row.hold(false)
		if quiet <= 0 {
			quiet = time.Minute
		}
		st.mutedUntil = now.Add(quiet)
	}

	return &Transition{
		Device: st.dev, Slot: st.slot, Path: st.path,
		Label: st.label, Unit: st.unit, Text: st.row.Text,
		Value: st.value, Prev: prev,
		Severity: cand, Prior: prior, Band: band,
		Flapping: flapping, At: now,
	}
}

// Sweep advances the clock without a sample and returns the
// transitions that fall due: the candidates whose hold has elapsed,
// and the counters that have stood still for too long.
//
// A poller sees a condition persist because it reads the object again
// (ADR-0030, Event.Repeat). A device that PUSHES says "down" once and
// then says nothing — the second sample a hold is waiting for never
// arrives, and a stream that stops is silence by definition. Sweeping
// costs no wire traffic: it asks the state the evaluator already
// holds what time has made true. Callers that watch a push protocol
// call it on a ticker; a caller that only polls may call it too, and
// gets the same verdicts at the same moments.
func (e *Evaluator) Sweep() []Transition {
	now := e.clk.Now()

	e.mu.Lock()
	defer e.mu.Unlock()
	keys := make([]string, 0, len(e.state))
	for k := range e.state {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make([]Transition, 0, 4)
	for _, k := range keys {
		st := e.state[k]
		cand, band := st.cand, st.candBand
		if st.since.IsZero() {
			// Nothing pending. Only a counter can change verdict with
			// no sample at all: the stall IS the absence.
			if st.row.Kind != KindCounter || !st.haveNum || now.Sub(st.moved) < st.row.stalledFor() {
				continue
			}
			cand, band = st.row.verdict(), "stalled"
		}
		if tr := e.settle(st, cand, band, st.value, now); tr != nil {
			out = append(out, *tr)
		}
	}
	return out
}

// Explain is the verdict one value would produce on a fresh object,
// with no hold and no flap discipline: the question an operator asks
// while authoring a template ("what would 81 do?"). It changes no
// state, so it can be asked about a device that is not even connected.
//
// A counter always answers normal: one sample cannot show a stall.
func (e *Evaluator) Explain(ev consumer.Event) (Severity, string, *Row) {
	row := e.tpl.RowFor(ev.Path)
	if row == nil {
		return Info, "", nil
	}
	now := e.clk.Now()
	st := &objState{sev: Normal, band: "normal", moved: now}
	sev, band := e.classify(row, st, ev.Value, stringOf(ev.Value), now)
	return sev, band, row
}

// classify is the verdict for one sample, before hold and flap
// discipline. It returns the severity and the name of the rule that
// produced it.
func (e *Evaluator) classify(row *Row, st *objState, v consumer.Value, s string, now time.Time) (Severity, string) {
	switch row.Kind {
	case KindCounter:
		n, ok := numberOf(v, s)
		if !ok {
			return Error, "unreadable"
		}
		if !st.haveNum || n != st.num {
			st.num, st.haveNum, st.moved = n, true, now
			return Normal, "normal"
		}
		if now.Sub(st.moved) >= row.stalledFor() {
			return row.verdict(), "stalled"
		}
		return Normal, "normal"

	case KindEnum:
		if name, listed := row.Values[s]; listed {
			sev, err := ParseSeverity(name)
			if err != nil {
				return Error, "unreadable"
			}
			if sev == Normal {
				return Normal, "normal"
			}
			return sev, "value"
		}
		if row.Normal != "" && s != row.Normal {
			return row.verdict(), "value"
		}
		return Normal, "normal"

	case KindText:
		if row.re != nil {
			if row.re.MatchString(s) {
				return Normal, "normal"
			}
			return row.verdict(), "drift"
		}
		if s == row.Normal {
			return Normal, "normal"
		}
		return row.verdict(), "drift"
	}

	// number
	n, ok := numberOf(v, s)
	if !ok {
		return Error, "unreadable"
	}
	st.num, st.haveNum = n, true
	raw, band := worstBand(row, n)
	if raw >= st.sev {
		return raw, band
	}
	// Improving: only leave the adopted band once the value has passed
	// its clear point — that is the hysteresis.
	if held, side := bandByName(row, st.band); held != nil && !cleared(*held, side, n) {
		return st.sev, st.band
	}
	return raw, band
}

// worstBand returns the worst band the value satisfies, high side and
// low side together.
func worstBand(row *Row, n float64) (Severity, string) {
	worst, name := Normal, "normal"
	for _, b := range row.High {
		if n >= b.Raise {
			if s, err := ParseSeverity(b.Severity); err == nil && s > worst {
				worst, name = s, "high "+b.Severity
			}
		}
	}
	for _, b := range row.Low {
		if n <= b.Raise {
			if s, err := ParseSeverity(b.Severity); err == nil && s > worst {
				worst, name = s, "low "+b.Severity
			}
		}
	}
	return worst, name
}

// bandByName finds the band a band-name ("high major") refers to.
func bandByName(row *Row, name string) (*Band, string) {
	side, sev, found := strings.Cut(name, " ")
	if !found {
		return nil, ""
	}
	bands := row.High
	if side == "low" {
		bands = row.Low
	}
	for i := range bands {
		if bands[i].Severity == sev {
			return &bands[i], side
		}
	}
	return nil, ""
}

// cleared reports whether the value has passed a band's clear point.
// A band with no clear point is left as soon as its raise point is no
// longer satisfied.
func cleared(b Band, side string, n float64) bool {
	c := b.Raise
	if b.Clear != nil {
		c = *b.Clear
	}
	if side == "low" {
		return n > c
	}
	return n < c
}

// pruneOlder drops timestamps before cutoff, in place.
func pruneOlder(ts []time.Time, cutoff time.Time) []time.Time {
	out := ts[:0]
	for _, t := range ts {
		if t.After(cutoff) {
			out = append(out, t)
		}
	}
	return out
}

// numberOf reads a numeric value. A device that spells its numbers as
// strings (the FusioN6 serves "20000") is read as a number, because
// the operator's threshold is about the quantity, not the spelling.
func numberOf(v consumer.Value, s string) (float64, bool) {
	switch v.Kind {
	case consumer.KindInt:
		return float64(v.Int), true
	case consumer.KindUint:
		return float64(v.Uint), true
	case consumer.KindFloat:
		return v.Float, true
	case consumer.KindBool:
		if v.Bool {
			return 1, true
		}
		return 0, true
	}
	n, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return n, err == nil
}

// stringOf renders a value for comparison and for the operator.
func stringOf(v consumer.Value) string {
	switch v.Kind {
	case consumer.KindInt:
		return strconv.FormatInt(v.Int, 10)
	case consumer.KindUint:
		return strconv.FormatUint(v.Uint, 10)
	case consumer.KindFloat:
		return strconv.FormatFloat(v.Float, 'g', -1, 64)
	case consumer.KindBool:
		return strconv.FormatBool(v.Bool)
	case consumer.KindIPAddr:
		return fmt.Sprintf("%d.%d.%d.%d", v.IPAddr[0], v.IPAddr[1], v.IPAddr[2], v.IPAddr[3])
	}
	return v.Str
}

// Active lists every object currently above normal, worst first then
// by path, so a status view is deterministic.
func (e *Evaluator) Active() []Transition {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]Transition, 0, len(e.state))
	for _, st := range e.state {
		if !st.sev.IsAlarm() && st.sev != Error {
			continue
		}
		out = append(out, Transition{
			Device: st.dev, Slot: st.slot, Path: st.path,
			Label: st.label, Unit: st.unit, Text: st.row.Text,
			Value: st.value, Severity: st.sev, Band: st.band,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Severity != out[j].Severity {
			return out[i].Severity > out[j].Severity
		}
		if out[i].Device != out[j].Device {
			return out[i].Device < out[j].Device
		}
		return out[i].Path < out[j].Path
	})
	return out
}

// Counts is how many objects sit at each severity name, over every
// object the evaluator has seen and a row covers. Severities with no
// object are present with a zero, so a scrape shows "nothing is
// critical" rather than showing nothing.
func (e *Evaluator) Counts() map[string]int {
	out := map[string]int{}
	for s := Info; s <= Error; s++ {
		out[s.String()] = 0
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, st := range e.state {
		out[st.sev.String()]++
	}
	return out
}

// Severity returns the verdict in force for one object.
func (e *Evaluator) Severity(device string, slot int, path string) Severity {
	e.mu.Lock()
	defer e.mu.Unlock()
	if st, ok := e.state[key(device, slot, path)]; ok {
		return st.sev
	}
	return Info
}
