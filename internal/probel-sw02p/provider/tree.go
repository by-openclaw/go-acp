package probelsw02p

import (
	"fmt"
	"strconv"
	"sync"

	"acp/internal/export/canonical"
	"acp/internal/probel-sw02p/codec"
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
// Protect state from SW-P-08 is omitted from this scaffold — those
// fields will be re-added alongside the per-command commits that use
// them. Salvo "pending" storage lands here with rx 05 / 35.
type matrixState struct {
	targetCount int
	sourceCount int

	// sources maps destination (0-based) to source (0-based) for every
	// currently-routed destination. Sparse: absence of a key means
	// "unconnected / unknown". Sparse storage is mandatory per scale
	// targets in root CLAUDE.md (65535×65535 per matrix).
	sources map[uint16]uint16

	// pending is the server-wide salvo buffer for §3.2.7 CONNECT ON GO
	// messages. Each rx 05 appends one slot; the next rx 06 GO either
	// applies every slot to the sources map (set) or drops the whole
	// list (clear). A single slot list per (matrix, level) pair mirrors
	// the SW-P-02 §3.2.7 / §3.2.8 flow and the real-world router
	// behaviour — multiple controllers feeding the same matrix share
	// the same pending buffer.
	pending []pendingSlot

	// pendingGroups holds the per-SalvoID buffers fed by §3.2.36
	// CONNECT ON GO GROUP SALVO. Populated by rx 35, drained by
	// rx 36 GO GROUP SALVO. Keyed by SalvoID (0-127); up to 128
	// groups can be staged simultaneously on the same matrix. Sparse
	// — a SalvoID with no staged slots simply does not appear in
	// the map.
	pendingGroups map[uint8][]pendingSlot

	// targetLabels / sourceLabels hold the human-readable names from
	// the canonical tree's TargetLabels / SourceLabels maps. Empty
	// slice = names not declared in the tree.
	targetLabels []string
	sourceLabels []string
}

// pendingSlot is one crosspoint staged by rx 05 / rx 69 CONNECT ON GO
// or rx 35 / rx 71 CONNECT ON GO GROUP SALVO, waiting for rx 06 /
// rx 36 GO to commit or clear. Extended marks slots staged via the
// extended-addressing variants (rx 069 / rx 071); commit emits tx 068
// Extended CONNECTED for those and tx 004 CONNECTED for narrow ones
// so the CONNECTED form on the wire always matches the addressing
// range the controller used to stage it (§3.2.51 + §3.2.50 vs.
// §3.2.7 + §3.2.6).
type pendingSlot struct {
	Destination uint16
	Source      uint16
	Extended    bool
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

// appendPending stages one crosspoint into (matrix, level)'s pending
// salvo buffer. Auto-creates the matrixState for (m, l) if the tree
// has no canonical entry — SW-P-02 is single-matrix / single-level on
// the wire, so a controller can legitimately issue CONNECT ON GO
// before any canonical tree has been loaded. The count fields stay 0
// until a tree is declared; applyPending honours those zero counts by
// skipping out-of-range slots.
func (t *tree) appendPending(m, l uint8, slot pendingSlot) {
	t.mu.Lock()
	defer t.mu.Unlock()
	key := matrixKey{matrix: m, level: l}
	st, ok := t.matrices[key]
	if !ok {
		st = &matrixState{sources: map[uint16]uint16{}}
		t.matrices[key] = st
	}
	st.pending = append(st.pending, slot)
}

// drainPending atomically empties (matrix, level)'s pending buffer and
// returns its contents. Used by handleGo on both op=set (apply each
// slot and broadcast) and op=clear (just discard). Returns nil if no
// matrixState exists for the key — a spurious GO on an unknown matrix
// is a no-op rather than an error.
func (t *tree) drainPending(m, l uint8) []pendingSlot {
	t.mu.Lock()
	defer t.mu.Unlock()
	st, ok := t.matrices[matrixKey{matrix: m, level: l}]
	if !ok {
		return nil
	}
	out := st.pending
	st.pending = nil
	return out
}

// appendPendingGroup stages one crosspoint into the SalvoID-keyed
// pending buffer on (matrix, level). Auto-creates the matrixState and
// the per-salvo slice on first use.
func (t *tree) appendPendingGroup(m, l uint8, salvoID uint8, slot pendingSlot) {
	t.mu.Lock()
	defer t.mu.Unlock()
	key := matrixKey{matrix: m, level: l}
	st, ok := t.matrices[key]
	if !ok {
		st = &matrixState{sources: map[uint16]uint16{}}
		t.matrices[key] = st
	}
	if st.pendingGroups == nil {
		st.pendingGroups = map[uint8][]pendingSlot{}
	}
	st.pendingGroups[salvoID] = append(st.pendingGroups[salvoID], slot)
}

// drainPendingGroup atomically empties (matrix, level)'s SalvoID-keyed
// pending buffer and returns its contents. Returns nil if the SalvoID
// has no staged slots — a spurious GO GROUP SALVO on an empty group
// is legal per §3.2.37 (tx 38 ack still fires with Result=Empty).
func (t *tree) drainPendingGroup(m, l uint8, salvoID uint8) []pendingSlot {
	t.mu.Lock()
	defer t.mu.Unlock()
	st, ok := t.matrices[matrixKey{matrix: m, level: l}]
	if !ok || st.pendingGroups == nil {
		return nil
	}
	out := st.pendingGroups[salvoID]
	delete(st.pendingGroups, salvoID)
	return out
}

// sourceLockSnapshot returns a per-source lock bitmap sized to the
// widest declared sourceCount across matrix 0. Per §3.2.17 note 2 a
// zero bit is ambiguous (card absent OR signal lost) — this plugin
// runs as software with no physical input cards to monitor, so every
// declared source reports "locked = true" (clean signal) by default.
// Future HW-monitor integration can swap this out via a ServerOption.
func (t *tree) sourceLockSnapshot() []bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	maxSrc := 0
	for key, st := range t.matrices {
		if key.matrix != 0 {
			continue
		}
		if st.sourceCount > maxSrc {
			maxSrc = st.sourceCount
		}
	}
	if maxSrc == 0 {
		return nil
	}
	out := make([]bool, maxSrc)
	for i := range out {
		out[i] = true
	}
	return out
}

// buildRouterConfigResponse1 derives the tx 076 RESPONSE-1 payload
// from the canonical tree. All levels registered on matrix 0 are
// emitted in ascending level order with the shared target/source
// counts drawn from their matrixState. Up to RouterConfigMaxLevels
// (28) are reported — levels beyond that are silently dropped
// because the §3.2.58 bit map cannot encode them.
func (t *tree) buildRouterConfigResponse1() codec.RouterConfigResponse1Params {
	t.mu.RLock()
	defer t.mu.RUnlock()
	var bitmap uint32
	levels := make([]codec.RouterConfigResponse1LevelEntry, 0)
	for lvl := uint8(0); int(lvl) < codec.RouterConfigMaxLevels; lvl++ {
		st, ok := t.matrices[matrixKey{matrix: 0, level: lvl}]
		if !ok {
			continue
		}
		bitmap |= 1 << uint(lvl)
		levels = append(levels, codec.RouterConfigResponse1LevelEntry{
			NumDestinations: uint16(st.targetCount),
			NumSources:      uint16(st.sourceCount),
		})
	}
	return codec.RouterConfigResponse1Params{LevelMap: bitmap, Levels: levels}
}

// lookupSource returns the currently-routed source for (matrix, level,
// dst). The second return is true when the tree has a recorded route;
// callers encode the §3.2.5 "destination out of range" sentinel
// (codec.DestOutOfRangeSource) when ok = false.
func (t *tree) lookupSource(m, l uint8, dst uint16) (uint16, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	st, ok := t.matrices[matrixKey{matrix: m, level: l}]
	if !ok {
		return 0, false
	}
	src, ok := st.sources[dst]
	return src, ok
}

// applyConnectLenient records a crosspoint on (matrix, level, dst)
// without rejecting out-of-range indices — used by the salvo commit
// path where the tree may not have declared target/source counts yet.
// When the matrix is unknown, auto-creates it; when target/source
// counts are non-zero, silently skips slots outside those counts so a
// rogue CONNECT ON GO cannot corrupt the state of a well-defined
// tree. Returns true if the crosspoint was recorded, false if skipped.
func (t *tree) applyConnectLenient(m, l uint8, dst, src uint16) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	key := matrixKey{matrix: m, level: l}
	st, ok := t.matrices[key]
	if !ok {
		st = &matrixState{sources: map[uint16]uint16{}}
		t.matrices[key] = st
	}
	if st.targetCount > 0 && int(dst) >= st.targetCount {
		return false
	}
	if st.sourceCount > 0 && int(src) >= st.sourceCount {
		return false
	}
	st.sources[dst] = src
	return true
}
