package probelsw08p

import (
	"errors"
	"testing"

	"dhs/internal/export/canonical"
)

// freshTree returns an empty tree with all maps initialised, matching
// what newTree produces.
func freshTree() *tree {
	return &tree{
		matrices:    map[matrixKey]*matrixState{},
		deviceNames: map[uint16]string{},
		salvos:      map[uint8][]salvoSlot{},
	}
}

// TestNewTreeBuildErrorFallback installs the test-only treeBuildErrHook
// so newTree returns an error, then asserts newServer absorbs it and
// still produces a live server with an empty (non-nil) tree. The guard
// in newServer (logger.Error + empty-tree fallback) is otherwise
// unreachable because newTree never fails in production.
func TestNewTreeBuildErrorFallback(t *testing.T) {
	sentinel := errors.New("forced tree build failure")
	treeBuildErrHook = func() error { return sentinel }
	defer func() { treeBuildErrHook = nil }()

	srv := newComplianceServer(t)
	if srv == nil {
		t.Fatal("newServer returned nil despite tree-build fallback")
	}
	if srv.tree == nil || srv.tree.matrices == nil {
		t.Fatalf("fallback tree not initialised: %+v", srv.tree)
	}
	if srv.tree.Size() != 0 {
		t.Errorf("fallback tree size = %d; want 0 (empty)", srv.tree.Size())
	}
}

// TestNewTreeNilExport: a nil Export / nil Root yields an empty tree.
func TestNewTreeNilExport(t *testing.T) {
	tr, err := newTree(nil)
	if err != nil {
		t.Fatalf("newTree(nil): %v", err)
	}
	if tr.Size() != 0 {
		t.Errorf("size = %d; want 0", tr.Size())
	}
	tr2, err := newTree(&canonical.Export{})
	if err != nil {
		t.Fatalf("newTree(empty): %v", err)
	}
	if tr2.Size() != 0 {
		t.Errorf("size = %d; want 0", tr2.Size())
	}
}

// TestAddMatrixSingleLevelAndZeroNumber: a Matrix with no Labels yields
// a single anonymous level 0, and Number <= 0 clamps matrixID to 0.
func TestAddMatrixSingleLevelAndZeroNumber(t *testing.T) {
	exp := &canonical.Export{
		Root: &canonical.Node{
			Header: canonical.Header{
				Number: 1, Identifier: "router", OID: "1",
				Children: []canonical.Element{
					&canonical.Matrix{
						Header: canonical.Header{Number: 0, Identifier: "m", OID: "1.1"},
						Type:   canonical.MatrixOneToN, Mode: canonical.ModeLinear,
						TargetCount: 4, SourceCount: 4,
						// No Labels → single-level synthesis path.
					},
				},
			},
		},
	}
	tr, err := newTree(exp)
	if err != nil {
		t.Fatalf("newTree: %v", err)
	}
	if tr.Size() != 1 {
		t.Fatalf("size = %d; want 1 single level", tr.Size())
	}
	if _, ok := tr.lookup(0, 0); !ok {
		t.Error("matrixID 0 / level 0 not registered (Number<=0 clamp failed)")
	}
}

// TestBuildLabelsMissingKey: buildLabels returns all-empty when the
// outer map lacks the level key (the !ok early return).
func TestBuildLabelsMissingKey(t *testing.T) {
	outer := map[string]map[string]string{"Other": {"0": "X"}}
	got := buildLabels(outer, "Video", 3)
	if len(got) != 3 {
		t.Fatalf("len = %d; want 3", len(got))
	}
	for i, s := range got {
		if s != "" {
			t.Errorf("got[%d] = %q; want empty", i, s)
		}
	}
	// nil outer map also returns all-empty.
	if g := buildLabels(nil, "Video", 2); len(g) != 2 || g[0] != "" {
		t.Errorf("nil outer = %v; want [\"\" \"\"]", g)
	}

	// Invalid inner keys (non-numeric, negative, out-of-range) are skipped
	// via the continue arm; only the valid "1" entry lands.
	withBad := map[string]map[string]string{
		"Video": {"abc": "X", "-1": "Y", "9": "Z", "1": "OK"},
	}
	g := buildLabels(withBad, "Video", 3)
	if g[1] != "OK" {
		t.Errorf("g[1] = %q; want OK", g[1])
	}
	if g[0] != "" || g[2] != "" {
		t.Errorf("invalid keys leaked: %v", g)
	}
}

// TestDeviceNameKnown: a registered name is returned verbatim; an
// unknown id falls back to the positional default.
func TestDeviceNameKnown(t *testing.T) {
	tr := freshTree()
	tr.setDeviceName(7, "MIXER")
	if got := tr.deviceName(7); got != "MIXER" {
		t.Errorf("deviceName(7) = %q; want MIXER", got)
	}
	if got := tr.deviceName(42); got != "DEV 0042" {
		t.Errorf("deviceName(42) = %q; want DEV 0042", got)
	}
}

// TestProtectAtUnknown: protectAt on an unknown (matrix, level) returns
// the zero record.
func TestProtectAtUnknown(t *testing.T) {
	tr := freshTree()
	rec := tr.protectAt(5, 5, 0)
	if rec.deviceID != 0 || rec.state != 0 {
		t.Errorf("rec = %+v; want zero record", rec)
	}
}

// TestApplyProtectConnectErrors covers the unknown-matrix, dst-overflow,
// and override-block arms.
func TestApplyProtectConnectErrors(t *testing.T) {
	tr := freshTree()
	tr.matrices[matrixKey{0, 0}] = &matrixState{
		targetCount: 4, sourceCount: 4,
		sources:  map[uint16]uint16{},
		protects: map[uint16]protectRecord{},
	}
	if err := tr.applyProtectConnect(9, 0, 0, 1, 1, false); err == nil {
		t.Error("unknown matrix: want error")
	}
	if err := tr.applyProtectConnect(0, 0, 99, 1, 1, false); err == nil {
		t.Error("dst overflow: want error")
	}
	// Seed an override protect (state 2), then a non-override connect by a
	// different device must be blocked.
	if err := tr.applyProtectConnect(0, 0, 0, 1, 2, true); err != nil {
		t.Fatalf("seed override: %v", err)
	}
	if err := tr.applyProtectConnect(0, 0, 0, 2, 1, false); err == nil {
		t.Error("non-override over an override protect: want error")
	}
	// override=true seizes it.
	if err := tr.applyProtectConnect(0, 0, 0, 2, 2, true); err != nil {
		t.Errorf("override seize: %v", err)
	}
}

// TestApplyProtectDisconnectBranches covers unknown-matrix, dst-overflow,
// not-held (idempotent nil), and wrong-owner arms.
func TestApplyProtectDisconnectBranches(t *testing.T) {
	tr := freshTree()
	tr.matrices[matrixKey{0, 0}] = &matrixState{
		targetCount: 4, sourceCount: 4,
		sources:  map[uint16]uint16{},
		protects: map[uint16]protectRecord{},
	}
	if err := tr.applyProtectDisconnect(9, 0, 0, 1); err == nil {
		t.Error("unknown matrix: want error")
	}
	if err := tr.applyProtectDisconnect(0, 0, 99, 1); err == nil {
		t.Error("dst overflow: want error")
	}
	// Not held → idempotent nil.
	if err := tr.applyProtectDisconnect(0, 0, 0, 1); err != nil {
		t.Errorf("disconnect unheld dst: %v; want nil", err)
	}
	// Held by 42; disconnect by 7 → wrong-owner error.
	if err := tr.applyProtectConnect(0, 0, 0, 42, 1, false); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := tr.applyProtectDisconnect(0, 0, 0, 7); err == nil {
		t.Error("wrong owner: want error")
	}
	// Correct owner releases.
	if err := tr.applyProtectDisconnect(0, 0, 0, 42); err != nil {
		t.Errorf("owner release: %v", err)
	}
}

// TestVisitNil: visit on a nil element is a no-op (early return).
func TestVisitNil(t *testing.T) {
	tr := freshTree()
	visit(nil, tr)
	if tr.Size() != 0 {
		t.Errorf("size = %d; want 0 after visit(nil)", tr.Size())
	}
}

// TestClearProtectsWildcards covers the matrix and level wildcard
// continue arms plus a real clear.
func TestClearProtectsWildcards(t *testing.T) {
	tr := freshTree()
	mk := func(m, l uint8) {
		tr.matrices[matrixKey{m, l}] = &matrixState{
			targetCount: 4, sourceCount: 4,
			sources:  map[uint16]uint16{},
			protects: map[uint16]protectRecord{0: {deviceID: 1, state: 1}},
		}
	}
	mk(0, 0)
	mk(0, 1)
	mk(1, 0)

	// Clear matrix 0 level 0 only — the others keep their protect (both
	// continue arms exercised: matrix!=0 skip and level!=0 skip).
	tr.clearProtects(0, 0)
	if len(tr.matrices[matrixKey{0, 0}].protects) != 0 {
		t.Error("matrix0/level0 protect not cleared")
	}
	if len(tr.matrices[matrixKey{0, 1}].protects) != 1 {
		t.Error("matrix0/level1 protect wrongly cleared")
	}
	if len(tr.matrices[matrixKey{1, 0}].protects) != 1 {
		t.Error("matrix1/level0 protect wrongly cleared")
	}

	// Wildcard-all clears everything.
	tr.clearProtects(0xFF, 0xFF)
	for k, st := range tr.matrices {
		if len(st.protects) != 0 {
			t.Errorf("%v protects not cleared by wildcard", k)
		}
	}
}

// TestUpdateLabelsNilInitAndSkip covers updateSourceLabels /
// updateTargetLabels: unknown matrix early return, nil-slice lazy init,
// and out-of-range index skip.
func TestUpdateLabelsNilInitAndSkip(t *testing.T) {
	tr := freshTree()
	// Unknown matrix early-returns silently.
	tr.updateSourceLabels(9, 0, 0, []string{"X"})
	tr.updateTargetLabels(9, 0, 0, []string{"X"})

	// State with nil label slices to force lazy init.
	tr.matrices[matrixKey{0, 0}] = &matrixState{
		targetCount: 2, sourceCount: 2,
		sources:  map[uint16]uint16{},
		protects: map[uint16]protectRecord{},
		// sourceLabels / targetLabels intentionally nil.
	}
	// first=1 with two names → index 1 in range, index 2 out of range
	// (skip arm).
	tr.updateSourceLabels(0, 0, 1, []string{"S1", "S2"})
	tr.updateTargetLabels(0, 0, 1, []string{"T1", "T2"})
	st := tr.matrices[matrixKey{0, 0}]
	if len(st.sourceLabels) != 2 || st.sourceLabels[1] != "S1" {
		t.Errorf("sourceLabels = %v; want [_, S1]", st.sourceLabels)
	}
	if len(st.targetLabels) != 2 || st.targetLabels[1] != "T1" {
		t.Errorf("targetLabels = %v; want [_, T1]", st.targetLabels)
	}
}

// TestSalvoAppendNilInit covers the lazy salvos-map init in salvoAppend
// when the map starts nil.
func TestSalvoAppendNilInit(t *testing.T) {
	tr := &tree{matrices: map[matrixKey]*matrixState{}} // salvos nil
	tr.salvoAppend(3, salvoSlot{matrix: 0, level: 0, dst: 1, src: 2})
	slot, ok, last := tr.salvoSlotAt(3, 0)
	if !ok || !last {
		t.Fatalf("salvoSlotAt = (%+v, ok=%v, last=%v); want ok+last", slot, ok, last)
	}
	if slot.dst != 1 || slot.src != 2 {
		t.Errorf("slot = %+v; want dst=1 src=2", slot)
	}
}

// TestCurrentSourceDstOverflow: currentSource returns (0,false) when dst
// is at/past targetCount even on a known (matrix, level).
func TestCurrentSourceDstOverflow(t *testing.T) {
	tr := freshTree()
	tr.matrices[matrixKey{0, 0}] = &matrixState{
		targetCount: 4, sourceCount: 4,
		sources:  map[uint16]uint16{},
		protects: map[uint16]protectRecord{},
	}
	if src, ok := tr.currentSource(0, 0, 99); ok || src != 0 {
		t.Errorf("currentSource(dst=99) = (%d,%v); want (0,false)", src, ok)
	}
}

// TestApplyConnectErrors covers the unknown-matrix, dst-overflow, and
// src-overflow arms.
func TestApplyConnectErrors(t *testing.T) {
	tr := freshTree()
	tr.matrices[matrixKey{0, 0}] = &matrixState{
		targetCount: 4, sourceCount: 4,
		sources:  map[uint16]uint16{},
		protects: map[uint16]protectRecord{},
	}
	if err := tr.applyConnect(9, 0, 0, 0); err == nil {
		t.Error("unknown matrix: want error")
	}
	if err := tr.applyConnect(0, 0, 99, 0); err == nil {
		t.Error("dst overflow: want error")
	}
	if err := tr.applyConnect(0, 0, 0, 99); err == nil {
		t.Error("src overflow: want error")
	}
	if err := tr.applyConnect(0, 0, 1, 2); err != nil {
		t.Errorf("valid connect: %v", err)
	}
}
