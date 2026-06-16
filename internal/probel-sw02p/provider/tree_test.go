package probelsw02p

import (
	"testing"

	"dhs/internal/export/canonical"
	"dhs/internal/probel-sw02p/codec"
)

// strptr is a tiny *string helper for canonical Description fields.
func strptr(s string) *string { return &s }

// TestNewTreeNilExport confirms newTree tolerates a nil Export and a
// nil Root — both yield an empty (but non-nil) tree.
func TestNewTreeNilExport(t *testing.T) {
	t1, err := newTree(nil)
	if err != nil {
		t.Fatalf("newTree(nil): %v", err)
	}
	if t1 == nil || t1.Size() != 0 {
		t.Errorf("newTree(nil) = %+v; want empty tree", t1)
	}
	t2, err := newTree(&canonical.Export{Root: nil})
	if err != nil {
		t.Fatalf("newTree(Root=nil): %v", err)
	}
	if t2.Size() != 0 {
		t.Errorf("newTree(Root=nil) size = %d; want 0", t2.Size())
	}
}

// TestVisitSkipsNilChild covers the e == nil guard in visit: a Node
// whose Children slice contains a nil element must not panic and must
// still register the sibling Matrix.
func TestVisitSkipsNilChild(t *testing.T) {
	exp := &canonical.Export{
		Root: &canonical.Node{
			Header: canonical.Header{Number: 1, Identifier: "root", OID: "1",
				Children: []canonical.Element{
					nil,
					&canonical.Matrix{
						Header:      canonical.Header{Number: 1, Identifier: "m", OID: "1.1"},
						TargetCount: 2, SourceCount: 2,
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
		t.Errorf("size = %d; want 1 (nil child skipped, matrix kept)", tr.Size())
	}
}

// TestAddMatrixNonPositiveNumberMapsToMatrixZero covers the
// m.Number <= 0 branch in addMatrix: a Matrix with Number 0 is stored
// at matrix id 0 (rather than underflowing uint8(Number-1)).
func TestAddMatrixNonPositiveNumberMapsToMatrixZero(t *testing.T) {
	exp := &canonical.Export{
		Root: &canonical.Node{
			Header: canonical.Header{Number: 1, Identifier: "root", OID: "1",
				Children: []canonical.Element{
					&canonical.Matrix{
						Header:      canonical.Header{Number: 0, Identifier: "m", OID: "1.1"},
						TargetCount: 2, SourceCount: 2,
					},
				},
			},
		},
	}
	tr, err := newTree(exp)
	if err != nil {
		t.Fatalf("newTree: %v", err)
	}
	if _, ok := tr.matrices[matrixKey{matrix: 0, level: 0}]; !ok {
		t.Errorf("matrix Number=0 not stored at id 0; matrices = %+v", tr.matrices)
	}
}

// TestAddMatrixWithLevelsAndLabels exercises the multi-level branch of
// addMatrix + labelKey (Description present) + buildLabels (outer map
// with a resolvable inner map, plus a bad ordinal key that is skipped).
func TestAddMatrixWithLevelsAndLabels(t *testing.T) {
	exp := &canonical.Export{
		Root: &canonical.Node{
			Header: canonical.Header{Number: 1, Identifier: "root", OID: "1",
				Children: []canonical.Element{
					&canonical.Matrix{
						Header:      canonical.Header{Number: 1, Identifier: "m", OID: "1.1"},
						TargetCount: 3, SourceCount: 3,
						Labels: []canonical.MatrixLabel{
							{BasePath: "root.m.video", Description: strptr("Primary")},
						},
						TargetLabels: map[string]map[string]string{
							"Primary": {
								"0":   "OUT 1",
								"2":   "OUT 3",
								"bad": "ignored", // non-numeric key → skipped
								"9":   "oob",     // >= n → skipped
							},
						},
						SourceLabels: map[string]map[string]string{
							"Primary": {"1": "IN 2"},
						},
					},
				},
			},
		},
	}
	tr, err := newTree(exp)
	if err != nil {
		t.Fatalf("newTree: %v", err)
	}
	st, ok := tr.matrices[matrixKey{matrix: 0, level: 0}]
	if !ok {
		t.Fatal("level 0 not registered")
	}
	if len(st.targetLabels) != 3 {
		t.Fatalf("targetLabels len = %d; want 3", len(st.targetLabels))
	}
	if st.targetLabels[0] != "OUT 1" || st.targetLabels[2] != "OUT 3" {
		t.Errorf("targetLabels = %v; want [OUT 1, '', OUT 3]", st.targetLabels)
	}
	if st.targetLabels[1] != "" {
		t.Errorf("targetLabels[1] = %q; want empty (no key)", st.targetLabels[1])
	}
	if st.sourceLabels[1] != "IN 2" {
		t.Errorf("sourceLabels[1] = %q; want IN 2", st.sourceLabels[1])
	}
}

// TestLabelKeyFallsBackToBasePath covers labelKey's BasePath branch:
// a level with no Description (nil) keys the label maps by BasePath, so
// a TargetLabels map keyed by BasePath resolves.
func TestLabelKeyFallsBackToBasePath(t *testing.T) {
	exp := &canonical.Export{
		Root: &canonical.Node{
			Header: canonical.Header{Number: 1, Identifier: "root", OID: "1",
				Children: []canonical.Element{
					&canonical.Matrix{
						Header:      canonical.Header{Number: 1, Identifier: "m", OID: "1.1"},
						TargetCount: 1, SourceCount: 1,
						Labels: []canonical.MatrixLabel{
							{BasePath: "root.m.video"}, // Description nil → BasePath key
						},
						TargetLabels: map[string]map[string]string{
							"root.m.video": {"0": "OUT 1"},
						},
					},
				},
			},
		},
	}
	tr, err := newTree(exp)
	if err != nil {
		t.Fatalf("newTree: %v", err)
	}
	st := tr.matrices[matrixKey{matrix: 0, level: 0}]
	if st.targetLabels[0] != "OUT 1" {
		t.Errorf("targetLabels[0] = %q; want OUT 1 (BasePath key)", st.targetLabels[0])
	}
}

// TestLabelKeyEmptyDescriptionFallsBackToBasePath covers the
// *Description != "" half of labelKey: a non-nil but empty Description
// also falls back to BasePath.
func TestLabelKeyEmptyDescriptionFallsBackToBasePath(t *testing.T) {
	got := labelKey(canonical.MatrixLabel{BasePath: "bp", Description: strptr("")})
	if got != "bp" {
		t.Errorf("labelKey(empty desc) = %q; want bp", got)
	}
}

// TestBuildLabelsMissingInnerKey covers buildLabels' inner-not-found
// branch: an outer map that lacks the requested level key returns an
// all-empty slice sized to n.
func TestBuildLabelsMissingInnerKey(t *testing.T) {
	out := buildLabels(map[string]map[string]string{"Other": {"0": "x"}}, "Primary", 2)
	if len(out) != 2 || out[0] != "" || out[1] != "" {
		t.Errorf("buildLabels(missing key) = %v; want two empties", out)
	}
}

// TestApplyConnectErrors covers the three error arms of applyConnect:
// unknown matrix, dst out of range, src out of range.
func TestApplyConnectErrors(t *testing.T) {
	srv := newTestServer(t) // matrix 0 level 0, counts 4/4

	if err := srv.tree.applyConnect(9, 9, 0, 0); err == nil {
		t.Error("applyConnect(unknown matrix) = nil; want error")
	}
	if err := srv.tree.applyConnect(0, 0, 99, 0); err == nil {
		t.Error("applyConnect(dst oob) = nil; want error")
	}
	if err := srv.tree.applyConnect(0, 0, 0, 99); err == nil {
		t.Error("applyConnect(src oob) = nil; want error")
	}
	// Happy path still works.
	if err := srv.tree.applyConnect(0, 0, 1, 2); err != nil {
		t.Errorf("applyConnect(valid) = %v; want nil", err)
	}
}

// TestDrainPendingUnknownMatrix covers drainPending's unknown-key arm:
// a GO on a matrix with no state returns nil rather than erroring.
func TestDrainPendingUnknownMatrix(t *testing.T) {
	tr := &tree{matrices: map[matrixKey]*matrixState{}}
	if got := tr.drainPending(7, 3); got != nil {
		t.Errorf("drainPending(unknown) = %v; want nil", got)
	}
}

// TestProtectClearRejectsOverride covers protectClear's
// ProbelOverride arm: clearing a ProtectProBelOverride entry is
// rejected and echoes the unchanged entry.
func TestProtectClearRejectsOverride(t *testing.T) {
	srv := newTestServer(t)
	srv.tree.mu.Lock()
	st := srv.tree.matrices[matrixKey{matrix: 0, level: 0}]
	st.protect = map[uint16]protectEntry{
		1: {State: codec.ProtectProBelOverride, OwnerDevice: 7},
	}
	srv.tree.mu.Unlock()

	entry, res := srv.tree.protectClear(1, 7)
	if res != protectApplyRejectedOverride {
		t.Errorf("protectClear(override) = %v; want protectApplyRejectedOverride", res)
	}
	if entry.State != codec.ProtectProBelOverride {
		t.Errorf("echoed state = %v; want unchanged ProbelOverride", entry.State)
	}
}

// TestProtectClearAbsentDstNoOp covers protectClear's !exists arm when
// the protect map exists but the queried dst has no entry: the result
// is an accepted no-op echoing State=None. Distinct from the
// protect==nil early return — here the map is non-nil.
func TestProtectClearAbsentDstNoOp(t *testing.T) {
	srv := newTestServer(t)
	srv.tree.mu.Lock()
	st := srv.tree.matrices[matrixKey{matrix: 0, level: 0}]
	// Non-nil map with an unrelated entry so the dst lookup misses.
	st.protect = map[uint16]protectEntry{
		1: {State: codec.ProtectProBel, OwnerDevice: 7},
	}
	srv.tree.mu.Unlock()

	entry, res := srv.tree.protectClear(99, 7) // dst 99 absent
	if res != protectApplyAccepted {
		t.Errorf("protectClear(absent dst) = %v; want protectApplyAccepted", res)
	}
	if entry.State != codec.ProtectNone {
		t.Errorf("echoed state = %v; want ProtectNone", entry.State)
	}
}

// TestSourceLockSnapshotSkipsNonMatrixZero covers the key.matrix != 0
// continue arm of sourceLockSnapshot: only matrix-0 levels contribute
// to the source count; a wider matrix-1 level is ignored.
func TestSourceLockSnapshotSkipsNonMatrixZero(t *testing.T) {
	tr := &tree{matrices: map[matrixKey]*matrixState{
		{matrix: 0, level: 0}: {sourceCount: 2, sources: map[uint16]uint16{}},
		{matrix: 1, level: 0}: {sourceCount: 9, sources: map[uint16]uint16{}}, // ignored
	}}
	got := tr.sourceLockSnapshot()
	if len(got) != 2 {
		t.Fatalf("snapshot len = %d; want 2 (matrix-1 ignored)", len(got))
	}
	for i, v := range got {
		if !v {
			t.Errorf("snapshot[%d] = false; want true (software default)", i)
		}
	}
}

// TestProtectDumpCountCap covers the count-limit branch of protectDump:
// with more stored entries than count, only the first count (in
// ascending-dst order) are returned.
func TestProtectDumpCountCap(t *testing.T) {
	srv := newTestServer(t)
	srv.tree.mu.Lock()
	st := srv.tree.matrices[matrixKey{matrix: 0, level: 0}]
	st.protect = map[uint16]protectEntry{
		5: {State: codec.ProtectProBel, OwnerDevice: 1},
		2: {State: codec.ProtectProBel, OwnerDevice: 1},
		8: {State: codec.ProtectProBel, OwnerDevice: 1},
		1: {State: codec.ProtectProBel, OwnerDevice: 1},
	}
	srv.tree.mu.Unlock()

	out := srv.tree.protectDump(0, 2)
	if len(out) != 2 {
		t.Fatalf("protectDump(count=2) len = %d; want 2", len(out))
	}
	if out[0].Destination != 1 || out[1].Destination != 2 {
		t.Errorf("protectDump order = [%d,%d]; want ascending [1,2]",
			out[0].Destination, out[1].Destination)
	}
}

// TestProtectEntriesByDeviceMatches covers protectEntriesByDevice's
// match loop: only entries whose OwnerDevice equals the query are
// returned.
func TestProtectEntriesByDeviceMatches(t *testing.T) {
	srv := newTestServer(t)
	srv.tree.mu.Lock()
	st := srv.tree.matrices[matrixKey{matrix: 0, level: 0}]
	st.protect = map[uint16]protectEntry{
		1: {State: codec.ProtectProBel, OwnerDevice: 42, OwnerName: "A"},
		2: {State: codec.ProtectProBel, OwnerDevice: 99},
		3: {State: codec.ProtectProBel, OwnerDevice: 42},
	}
	srv.tree.mu.Unlock()

	got := srv.tree.protectEntriesByDevice(42)
	if len(got) != 2 {
		t.Fatalf("protectEntriesByDevice(42) len = %d; want 2", len(got))
	}
	for _, e := range got {
		if e.OwnerDevice != 42 {
			t.Errorf("entry OwnerDevice = %d; want 42", e.OwnerDevice)
		}
	}
	// No match → empty (non-nil) slice.
	if none := srv.tree.protectEntriesByDevice(7); len(none) != 0 {
		t.Errorf("protectEntriesByDevice(7) = %v; want empty", none)
	}
}

// TestProtectEntriesByDeviceNoProtectMap covers the early-return arm
// when matrix 0 has no protect map at all.
func TestProtectEntriesByDeviceNoProtectMap(t *testing.T) {
	tr := &tree{matrices: map[matrixKey]*matrixState{}}
	if got := tr.protectEntriesByDevice(1); got != nil {
		t.Errorf("protectEntriesByDevice(no matrix) = %v; want nil", got)
	}
}

// TestSourceLockSnapshotEmptyTree covers the maxSrc == 0 arm: a tree
// whose matrix 0 declares no sources returns nil.
func TestSourceLockSnapshotEmptyTree(t *testing.T) {
	tr := &tree{matrices: map[matrixKey]*matrixState{
		{matrix: 0, level: 0}: {sources: map[uint16]uint16{}}, // sourceCount 0
	}}
	if got := tr.sourceLockSnapshot(); got != nil {
		t.Errorf("sourceLockSnapshot(no sources) = %v; want nil", got)
	}
}

// TestLookupSourceUnknownMatrix covers lookupSource's unknown-matrix
// arm.
func TestLookupSourceUnknownMatrix(t *testing.T) {
	tr := &tree{matrices: map[matrixKey]*matrixState{}}
	if _, ok := tr.lookupSource(0, 0, 0); ok {
		t.Error("lookupSource(unknown) ok = true; want false")
	}
}

// TestApplyConnectLenientSrcOutOfRange covers the src-out-of-range skip
// in applyConnectLenient: a declared sourceCount rejects an oversized
// source while leaving the tree untouched.
func TestApplyConnectLenientSrcOutOfRange(t *testing.T) {
	srv := newTestServer(t) // counts 4/4
	if ok := srv.tree.applyConnectLenient(0, 0, 0, 99); ok {
		t.Error("applyConnectLenient(src oob) = true; want false")
	}
	if _, ok := srv.tree.matrices[matrixKey{matrix: 0, level: 0}].sources[0]; ok {
		t.Error("tree[0] recorded despite src oob")
	}
}
