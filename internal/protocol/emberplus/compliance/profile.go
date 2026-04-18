// Package compliance tracks per-session deviations from the strict
// Ember+ specification. Each tolerance measure in our decoder/plugin
// (see docs/protocols/emberplus.md §A9) bumps a named counter when it
// fires. The resulting profile lets the user build a compatibility
// matrix per provider: which ones are strict, which are lax, which
// emit unexpected shapes.
//
// Zero allocations on the hot path — counters are atomic int64.
package compliance

import (
	"sort"
	"sync"
	"sync/atomic"
)

// Event labels. Keep this list short and stable — documented in
// docs/protocols/emberplus.md §A9. Adding a new event label is an
// API change: downstream tooling may aggregate by key.
const (
	// NonQualifiedElement fires when a provider delivers a Node /
	// Parameter / Matrix / Function without a RELATIVE-OID path,
	// forcing the consumer to derive it from the parent walk
	// ancestry. Spec p.85/p.87 permit both forms; real-world
	// Qualified* wrappers are the modern default.
	NonQualifiedElement = "non_qualified_element"

	// MultiFrameReassembly fires when S101 FlagFirst/FlagLast is
	// observed and the payload had to be reassembled across frames.
	// Any walk of more than a few hundred objects triggers this.
	MultiFrameReassembly = "multi_frame_reassembly"

	// InvocationSuccessDefault fires when InvocationResult arrives
	// without the success field (spec p.92: "True or omitted if no
	// errors"). Common on Lawo-derived providers.
	InvocationSuccessDefault = "invocation_success_default"

	// ConnectionOperationDefault fires when a Connection omits the
	// operation field and the decoder falls back to absolute
	// (spec p.89 default).
	ConnectionOperationDefault = "connection_operation_default"

	// ConnectionDispositionDefault fires when a Connection omits
	// the disposition field; decoder falls back to tally (p.89).
	ConnectionDispositionDefault = "connection_disposition_default"

	// ContentsSetOmitted fires when contents are delivered as a
	// bare CTX[1] sequence without the UNIVERSAL SET envelope.
	// Spec p.85 mandates the SET; some providers skip it.
	ContentsSetOmitted = "contents_set_omitted"

	// TupleDirectCtx fires when a Tuple arrives as direct CTX[0]
	// elements with no enclosing UNIVERSAL SEQUENCE. Spec p.92
	// defines the SEQUENCE; some providers inline the elements.
	TupleDirectCtx = "tuple_direct_ctx"

	// ElementCollectionBare fires when an ElementCollection is
	// inlined as CTX[0] children without the APP[4] wrapper.
	// Common in request frames from consumers that need it back.
	ElementCollectionBare = "element_collection_bare"

	// UnknownTagSkipped fires when the decoder encounters an
	// APP or CTX tag it does not recognise. Usually a vendor-
	// private extension; harmless.
	UnknownTagSkipped = "unknown_tag_skipped"
)

// Profile aggregates tolerance events for a single live connection.
// Thread-safe. Zero value is ready to use via Note / Snapshot.
type Profile struct {
	mu       sync.RWMutex
	counters map[string]*int64
}

// Note increments the counter for the given event label. Safe to call
// from any goroutine. Unknown labels are accepted — callers should
// stick to the constants above for aggregation across runs.
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
