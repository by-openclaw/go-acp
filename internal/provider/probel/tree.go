package probel

import (
	"fmt"
	"strconv"
	"sync"

	"acp/internal/export/canonical"
)

// matrixKey identifies one (matrix, level) pair within a Probel tree.
// Matrix id + level id are both uint8 on the wire (SW-P-08 §4).
type matrixKey struct {
	matrix uint8
	level  uint8
}

// matrixState holds the crosspoint snapshot for one (matrix, level):
// the current source for each destination plus the declared counts
// (target / source sizes). SW-P-08 is destination-driven — one source
// per destination per level.
type matrixState struct {
	targetCount int
	sourceCount int

	// sources maps destination (0-based) to source (0-based).
	// -1 means "unconnected / unknown".
	sources []int16

	// targetLabels / sourceLabels hold the human-readable names from the
	// canonical tree's TargetLabels / SourceLabels maps. Empty slice =
	// names not declared in the tree; the provider then replies with
	// positional defaults ("SRC 0001", "DST 0001") when queried.
	targetLabels []string
	sourceLabels []string

	// protects maps destination (0-based) → protect record. Absent entry
	// means "no protect" (ProtectNone on the wire). Populated by rx 012
	// Protect Connect / rx 029 Master Protect Connect; cleared by
	// rx 014 Protect Disconnect or rx 007 MaintClearProtects.
	protects map[uint16]protectRecord
}

// protectRecord captures who owns a protect on one destination + which
// of the four spec states applies. See ProtectState in
// cmd_protect_connect.go.
type protectRecord struct {
	deviceID uint16
	state    uint8 // matches iprobel.ProtectState values (0..3)
}

// tree is the in-memory state indexed by (matrix, level). Built from a
// canonical.Export at Serve start. Mutated by SetValue (API path) or by
// incoming CrosspointConnect requests (wire path) — both paths pass
// through the same applyConnect helper so announcements fan out
// consistently.
//
// Device-name table lives alongside matrix state: it is a single
// process-wide map[deviceID]name used by rx 017 Protect Device Name
// Request. Seeded with a handful of positional defaults ("DEV 0001",
// …) so probes against an empty provider return a stable reply.
type tree struct {
	mu          sync.RWMutex
	matrices    map[matrixKey]*matrixState
	deviceNames map[uint16]string
}

// newTree reduces a canonical Export to the per-(matrix,level) state the
// provider needs. Matrix elements get scanned for Labels (which name
// each level) + TargetLabels / SourceLabels (which give names per
// ordinal index).
//
// Probel-specific mapping:
//   - canonical Matrix.TargetCount  → matrixState.targetCount
//   - canonical Matrix.SourceCount  → matrixState.sourceCount
//   - canonical Matrix.Labels[i].BasePath → level i (0-based)
//   - canonical Matrix.TargetLabels["<levelLabel>"]["<n>"] → target name
//   - canonical Matrix.SourceLabels["<levelLabel>"]["<n>"] → source name
//
// MatrixID is derived from the element's Number (1-based canonical) minus 1
// so SW-P-08 IDs start at 0 per spec.
func newTree(exp *canonical.Export) (*tree, error) {
	t := &tree{
		matrices:    map[matrixKey]*matrixState{},
		deviceNames: map[uint16]string{},
	}
	if exp == nil || exp.Root == nil {
		return t, nil
	}
	visit(exp.Root, t)
	return t, nil
}

// setDeviceName registers a human-readable label for deviceID. Called
// from the API path (provider.SetDeviceName) or from test helpers.
func (t *tree) setDeviceName(device uint16, name string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.deviceNames[device] = name
}

// deviceName returns the registered name, or a positional default
// ("DEV 0001" etc.) when unknown — mirrors the TS emulator's behaviour
// so controllers always get a 8-char-max string back.
func (t *tree) deviceName(device uint16) string {
	t.mu.RLock()
	n, ok := t.deviceNames[device]
	t.mu.RUnlock()
	if ok {
		return n
	}
	return fmt.Sprintf("DEV %04d", device)
}

// protectAt returns the protect record for (matrix, level, dst), or
// the zero-value record (state=ProtectNone) when no protect is held.
func (t *tree) protectAt(m, l uint8, dst uint16) protectRecord {
	t.mu.RLock()
	defer t.mu.RUnlock()
	st, ok := t.matrices[matrixKey{matrix: m, level: l}]
	if !ok || st.protects == nil {
		return protectRecord{}
	}
	return st.protects[dst]
}

// applyProtectConnect records a protect ownership on (matrix, level,
// dst) for device. Honours one-owner-wins — a subsequent call with a
// different device overwrites only if the current state allows it
// (ProtectNone or ProtectProbel; ProtectProbelOver blocks). Master
// variant (rx 029) bypasses this check by passing override=true.
func (t *tree) applyProtectConnect(m, l uint8, dst, device uint16, state uint8, override bool) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	st, ok := t.matrices[matrixKey{matrix: m, level: l}]
	if !ok {
		return fmt.Errorf("probel: unknown matrix=%d level=%d", m, l)
	}
	if int(dst) >= st.targetCount {
		return fmt.Errorf("probel: dst %d >= targetCount %d", dst, st.targetCount)
	}
	existing := st.protects[dst]
	if !override && existing.state == uint8(2) { // ProtectProbelOver
		return fmt.Errorf("probel: dst=%d already under override protect by device=%d", dst, existing.deviceID)
	}
	st.protects[dst] = protectRecord{deviceID: device, state: state}
	return nil
}

// applyProtectDisconnect clears the protect on (matrix, level, dst)
// when the caller is the current owner. Returns an error otherwise —
// a Probel-Protected dst cannot be released by a different device.
func (t *tree) applyProtectDisconnect(m, l uint8, dst, device uint16) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	st, ok := t.matrices[matrixKey{matrix: m, level: l}]
	if !ok {
		return fmt.Errorf("probel: unknown matrix=%d level=%d", m, l)
	}
	if int(dst) >= st.targetCount {
		return fmt.Errorf("probel: dst %d >= targetCount %d", dst, st.targetCount)
	}
	existing, had := st.protects[dst]
	if !had {
		return nil // already clear, idempotent
	}
	if existing.deviceID != device {
		return fmt.Errorf("probel: dst=%d owned by device=%d, not %d",
			dst, existing.deviceID, device)
	}
	delete(st.protects, dst)
	return nil
}

// visit walks any canonical Element, adding Matrix elements to the tree.
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

// addMatrix registers one canonical Matrix in the tree. A matrix without
// explicit Labels is treated as single-level (level=0); one with Labels
// gets one matrixState per label entry.
func addMatrix(m *canonical.Matrix, t *tree) {
	matrixID := uint8(m.Number - 1)
	if m.Number <= 0 {
		matrixID = 0
	}

	levels := m.Labels
	if len(levels) == 0 {
		// Single-level matrix — synthesise one anonymous level 0.
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
// SourceLabels maps are indexed by. Convention: description when
// present, falling back to basePath.
func labelKey(lvl canonical.MatrixLabel) string {
	if lvl.Description != nil && *lvl.Description != "" {
		return *lvl.Description
	}
	return lvl.BasePath
}

// buildState constructs the matrixState for one (matrix, level). key is
// the outer key into TargetLabels / SourceLabels (empty for single-level
// matrices).
func buildState(m *canonical.Matrix, key string) *matrixState {
	st := &matrixState{
		targetCount: int(m.TargetCount),
		sourceCount: int(m.SourceCount),
	}
	st.sources = make([]int16, st.targetCount)
	for i := range st.sources {
		st.sources[i] = -1
	}
	st.protects = map[uint16]protectRecord{}
	if st.targetCount > 0 {
		st.targetLabels = buildLabels(m.TargetLabels, key, st.targetCount)
	}
	if st.sourceCount > 0 {
		st.sourceLabels = buildLabels(m.SourceLabels, key, st.sourceCount)
	}
	return st
}

// buildLabels resolves the inner map for one level key into a dense
// ordinal-indexed slice. Missing entries yield an empty string — the
// provider's name-request handler will synthesise positional defaults
// at reply time.
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

// lookup returns the (matrix, level) state if known.
func (t *tree) lookup(m, l uint8) (*matrixState, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	st, ok := t.matrices[matrixKey{matrix: m, level: l}]
	return st, ok
}

// currentSource returns the source currently routed to dst on
// (matrix, level), or (0, false) if unknown or unrouted.
func (t *tree) currentSource(m, l uint8, dst uint16) (uint16, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	st, ok := t.matrices[matrixKey{matrix: m, level: l}]
	if !ok || int(dst) >= len(st.sources) || st.sources[dst] < 0 {
		return 0, false
	}
	return uint16(st.sources[dst]), true
}

// clearProtects wipes the protect map for the given (matrix, level).
// 0xFF wildcards are honoured: matrix=0xFF means "all matrices" and
// level=0xFF means "all levels on the matched matrix". Called by the
// rx 007 MaintClearProtects handler. Harmless when protect state is
// empty (the default until a rx 012 Protect Connect lands).
func (t *tree) clearProtects(matrix, level uint8) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for k, st := range t.matrices {
		if matrix != 0xFF && k.matrix != matrix {
			continue
		}
		if level != 0xFF && k.level != level {
			continue
		}
		for d := range st.protects {
			delete(st.protects, d)
		}
	}
}

// updateSourceLabels overwrites sourceLabels[first:first+len(names)] on
// (matrix, level). Labels beyond sourceCount are silently dropped.
// Called by the rx 117 UPDATE NAME handler for Source + SourceAssoc
// name types.
func (t *tree) updateSourceLabels(m, l uint8, first uint16, names []string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	st, ok := t.matrices[matrixKey{matrix: m, level: l}]
	if !ok {
		return
	}
	if st.sourceLabels == nil {
		st.sourceLabels = make([]string, st.sourceCount)
	}
	for i, n := range names {
		idx := int(first) + i
		if idx < 0 || idx >= st.sourceCount {
			continue
		}
		st.sourceLabels[idx] = n
	}
}

// updateTargetLabels mirrors updateSourceLabels for destinations.
func (t *tree) updateTargetLabels(m, l uint8, first uint16, names []string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	st, ok := t.matrices[matrixKey{matrix: m, level: l}]
	if !ok {
		return
	}
	if st.targetLabels == nil {
		st.targetLabels = make([]string, st.targetCount)
	}
	for i, n := range names {
		idx := int(first) + i
		if idx < 0 || idx >= st.targetCount {
			continue
		}
		st.targetLabels[idx] = n
	}
}

// Size reports how many (matrix, level) pairs are in the tree.
func (t *tree) Size() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.matrices)
}

// applyConnect records a crosspoint connection on (matrix, level, dst).
// Returns an error if the indices are out of range. Mutations go
// through this single chokepoint so any future announce fan-out stays
// consistent.
func (t *tree) applyConnect(m, l uint8, dst uint16, src uint16) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	st, ok := t.matrices[matrixKey{matrix: m, level: l}]
	if !ok {
		return fmt.Errorf("probel: unknown matrix=%d level=%d", m, l)
	}
	if int(dst) >= len(st.sources) {
		return fmt.Errorf("probel: dst %d >= targetCount %d on matrix=%d level=%d",
			dst, len(st.sources), m, l)
	}
	if int(src) >= st.sourceCount {
		return fmt.Errorf("probel: src %d >= sourceCount %d on matrix=%d level=%d",
			src, st.sourceCount, m, l)
	}
	st.sources[dst] = int16(src)
	return nil
}

