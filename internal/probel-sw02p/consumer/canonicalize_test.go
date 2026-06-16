package probelsw02p

import (
	"context"
	"testing"

	"dhs/internal/export/canonical"
)

// TestCanonicalizeEmptyConfig: a pre-Connect plugin with a zero-value
// MatrixConfig yields a lone device Node with an empty children slice.
func TestCanonicalizeEmptyConfig(t *testing.T) {
	p := &Plugin{logger: quietLogger()}
	exp, err := p.Canonicalize(context.Background())
	if err != nil {
		t.Fatalf("Canonicalize: %v", err)
	}
	root, ok := exp.Root.(*canonical.Node)
	if !ok {
		t.Fatalf("root = %T; want *canonical.Node", exp.Root)
	}
	if root.Identifier != "device" {
		t.Errorf("root.Identifier = %q; want \"device\" (no host set)", root.Identifier)
	}
	if root.OID != "1" {
		t.Errorf("root.OID = %q; want \"1\"", root.OID)
	}
	if len(root.Children) != 0 {
		t.Errorf("root.Children = %d entries; want 0 (empty config)", len(root.Children))
	}
}

// TestCanonicalizeWithMatrix: a configured plugin with a host emits a
// device Node parenting one Matrix shaped from the MatrixConfig.
func TestCanonicalizeWithMatrix(t *testing.T) {
	p := &Plugin{logger: quietLogger()}
	p.host = "matrix.example"
	p.matrixCfg = MatrixConfig{MatrixID: 2, Level: 1, Dsts: 128, Srcs: 64}

	exp, err := p.Canonicalize(context.Background())
	if err != nil {
		t.Fatalf("Canonicalize: %v", err)
	}
	root, ok := exp.Root.(*canonical.Node)
	if !ok {
		t.Fatalf("root = %T; want *canonical.Node", exp.Root)
	}
	if root.Identifier != "matrix.example" {
		t.Errorf("root.Identifier = %q; want host", root.Identifier)
	}
	if len(root.Children) != 1 {
		t.Fatalf("root.Children = %d; want 1 matrix", len(root.Children))
	}
	m, ok := root.Children[0].(*canonical.Matrix)
	if !ok {
		t.Fatalf("child = %T; want *canonical.Matrix", root.Children[0])
	}
	if m.Identifier != "matrix-2.level-1" {
		t.Errorf("matrix.Identifier = %q; want matrix-2.level-1", m.Identifier)
	}
	if m.OID != "1.3.2" {
		t.Errorf("matrix.OID = %q; want 1.3.2 (matrixID+1, level+1)", m.OID)
	}
	if m.TargetCount != 128 || m.SourceCount != 64 {
		t.Errorf("matrix counts = (%d, %d); want (128, 64)", m.TargetCount, m.SourceCount)
	}
	if m.Type != canonical.MatrixOneToN || m.Mode != canonical.ModeLinear {
		t.Errorf("matrix type/mode = (%q, %q); want (oneToN, linear)", m.Type, m.Mode)
	}
}

// TestCanonicalizeSrcsOnly covers the boundary where Dsts == 0 but
// Srcs != 0 — the matrix branch still fires (the empty-children guard
// requires BOTH to be zero).
func TestCanonicalizeSrcsOnly(t *testing.T) {
	p := &Plugin{logger: quietLogger()}
	p.matrixCfg = MatrixConfig{Srcs: 8}
	exp, err := p.Canonicalize(context.Background())
	if err != nil {
		t.Fatalf("Canonicalize: %v", err)
	}
	root := exp.Root.(*canonical.Node)
	if len(root.Children) != 1 {
		t.Errorf("Children = %d; want 1 (Srcs!=0 forces the matrix branch)", len(root.Children))
	}
}

// TestCanonicalizeContextCanceled: a canceled context short-circuits
// before any tree is built.
func TestCanonicalizeContextCanceled(t *testing.T) {
	p := &Plugin{logger: quietLogger()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Canonicalize(ctx); err == nil {
		t.Error("Canonicalize(canceled ctx) = nil error; want non-nil")
	}
}
