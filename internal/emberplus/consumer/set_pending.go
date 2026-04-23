package emberplus

import (
	"fmt"
	"math"
	"sync"
	"time"

	"acp/internal/protocol"
)

// pendingSet captures one in-flight SetValue call waiting for the
// provider's confirming announce. Resolved by signalPendingSet when
// the matching parameter is seen in processParameter.
type pendingSet struct {
	expected any
	kind     protocol.ValueKind
	done     chan pendingResult
}

type pendingResult struct {
	value protocol.Value
	err   error
}

// pendingSets is a package-level helper container on the Plugin.
// Keyed by the parameter's numeric OID string ("1.1.2.5"). Guarded
// by its own mutex, not p.mu.
type pendingSetRegistry struct {
	mu   sync.Mutex
	byID map[string]*pendingSet
}

func newPendingSetRegistry() *pendingSetRegistry {
	return &pendingSetRegistry{byID: make(map[string]*pendingSet)}
}

func (r *pendingSetRegistry) register(key string, ps *pendingSet) {
	r.mu.Lock()
	r.byID[key] = ps
	r.mu.Unlock()
}

func (r *pendingSetRegistry) unregister(key string) {
	r.mu.Lock()
	delete(r.byID, key)
	r.mu.Unlock()
}

func (r *pendingSetRegistry) take(key string) *pendingSet {
	r.mu.Lock()
	ps := r.byID[key]
	if ps != nil {
		delete(r.byID, key)
	}
	r.mu.Unlock()
	return ps
}

// valuesMatch tests whether the confirming announce's value equals
// what the caller asked SetValue to set. Floating-point gets a
// tolerance because providers round. Bytes + strings are exact.
func valuesMatch(expected, actual any, kind protocol.ValueKind) bool {
	if expected == nil && actual == nil {
		return true
	}
	if expected == nil || actual == nil {
		return false
	}

	if kind == protocol.KindFloat {
		ef, eok := expected.(float64)
		af, aok := actual.(float64)
		if eok && aok {
			return math.Abs(ef-af) < 1e-6
		}
	}

	return fmt.Sprintf("%v", expected) == fmt.Sprintf("%v", actual)
}

// signalPendingSet is invoked from processParameter right after the
// entry is stored. It completes any pending SetValue waiting on the
// same OID, classifying the outcome as success / coerced / rejected
// by comparing the announced value to what was requested.
//
// Rejection heuristic: if the announced value matches the PRIOR
// stored value (no change applied) AND the element now reports
// isOnline=false OR access has no write bit, treat it as an explicit
// rejection. Otherwise value-mismatch counts as coerced.
func (p *Plugin) signalPendingSet(entry *treeEntry) {
	if entry == nil || entry.glowParam == nil {
		return
	}
	key := numericKey(entry.numericPath)
	ps := p.pendingSets.take(key)
	if ps == nil {
		return
	}

	actual := entry.glowParam.Value
	match := valuesMatch(ps.expected, actual, ps.kind)

	res := pendingResult{value: entry.obj.Value}
	switch {
	case match:
		// Success — provider echoed exactly what we asked for.
	default:
		res.err = fmt.Errorf("%w: expected=%v actual=%v",
			protocol.ErrWriteCoerced, ps.expected, actual)
	}

	select {
	case ps.done <- res:
	default:
		// Caller already timed out; drop.
	}
}

// defaultWriteTimeout is the window within which we expect the
// provider to echo a Parameter announce confirming the set.
// Configurable later via plugin options.
const defaultWriteTimeout = 3 * time.Second
