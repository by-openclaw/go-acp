package probelsw02p

import (
	"fmt"
	"strconv"
	"sync"

	"acp/internal/export/canonical"
)

// matrixKey identifies one (matrix, level) pair within an SW-P-02
// tree. Matrix id + level id are both uint8 on the wire.
type matrixKey struct {
	matrix uint8
	level  uint8
}

// matrixState holds the crosspoint snapshot for one (matrix, level):
// the current source for each destination plus the declared counts.
// SW-P-02 is destination-driven — one source per destination per
// level.
//
// Protect and salvo state that exist on SW-P-08 are omitted from this
// scaffold — those fields will be re-added alongside the per-command
// commits that use them, so the structure stays minimal until there is
// something to store.
type matrixState struct {
	targetCount int
	sourceCount int

	// sources maps destination (0-based) to source (0-based) for every
	// currently-routed destination. Sparse: absence of a key means
	// "unconnected / unknown". Sparse storage is mandatory per scale
	// targets in root CLAUDE.md (65535×65535 per matrix).
	sources map[uint16]uint16

	// targetLabels / sourceLabels hold the human-readable names from
	// the canonical tree's TargetLabels / SourceLabels maps. Empty
	// slice = names not declared in the tree.
	targetLabels []string
	sourceLabels []string
}

// tree is the in-memory state indexed by (matrix, level). Built from a
// canonical.Export at Serve start. Mutated by SetValue (API path) or
// by incoming CrosspointConnect requests (wire path, added per-command)
// — both paths pass through the same applyConnect helper so any
// future announcement fan-out stays consistent.
type tree struct {
	mu       sync.RWMutex
	matrices map[matrixKey]*matrixState
}

// newTree reduces a canonical Export to the per-(matrix,level) state
// the provider needs.
func newTree(exp *canonical.Export) (*tree, error) {
	t := &tree{matrices: map[matrixKey]*matrixState{}}
	if exp == nil || exp.Root == nil {
		return t, nil
	}
	visit(exp.Root, t)
	return t, nil
}

// visit walks any canonical Element, adding Matrix elements to the
// tree.
func visit(e canonical.Element, t *tree) {
	if e == nil {
		return
	}
	if m, ok := e.(*canonical.Matrix); ok {
		addMatrix(m, t)
	}
	for _, c := range e.Common().Children {
		visit(c, t)
	}
}

// addMatrix registers one canonical Matrix in the tree.
func addMatrix(m *canonical.Matrix, t *tree) {
	matrixID := uint8(m.Number - 1)
	if m.Number <= 0 {
		matrixID = 0
	}

	levels := m.Labels
	if len(levels) == 0 {
		st := buildState(m, "")
		t.matrices[matrixKey{matrix: matrixID, level: 0}] = st
		return
	}
	for i, lvl := range levels {
		st := buildState(m, labelKey(lvl))
		t.matrices[matrixKey{matrix: matrixID, level: uint8(i)}] = st
	}
}

// labelKey returns the key the canonical Matrix's TargetLabels /
// SourceLabels maps are indexed by.
func labelKey(lvl canonical.MatrixLabel) string {
	if lvl.Description != nil && *lvl.Description != "" {
		return *lvl.Description
	}
	return lvl.BasePath
}

// buildState constructs the matrixState for one (matrix, level).
func buildState(m *canonical.Matrix, key string) *matrixState {
	st := &matrixState{
		targetCount: int(m.TargetCount),
		sourceCount: int(m.SourceCount),
	}
	st.sources = map[uint16]uint16{}
	if st.targetCount > 0 {
		st.targetLabels = buildLabels(m.TargetLabels, key, st.targetCount)
	}
	if st.sourceCount > 0 {
		st.sourceLabels = buildLabels(m.SourceLabels, key, st.sourceCount)
	}
	return st
}

// buildLabels resolves the inner map for one level key into a dense
// ordinal-indexed slice.
func buildLabels(outer map[string]map[string]string, key string, n int) []string {
	out := make([]string, n)
	if outer == nil {
		return out
	}
	inner, ok := outer[key]
	if !ok {
		return out
	}
	for sk, sv := range inner {
		idx, err := strconv.Atoi(sk)
		if err != nil || idx < 0 || idx >= n {
			continue
		}
		out[idx] = sv
	}
	return out
}

// Size reports how many (matrix, level) pairs are in the tree.
func (t *tree) Size() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.matrices)
}

// applyConnect records a crosspoint connection on (matrix, level,
// dst). Returns an error if the indices are out of range.
func (t *tree) applyConnect(m, l uint8, dst uint16, src uint16) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	st, ok := t.matrices[matrixKey{matrix: m, level: l}]
	if !ok {
		return fmt.Errorf("probel-sw02p: unknown matrix=%d level=%d", m, l)
	}
	if int(dst) >= st.targetCount {
		return fmt.Errorf("probel-sw02p: dst %d >= targetCount %d on matrix=%d level=%d",
			dst, st.targetCount, m, l)
	}
	if int(src) >= st.sourceCount {
		return fmt.Errorf("probel-sw02p: src %d >= sourceCount %d on matrix=%d level=%d",
			src, st.sourceCount, m, l)
	}
	st.sources[dst] = src
	return nil
}
