// Package compliance tracks per-session deviations from a strict
// protocol specification. Each plugin (ACP1, ACP2, Ember+, future
// Probel / TSL / NMOS) defines its own named event constants close
// to the code that fires them — this package provides only the
// generic counter machinery.
//
// Rationale — see memory/feedback_no_workaround.md §7–8: when a
// provider deviates from spec we NEVER silently work around it. We
// absorb the deviation, fire a named event, and surface the profile
// so the operator can audit which providers are strict vs lax.
//
// Zero allocations on the hot path — counters are atomic int64.
package compliance

import (
	"log/slog"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Profile aggregates tolerance events for a single live connection.
// Thread-safe. Zero value is ready to use via Note / Snapshot.
//
// Usage (plugin side):
//
//	const ShortReply = "acp1_short_reply"
//
//	if p.profile != nil {
//		p.profile.Note(ShortReply)
//	}
//
// R22 #487 extends Profile with per-event first/last timestamps and
// (optionally) a bounded ring of observation events with per-call
// attrs so `dhs consumer emberplus profile --show-events` can render
// per-occurrence detail (matrix path, target, source). Use Observe
// instead of Note when the firing site has structured context the
// operator may want to grep for; Note stays the lock-light hot-path
// API for counter-only fires.
type Profile struct {
	mu       sync.RWMutex
	counters map[string]*counter

	// events is a bounded ring of recent observations from Observe.
	// Capacity is fixed at construction time (ringSize). Set via
	// NewProfileWithRing; the zero-value Profile keeps events at nil
	// so Note-only callers pay zero cost for the ring.
	eventsMu sync.Mutex
	events   []ObservedEvent
	ringHead int
	ringFull bool
}

// counter is the per-event-kind hot-path state. count uses atomic
// add; firstNano / lastNano are written via atomic.Store /
// CompareAndSwap so Note() can stay lock-light after the map slot
// exists.
type counter struct {
	count     atomic.Int64
	firstNano atomic.Int64 // 0 until the first observation
	lastNano  atomic.Int64
}

// ObservedEvent is one observation captured by Observe(). Attrs
// carry the per-call structured context (slog.Attr list); kind
// matches the package-level event constant; at records observation
// time in UnixNano.
type ObservedEvent struct {
	Kind  string
	At    time.Time
	Attrs []slog.Attr
}

// EventCount carries the aggregated state per event kind for
// machine-readable rendering (R22 --format json) and the text
// summary line.
type EventCount struct {
	Kind      string    `json:"kind"`
	Count     int64     `json:"count"`
	FirstSeen time.Time `json:"first_seen,omitempty"`
	LastSeen  time.Time `json:"last_seen,omitempty"`
}

// NewProfileWithRing returns a Profile with a bounded ring of
// observation events sized to capacity. A zero-value Profile (the
// historical form) keeps the ring at nil — Observe() then falls back
// to counter-only update like Note().
func NewProfileWithRing(capacity int) *Profile {
	if capacity <= 0 {
		return &Profile{}
	}
	return &Profile{events: make([]ObservedEvent, capacity)}
}

// Note increments the counter for the given event label. Safe to call
// from any goroutine. Unknown labels are accepted — callers should
// define constants in their protocol package so aggregation keys stay
// stable across runs.
func (p *Profile) Note(event string) {
	if p == nil {
		return
	}
	c := p.ensureCounter(event)
	now := time.Now().UnixNano()
	c.lastNano.Store(now)
	c.firstNano.CompareAndSwap(0, now)
	c.count.Add(1)
}

// Observe is the structured variant of Note: bumps the same counter
// and (when the ring is configured) records the per-call attrs so
// R22 #487 --show-events can render per-occurrence detail. attrs may
// be empty — Observe with no attrs is equivalent to Note plus a ring
// entry marking the observation timestamp.
func (p *Profile) Observe(event string, attrs ...slog.Attr) {
	if p == nil {
		return
	}
	c := p.ensureCounter(event)
	now := time.Now()
	nano := now.UnixNano()
	c.lastNano.Store(nano)
	c.firstNano.CompareAndSwap(0, nano)
	c.count.Add(1)

	// Ring push only when a ring exists; zero-value Profile skips.
	if p.events == nil {
		return
	}
	rec := ObservedEvent{Kind: event, At: now}
	if len(attrs) > 0 {
		rec.Attrs = append([]slog.Attr(nil), attrs...)
	}
	p.eventsMu.Lock()
	p.events[p.ringHead] = rec
	p.ringHead++
	if p.ringHead == len(p.events) {
		p.ringHead = 0
		p.ringFull = true
	}
	p.eventsMu.Unlock()
}

// ensureCounter returns the per-event counter, creating it lazily
// under the write lock when missing.
func (p *Profile) ensureCounter(event string) *counter {
	p.mu.RLock()
	c, ok := p.counters[event]
	p.mu.RUnlock()
	if ok {
		return c
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.counters == nil {
		p.counters = make(map[string]*counter, 8)
	}
	if c, ok = p.counters[event]; ok {
		return c
	}
	c = &counter{}
	p.counters[event] = c
	return c
}

// Snapshot returns the current counters as a plain map (kind -> count).
// Safe to read from the caller's goroutine; subsequent Note / Observe
// calls do not mutate it. Kept for back-compat with R22 pre-rollout
// callers — new code should prefer SnapshotEvents().
func (p *Profile) Snapshot() map[string]int64 {
	if p == nil {
		return nil
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make(map[string]int64, len(p.counters))
	for k, c := range p.counters {
		out[k] = c.count.Load()
	}
	return out
}

// SnapshotEvents returns the per-kind aggregated state with first /
// last timestamps for R22 #487 --format json. The slice is sorted by
// kind for deterministic CLI output. Empty when no events fired.
func (p *Profile) SnapshotEvents() []EventCount {
	if p == nil {
		return nil
	}
	p.mu.RLock()
	out := make([]EventCount, 0, len(p.counters))
	for k, c := range p.counters {
		ec := EventCount{Kind: k, Count: c.count.Load()}
		if first := c.firstNano.Load(); first > 0 {
			ec.FirstSeen = time.Unix(0, first).UTC()
		}
		if last := c.lastNano.Load(); last > 0 {
			ec.LastSeen = time.Unix(0, last).UTC()
		}
		out = append(out, ec)
	}
	p.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Kind < out[j].Kind })
	return out
}

// SnapshotEventsSince filters SnapshotEvents to entries whose
// LastSeen is within `window` of now. Counter values are returned
// unchanged — R22 #487 spec keeps the cumulative count and lets the
// operator interpret. Pass 0 to disable filtering (same as
// SnapshotEvents).
func (p *Profile) SnapshotEventsSince(window time.Duration) []EventCount {
	all := p.SnapshotEvents()
	if window <= 0 {
		return all
	}
	cutoff := time.Now().Add(-window)
	out := make([]EventCount, 0, len(all))
	for _, ec := range all {
		if ec.LastSeen.After(cutoff) {
			out = append(out, ec)
		}
	}
	return out
}

// Observations returns the recorded ObservedEvent ring in chronological
// order (oldest first). Empty when no ring was configured at
// construction or when nothing has been Observed yet. `since` filters
// to events newer than `now - since`; pass 0 to return everything.
func (p *Profile) Observations(since time.Duration) []ObservedEvent {
	if p == nil || p.events == nil {
		return nil
	}
	p.eventsMu.Lock()
	defer p.eventsMu.Unlock()
	if len(p.events) == 0 {
		return nil
	}
	// Materialise the ring in chronological order.
	var ordered []ObservedEvent
	if p.ringFull {
		ordered = make([]ObservedEvent, 0, len(p.events))
		ordered = append(ordered, p.events[p.ringHead:]...)
		ordered = append(ordered, p.events[:p.ringHead]...)
	} else {
		ordered = make([]ObservedEvent, 0, p.ringHead)
		ordered = append(ordered, p.events[:p.ringHead]...)
	}
	if since <= 0 {
		return ordered
	}
	cutoff := time.Now().Add(-since)
	out := make([]ObservedEvent, 0, len(ordered))
	for _, ev := range ordered {
		if ev.At.After(cutoff) {
			out = append(out, ev)
		}
	}
	return out
}

// SummaryLine produces a single-line, deterministically-sorted render
// of the profile suitable for a structured log value. Empty profile
// returns the empty string.
func (p *Profile) SummaryLine() string {
	snap := p.Snapshot()
	if len(snap) == 0 {
		return ""
	}
	keys := make([]string, 0, len(snap))
	for k := range snap {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []byte
	for i, k := range keys {
		if i > 0 {
			out = append(out, ' ')
		}
		out = append(out, k...)
		out = append(out, '=')
		out = appendInt(out, snap[k])
	}
	return string(out)
}

// Classification returns a coarse verdict based on which events fired:
//
//	strict   — zero tolerance events
//	partial  — at least one event fired (provider deviates from spec
//	           but does so within our tolerance envelope)
func (p *Profile) Classification() string {
	snap := p.Snapshot()
	for _, v := range snap {
		if v > 0 {
			return "partial"
		}
	}
	return "strict"
}

func appendInt(out []byte, v int64) []byte {
	if v == 0 {
		return append(out, '0')
	}
	var buf [20]byte
	n := len(buf)
	neg := v < 0
	if neg {
		v = -v
	}
	for v > 0 {
		n--
		buf[n] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		n--
		buf[n] = '-'
	}
	return append(out, buf[n:]...)
}
