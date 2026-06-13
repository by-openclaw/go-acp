package acp1

import (
	"errors"
	"testing"

	"dhs/internal/acp1/codec"
	"dhs/internal/consumer"
)

// mkTree builds a minimal SlotTree from objects for resolve() tests,
// populating the Labels index so Lookup() works.
func mkTree(objs ...consumer.Object) *SlotTree {
	t := &SlotTree{Objects: objs, Labels: map[string]map[string]int{}}
	for i, o := range objs {
		if t.Labels[o.Group] == nil {
			t.Labels[o.Group] = map[string]int{}
		}
		t.Labels[o.Group][o.Label] = i
	}
	return t
}

// TestResolve_ColdCacheExplicitPair covers the cold-cache fall-through
// where Label is set, tree is nil, but an explicit (Group, ID) is given.
func TestResolve_ColdCacheExplicitPair(t *testing.T) {
	g, id, err := resolve(consumer.ValueRequest{
		Label: "Gain", Group: "control", ID: 7,
	}, nil)
	if err != nil {
		t.Fatalf("cold-cache pair: %v", err)
	}
	if g != codec.GroupControl || id != 7 {
		t.Fatalf("got group=%v id=%d", g, id)
	}

	// Bad explicit group in cold-cache → falls past, then errors (no tree).
	if _, _, err := resolve(consumer.ValueRequest{
		Label: "Gain", Group: "bogus", ID: 7,
	}, nil); !errors.Is(err, consumer.ErrUnknownLabel) {
		t.Fatalf("bad group cold-cache: want ErrUnknownLabel, got %v", err)
	}

	// Label set, tree nil, no usable explicit pair → ErrUnknownLabel.
	if _, _, err := resolve(consumer.ValueRequest{
		Label: "Gain", ID: -1,
	}, nil); !errors.Is(err, consumer.ErrUnknownLabel) {
		t.Fatalf("no tree: want ErrUnknownLabel, got %v", err)
	}
}

// TestResolve_DeclaredGroupHitAndBadTreeGroup covers the primary-group hit
// plus the defensive bad-group-in-tree guard.
func TestResolve_DeclaredGroupHitAndBadTreeGroup(t *testing.T) {
	tree := mkTree(consumer.Object{Group: "control", ID: 7, Label: "Gain"})
	g, id, err := resolve(consumer.ValueRequest{Group: "control", Label: "Gain"}, tree)
	if err != nil || g != codec.GroupControl || id != 7 {
		t.Fatalf("declared hit: g=%v id=%d err=%v", g, id, err)
	}

	// Corrupted tree (declared-group path): Labels indexes the requested
	// group "control" → object 0, but the object's stored Group is an
	// unparseable token, so ParseGroup(obj.Group) fails (line ~912).
	badDeclared := &SlotTree{
		Objects: []consumer.Object{{Group: "not-a-real-group", ID: 1, Label: "X"}},
		Labels:  map[string]map[string]int{"control": {"X": 0}},
	}
	if _, _, err := resolve(consumer.ValueRequest{Group: "control", Label: "X"}, badDeclared); err == nil {
		t.Fatal("declared bad-tree-group: want error")
	}

	// Corrupted tree (any-group path): Labels indexes "identity" → object
	// 0 whose stored Group is unparseable, hitting the guard in the
	// no-group search loop (line ~923).
	badAny := &SlotTree{
		Objects: []consumer.Object{{Group: "not-a-real-group", ID: 0, Label: "Y"}},
		Labels:  map[string]map[string]int{"identity": {"Y": 0}},
	}
	if _, _, err := resolve(consumer.ValueRequest{Label: "Y"}, badAny); err == nil {
		t.Fatal("any-group bad-tree-group: want error")
	}
}

// TestResolve_DidYouMeanAndAnyGroup covers the no-group all-group search,
// the "did you mean" cross-group hint, and the not-found-anywhere error.
func TestResolve_DidYouMeanAndAnyGroup(t *testing.T) {
	tree := mkTree(consumer.Object{Group: "status", ID: 6, Label: "Temp"})

	// No group given → search all four, find in status.
	g, id, err := resolve(consumer.ValueRequest{Label: "Temp"}, tree)
	if err != nil || g != codec.GroupStatus || id != 6 {
		t.Fatalf("any-group: g=%v id=%d err=%v", g, id, err)
	}

	// Declared group "control" misses, but label exists in "status" → hint.
	_, _, err = resolve(consumer.ValueRequest{Group: "control", Label: "Temp"}, tree)
	if !errors.Is(err, consumer.ErrUnknownLabel) {
		t.Fatalf("did-you-mean: want ErrUnknownLabel, got %v", err)
	}

	// Not found anywhere.
	_, _, err = resolve(consumer.ValueRequest{Label: "Nonexistent"}, tree)
	if !errors.Is(err, consumer.ErrUnknownLabel) {
		t.Fatalf("not-found: want ErrUnknownLabel, got %v", err)
	}
}
