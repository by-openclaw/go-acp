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

	"acp/internal/protocol/emberplus/glow"
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
//	oneToOne: 1 source per target AND source used once globally.
//	nToN:     len(sources[t]) <= MaxConnectsPerTarget;
//	          sum(all targets) <= MaxTotalConnects.
//	any type: reject if target disposition is locked (disposition 3).
func (s *State) CanConnect(target int32, newSources []int32, op int64) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if cur, ok := s.Targets[target]; ok && cur.Disposition == glow.ConnDispLocked {
		return fmt.Errorf("target %d is locked (spec p.89 ConnectionDisposition)", target)
	}

	projected := projectSources(s.Targets[target], newSources, op)

	switch s.Type {
	case glow.MatrixTypeOneToN:
		if len(projected) > 1 {
			return fmt.Errorf("oneToN matrix: target %d would have %d sources (max 1) [spec p.33]",
				target, len(projected))
		}
	case glow.MatrixTypeOneToOne:
		if len(projected) > 1 {
			return fmt.Errorf("oneToOne matrix: target %d would have %d sources (max 1) [spec p.33]",
				target, len(projected))
		}
		// Source exclusivity: no other target may already use any source.
		for _, src := range projected {
			for tgtNum, tstate := range s.Targets {
				if tgtNum == target {
					continue
				}
				for _, cur := range tstate.Sources {
					if cur == src {
						return fmt.Errorf("oneToOne matrix: source %d already used by target %d [spec p.33]",
							src, tgtNum)
					}
				}
			}
		}
	case glow.MatrixTypeNToN:
		if s.MaxConnectsPerTarget > 0 && int32(len(projected)) > s.MaxConnectsPerTarget {
			return fmt.Errorf("nToN matrix: target %d would have %d sources (max %d per target) [spec p.33]",
				target, len(projected), s.MaxConnectsPerTarget)
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
				return fmt.Errorf("nToN matrix: total connects would be %d (max %d) [spec p.33]",
					total, s.MaxTotalConnects)
			}
		}
	}
	return nil
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
