// Package matrix holds the derived, RAM-only state that the consumer
// maintains for every Glow Matrix it walks. The wire layer (glow.Matrix,
// glow.Connection) stays spec-pure; everything UI-visible — resolved
// labels, resolved gain paths, lastChanged timestamps, change attribution —
// lives here.
//
// Spec reference: Ember+ Documentation v2.50 pp. 33–46 (Matrix Extensions)
// and pp. 88–89 (Glow DTD Matrix / Connection).
package matrix

import (
	"fmt"
	"sync"
	"time"

	"dhs/internal/emberplus/codec/glow"
)

// ChangeSource attributes the most recent mutation to its origin.
// Used by the UI to differentiate a local action from a provider
// announcement ("who last touched this crosspoint?").
type ChangeSource uint8

const (
	// ChangeWalk means the state entry was populated during the initial
	// GetDirectory walk.
	ChangeWalk ChangeSource = iota
	// ChangeAnnounce means the provider pushed an announcement.
	ChangeAnnounce
	// ChangeUser means a local SetConnection issued the change.
	ChangeUser
)

// TargetState is the per-target derived record. One entry per matrix
// target index; sources[] mirrors the wire Connection body, the rest is
// UI-only.
type TargetState struct {
	Target         int32
	Sources        []int32
	Operation      int64 // glow.ConnOp*
	Disposition    int64 // glow.ConnDisp*
	LabelTarget    string // resolved from MatrixContents.labels (basePath lookup)
	LabelSources   []string // resolved per source
	ResolvedGainDb map[int32]float64 // source -> gain (dB) if parametersLocation points at a gain param

	LastChanged time.Time
	ChangedBy   ChangeSource
}

// State carries every target's derived record plus static info needed for
// canConnect validation. One State per decoded Matrix.
type State struct {
	mu sync.RWMutex

	// Static (populated once on walk / update on contents change).
	Type                 int64
	AddressingMode       int64
	TargetCount          int32
	SourceCount          int32
	MaxTotalConnects     int32
	MaxConnectsPerTarget int32

	// Per-target derived state.
	Targets map[int32]*TargetState
}

// NewStateFromGlow builds a fresh State from a decoded Glow Matrix. Call
// once per walk; replace wholesale when MatrixContents changes.
//
// Fields mirrored from the decoded glow.Matrix Context-tagged SET:
//
//	| Context Tag | Field                    | Type         | Notes         |
//	|-------------|--------------------------|--------------|---------------|
//	|   [2]       | type                     | MatrixType   | 1-to-N / N-N  |
//	|   [3]       | addressingMode           | AddressMode  | linear default|
//	|   [4]       | targetCount              | INTEGER      |               |
//	|   [5]       | sourceCount              | INTEGER      |               |
//	|   [6]       | maximumTotalConnects     | INTEGER      | nToN cap      |
//	|   [7]       | maximumConnectsPerTarget | INTEGER      | nToN cap      |
//
// Spec reference: Ember+ Documentation.pdf §MatrixContents pp. 88-89.
func NewStateFromGlow(m *glow.Matrix) *State {
	s := &State{
		Type:                 m.MatrixType,
		AddressingMode:       m.AddressingMode,
		TargetCount:          m.TargetCount,
		SourceCount:          m.SourceCount,
		MaxTotalConnects:     m.MaxTotalConnects,
		MaxConnectsPerTarget: m.MaxConnectsPerTarget,
		Targets:              make(map[int32]*TargetState, len(m.Connections)),
	}
	for _, c := range m.Connections {
		s.Targets[c.Target] = &TargetState{
			Target:      c.Target,
			Sources:     append([]int32(nil), c.Sources...),
			Operation:   c.Operation,
			Disposition: c.Disposition,
			LastChanged: time.Now(),
			ChangedBy:   ChangeWalk,
		}
	}
	return s
}

// ApplyConnection merges an incoming announcement into the state. source
// == ChangeAnnounce updates the tally; anything else records the request
// as sent. Respects ConnectionOperation semantics (spec p.89).
//
// Fields consumed from the decoded glow.Connection (APPLICATION[16]):
//
//	| Context Tag | Field       | Type         | Semantics                 |
//	|-------------|-------------|--------------|---------------------------|
//	|   [0]       | target      | INTEGER      | target number             |
//	|   [1]       | sources     | RELATIVE-OID | source-number list        |
//	|   [2]       | operation   | INTEGER      | 0=absolute/1=conn/2=disc. |
//	|   [3]       | disposition | INTEGER      | 0=tally/1=mod/2=pend/3=lck|
//
// Spec reference: Ember+ Documentation.pdf §Connection p. 89.
func (s *State) ApplyConnection(c glow.Connection, source ChangeSource) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tgt, ok := s.Targets[c.Target]
	if !ok {
		tgt = &TargetState{Target: c.Target}
		s.Targets[c.Target] = tgt
	}
	switch c.Operation {
	case glow.ConnOpConnect:
		tgt.Sources = union(tgt.Sources, c.Sources)
	case glow.ConnOpDisconnect:
		tgt.Sources = difference(tgt.Sources, c.Sources)
	default: // absolute
		tgt.Sources = append([]int32(nil), c.Sources...)
	}
	tgt.Operation = c.Operation
	tgt.Disposition = c.Disposition
	tgt.LastChanged = time.Now()
	tgt.ChangedBy = source
}

// Snapshot returns a deep copy of all target records, safe to pass up to
// the UI without holding s.mu.
func (s *State) Snapshot() []TargetState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]TargetState, 0, len(s.Targets))
	for _, t := range s.Targets {
		cp := *t
		cp.Sources = append([]int32(nil), t.Sources...)
		if len(t.LabelSources) > 0 {
			cp.LabelSources = append([]string(nil), t.LabelSources...)
		}
		if len(t.ResolvedGainDb) > 0 {
			cp.ResolvedGainDb = make(map[int32]float64, len(t.ResolvedGainDb))
			for k, v := range t.ResolvedGainDb {
				cp.ResolvedGainDb[k] = v
			}
		}
		out = append(out, cp)
	}
	return out
}

// CanConnect validates an intended operation before we put it on the
// wire. Returns a descriptive error with spec reference on failure.
//
// Rules (spec p.33–34, p.89):
//
//	oneToN:   exactly 1 source per target; connect replaces.
//	oneToOne: 1 source per target. The "source used once globally"
//	          spec clause (p.33) is NOT enforced here — every shipping
//	          provider (Lawo VSM, EmberPlusView-as-provider, TinyEmber+)
//	          implements source-steal: setting src=A on tgt=B implicitly
//	          disconnects A from its prior target. Same precedent as
//	          feedback_probel_salvo_connected — when every shipping
//	          controller contradicts a literal spec clause, the connector
//	          aligns with the controllers. The provider's
//	          applyMatrixConnections handles the actual steal on receive.
//	          The consumer-side plugin.MatrixConnect fires the
//	          EmberPlusOneToOneSourceStealAccepted compliance event when
//	          it detects an implicit steal pre-flight, so operators see
//	          the deviation in the profile counter. Refs #465.
//	nToN:     len(sources[t]) <= MaxConnectsPerTarget;
//	          sum(all targets) <= MaxTotalConnects.
//	any type: reject if target disposition is locked (disposition 3).
func (s *State) CanConnect(target int32, newSources []int32, op int64) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if cur, ok := s.Targets[target]; ok && cur.Disposition == glow.ConnDispLocked {
		return fmt.Errorf("%w: target %d is locked (spec p.89 ConnectionDisposition)", ErrTargetLocked, target)
	}

	// Spec p.89: ConnectionOperation.Disconnect removes the listed
	// sources from the connection. The resulting empty sources[] is
	// a legal "target unrouted" state for every matrix type per spec
	// — including oneToN and oneToOne. Earlier code rejected Disconnect
	// pre-flight on these types citing "spec p.33", but that was the
	// same misreading as PR #98's provider-side coercion. Letting
	// Disconnect through here matches what every shipping Ember+ client
	// (Cerebrum, EmberPlusView, VSM) expects to be able to send.

	projected := projectSources(s.Targets[target], newSources, op)

	switch s.Type {
	case glow.MatrixTypeOneToN:
		if len(projected) > 1 {
			return fmt.Errorf("%w: oneToN matrix: target %d would have %d sources (max 1) [spec p.33]",
				ErrCardinalityExceeded, target, len(projected))
		}
	case glow.MatrixTypeOneToOne:
		if len(projected) > 1 {
			return fmt.Errorf("%w: oneToOne matrix: target %d would have %d sources (max 1) [spec p.33]",
				ErrCardinalityExceeded, target, len(projected))
		}
		// Source exclusivity (spec p.33 literal) is NOT enforced here —
		// see CanConnect doc-comment for the source-steal precedent.
		// The consumer's plugin.MatrixConnect detects the implicit
		// steal pre-flight and fires a compliance event. Refs #465.
	case glow.MatrixTypeNToN:
		if s.MaxConnectsPerTarget > 0 && int32(len(projected)) > s.MaxConnectsPerTarget {
			return fmt.Errorf("%w: nToN matrix: target %d would have %d sources (max %d per target) [spec p.33]",
				ErrMaxConnectsPerTarget, target, len(projected), s.MaxConnectsPerTarget)
		}
		if s.MaxTotalConnects > 0 {
			total := int32(len(projected))
			for tgtNum, tstate := range s.Targets {
				if tgtNum == target {
					continue
				}
				total += int32(len(tstate.Sources))
			}
			if total > s.MaxTotalConnects {
				return fmt.Errorf("%w: nToN matrix: total connects would be %d (max %d) [spec p.33]",
					ErrMaxTotalConnects, total, s.MaxTotalConnects)
			}
		}
	}
	return nil
}

// DetectOneToOneSourceSteal reports whether a SET on a oneToOne matrix
// would implicitly disconnect any of the requested sources from a
// prior target (the source-steal case documented in CanConnect's
// doc-comment; refs #465). Returns the list of (target, source) pairs
// whose disconnection is implied — empty when no steal is needed.
//
// Pure read; safe to call under any goroutine. Returns nil for
// non-oneToOne matrices.
func (s *State) DetectOneToOneSourceSteal(target int32, newSources []int32) []SourceStealPair {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.Type != glow.MatrixTypeOneToOne {
		return nil
	}
	var stolen []SourceStealPair
	for _, src := range newSources {
		for tgtNum, tstate := range s.Targets {
			if tgtNum == target {
				continue
			}
			for _, cur := range tstate.Sources {
				if cur == src {
					stolen = append(stolen, SourceStealPair{FromTarget: tgtNum, Source: src})
				}
			}
		}
	}
	return stolen
}

// SourceStealPair names one (target, source) pair whose source is
// being implicitly disconnected by a oneToOne SET. Used by the
// consumer's compliance reporting so each individual deviation is
// visible (not collapsed to a single event for a multi-source SET).
type SourceStealPair struct {
	FromTarget int32
	Source     int32
}

// projectSources simulates the union/difference/replace that the provider
// will apply, given the existing state and the requested op. Pure function.
func projectSources(cur *TargetState, newSources []int32, op int64) []int32 {
	var existing []int32
	if cur != nil {
		existing = cur.Sources
	}
	switch op {
	case glow.ConnOpConnect:
		return union(existing, newSources)
	case glow.ConnOpDisconnect:
		return difference(existing, newSources)
	default:
		return append([]int32(nil), newSources...)
	}
}

func union(a, b []int32) []int32 {
	seen := make(map[int32]struct{}, len(a)+len(b))
	out := make([]int32, 0, len(a)+len(b))
	for _, v := range a {
		if _, ok := seen[v]; !ok {
			seen[v] = struct{}{}
			out = append(out, v)
		}
	}
	for _, v := range b {
		if _, ok := seen[v]; !ok {
			seen[v] = struct{}{}
			out = append(out, v)
		}
	}
	return out
}

func difference(a, b []int32) []int32 {
	drop := make(map[int32]struct{}, len(b))
	for _, v := range b {
		drop[v] = struct{}{}
	}
	out := make([]int32, 0, len(a))
	for _, v := range a {
		if _, ok := drop[v]; !ok {
			out = append(out, v)
		}
	}
	return out
}
