package acp1

import (
	"context"
	"testing"

	"dhs/internal/acp1/codec"
)

// TestLookup_NilTree covers the nil-tree early return.
func TestLookup_NilTree(t *testing.T) {
	var tree *SlotTree
	if idx := tree.Lookup("control", "X"); idx != -1 {
		t.Fatalf("nil tree Lookup = %d, want -1", idx)
	}
}

// TestWalk_GetObjectDoError: the root getObject's Do errors (empty recv →
// io.EOF) → getObject's Do-error arm surfaces.
func TestWalk_GetObjectDoError(t *testing.T) {
	p, _, _ := walkPlugin(t) // no recv queued → Do returns io.EOF
	if _, err := p.Walk(context.Background(), 0); err == nil {
		t.Fatal("Walk with Do error: want error")
	}
}

// TestWalk_RootTypeMismatch: the root reply decodes to a non-Root type →
// the "root is type N" guard fires.
func TestWalk_RootTypeMismatch(t *testing.T) {
	p, ft, mtid := walkPlugin(t)
	// Reply to getObject(root) with an Integer object instead of Root.
	ft.recv = [][]byte{
		buildReply(t, mtid+1, codec.MTypeReply, byte(codec.MethodGetObject),
			codec.GroupRoot, 0, integerObject(0, "NotRoot")),
	}
	if _, err := p.Walk(context.Background(), 0); err == nil {
		t.Fatal("root type mismatch: want error")
	}
}

// TestWalk_SubGroupMarkersAndEmptyLabel drives the Walk loop's sub-group
// section handling (named open, NO_SUB_GROUP close, nested leaf) plus the
// empty-label leaf fallback.
func TestWalk_SubGroupMarkersAndEmptyLabel(t *testing.T) {
	p, ft, mtid := walkPlugin(t)
	// root: 4 control objects, nothing else.
	ft.recv = [][]byte{
		buildReply(t, mtid+1, codec.MTypeReply, byte(codec.MethodGetObject), codec.GroupRoot, 0, rootObject(0, 4, 0, 0)),
		// id 0: named section marker (String label leading space) → opens "DOWN CONV".
		buildReply(t, mtid+2, codec.MTypeReply, byte(codec.MethodGetObject), codec.GroupControl, 0, stringObject("x", " DOWN CONV")),
		// id 1: a normal object inside the section → nested path.
		buildReply(t, mtid+3, codec.MTypeReply, byte(codec.MethodGetObject), codec.GroupControl, 1, integerObject(5, "Level")),
		// id 2: NO_SUB_GROUP marker (single-space String label) → closes section.
		buildReply(t, mtid+4, codec.MTypeReply, byte(codec.MethodGetObject), codec.GroupControl, 2, stringObject("x", " ")),
		// id 3: an object with an empty label → leaf "#3" fallback.
		buildReply(t, mtid+5, codec.MTypeReply, byte(codec.MethodGetObject), codec.GroupControl, 3, integerObject(9, "")),
	}
	objs, err := p.Walk(context.Background(), 0)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(objs) != 4 {
		t.Fatalf("walked %d objects, want 4", len(objs))
	}
	// The nested object must carry a 3-element path [control, DOWN CONV, Level].
	var nested bool
	for _, o := range objs {
		if o.Label == "Level" && len(o.Path) == 3 {
			nested = true
		}
	}
	if !nested {
		t.Error("expected nested path under DOWN CONV section")
	}
}
