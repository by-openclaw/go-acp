// Package compliance records what a provider did that its own
// specification does not describe.
//
// # Why it exists
//
// The repo-wide posture (root CLAUDE.md, "Spec-strict, no-workaround")
// is that a deviation is never silently worked around. We absorb it so
// the operator keeps their device, we carry on, and we COUNT it here.
// This package is therefore the only place a deviation is visible at
// all: a count that is wrong is a deviation nobody will ever hear
// about, and a plant full of lax devices that reads as strict.
//
// # The contract
//
// One Profile belongs to one live connection. A connector notes an
// event by label whenever it absorbs something; the operator reads the
// counts at the end, or the summary line in a log.
//
//   - Note(label) counts one occurrence. It is safe from any
//     goroutine and allocates nothing after the first sighting of a
//     label, because it is called from the decode path.
//   - Snapshot() returns a copy, so a caller reading it while the
//     session runs does not watch the numbers move.
//   - SummaryLine() renders those counts deterministically, sorted by
//     label — two runs of one session that read differently are two
//     runs nobody can diff.
//   - Classification() is the coarse verdict: strict when nothing was
//     absorbed, partial when something was.
//
// A nil *Profile answers all four. That is deliberate: the call sites
// are on the decode path, and a nil check at each of them is a nil
// check to keep honest at each of them.
//
// # Labels
//
// A label is `<protocol>_<what_happened>`, lowercase, and is declared
// as a CONSTANT in the protocol's own package next to the code that
// fires it — see internal/emberplus/consumer/compliance_events.go for
// the shape. Constants rather than literals because the label is the
// aggregation key: a typo in one call site silently splits a count in
// two, and nothing downstream can tell that from two different
// deviations.
//
//	const ShortReply = "acp1_short_reply"
//
//	if p.profile != nil {
//		p.profile.Note(ShortReply)
//	}
//
// This package holds no labels of its own. It is the counter; the
// protocols own their vocabularies.
package compliance

import (
	"sort"
	"sync"
	"sync/atomic"
)

// Recorder is the half of a Profile a connector actually depends on:
// somewhere to note that a deviation happened. Taking this rather than
// *Profile keeps a protocol package from depending on how the counting
// is done — and *Profile satisfies it, nil included.
type Recorder interface {
	// Note counts one occurrence of a labelled deviation.
	Note(event string)
}

// Profile aggregates tolerance events for a single live connection.
// Thread-safe. The zero value is ready to use, and so is a nil pointer.
type Profile struct {
	mu       sync.RWMutex
	counters map[string]*int64
}

// Note increments the counter for the given event label. Safe to call
// from any goroutine. Unknown labels are accepted — callers declare
// constants in their protocol package so aggregation keys stay stable
// across runs.
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

// appendInt renders v into out. Written here rather than reached for
// through fmt because SummaryLine runs per log line.
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

// interface check: the concrete type is the reference implementation of
// the contract above.
var _ Recorder = (*Profile)(nil)
