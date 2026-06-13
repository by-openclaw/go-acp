package emberplus

import (
	"context"
	"testing"

	"dhs/internal/datastore"
	"dhs/internal/emberplus/codec/glow"
)

// TestSplitAndPersistDM drives the slot-splitting persistence path: a
// root with a matrix-bearing slot child, an empty (non-slot) child, plus
// the empty-tree and cancelled-ctx error branches.
func TestSplitAndPersistDM_Slots(t *testing.T) {
	store := datastore.NewTreeStore(t.TempDir())

	p := freshPlugin()
	// root(1) → slot node "oneToN"(1.1) → matrix(1.1.1); empty node(1.2).
	seedEntry(p, []int32{1}, "root", func(e *treeEntry) {
		e.glowNode = &glow.Node{Number: 1, Identifier: "root"}
	})
	seedEntry(p, []int32{1, 1}, "oneToN", func(e *treeEntry) {
		e.glowNode = &glow.Node{Number: 1, Identifier: "oneToN"}
	})
	seedEntry(p, []int32{1, 1, 1}, "mat", func(e *treeEntry) {
		e.glowMatrix = &glow.Matrix{Number: 1, Identifier: "mat", MatrixType: glow.MatrixTypeOneToN, TargetCount: 1, SourceCount: 1}
	})
	seedEntry(p, []int32{1, 2}, "empty", func(e *treeEntry) {
		e.glowNode = &glow.Node{Number: 2, Identifier: "empty"}
	})

	refs, err := p.SplitAndPersistDM(context.Background(), store, "TinyEmberPlusRouter@1.6.2")
	if err != nil {
		t.Fatalf("SplitAndPersistDM: %v", err)
	}
	if len(refs) != 1 || refs[0].Identity != "oneToN@1.6.2" {
		t.Errorf("slot refs = %+v, want one oneToN@1.6.2", refs)
	}

	// Cancelled ctx → ExportCanonical error propagates.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.SplitAndPersistDM(ctx, store, "x@1"); err == nil {
		t.Error("cancelled ctx should error")
	}

	// Empty tree → synthetic root with no slot-worthy children → 0 refs,
	// no error.
	empty := freshPlugin()
	got, err := empty.SplitAndPersistDM(context.Background(), store, "x@1")
	if err != nil {
		t.Errorf("empty tree: unexpected error %v", err)
	}
	if len(got) != 0 {
		t.Errorf("empty tree should yield 0 slot refs, got %d", len(got))
	}
}
