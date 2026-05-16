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
	"sort"
	"sync"
	"sync/atomic"
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
type Profile struct {
	mu       sync.RWMutex
	counters map[string]*int64
}

// Note increments the counter for the given event label. Safe to call
// from any goroutine. Unknown labels are accepted — callers should
// define constants in their protocol package so aggregation keys stay
// stable across runs.
func (p *Profile) Note(event string) {
	if p == nil {
		return
	}
	p.mu.RLock()
	ptr, ok := p.counters[event]
	p.mu.RUnlock()
	if !ok {
		p.mu.Lock()
		if p.counters == nil {
			p.counters = make(map[string]*int64, 8)
		}
		if ptr, ok = p.counters[event]; !ok {
			var zero int64
			ptr = &zero
			p.counters[event] = ptr
		}
		p.mu.Unlock()
	}
	atomic.AddInt64(ptr, 1)
}

// Snapshot returns the current counters as a plain map. Safe to read
// from the caller's goroutine; subsequent Note calls do not mutate it.
func (p *Profile) Snapshot() map[string]int64 {
	if p == nil {
		return nil
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make(map[string]int64, len(p.counters))
	for k, v := range p.counters {
		out[k] = atomic.LoadInt64(v)
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
