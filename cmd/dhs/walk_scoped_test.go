package main

import (
	"context"
	"errors"
	"testing"

	"dhs/internal/consumer"
)

// scopeWalker is a connector that can read one branch. The embedded
// interface is nil on purpose: any method this test does not name is a
// method walkScoped must not call.
type scopeWalker struct {
	consumer.Protocol
	under      map[string][]consumer.Object
	underErr   error
	fullCalls  int
	underCalls []string
}

func (s *scopeWalker) Walk(ctx context.Context, slot int) ([]consumer.Object, error) {
	s.fullCalls++
	return []consumer.Object{obj("everything")}, nil
}

func (s *scopeWalker) WalkUnder(ctx context.Context, path string) ([]consumer.Object, error) {
	s.underCalls = append(s.underCalls, path)
	if s.underErr != nil {
		return nil, s.underErr
	}
	return s.under[path], nil
}

// slotWalker is a connector that only knows how to walk a whole slot —
// ACP1, ACP2 and Ember+ today.
type slotWalker struct {
	consumer.Protocol
	calls int
}

func (s *slotWalker) Walk(ctx context.Context, slot int) ([]consumer.Object, error) {
	s.calls++
	return []consumer.Object{obj("a"), obj("b")}, nil
}

func obj(path ...string) consumer.Object {
	return consumer.Object{Path: path, Label: path[len(path)-1]}
}

func TestWalkScopedReadsOnlyTheNamedBranches(t *testing.T) {
	w := &scopeWalker{under: map[string][]consumer.Object{
		"system":       {obj("system", "sysDescr")},
		"ateme.Status": {obj("ateme", "Status", "Sat")},
	}}
	objs, scoped, err := walkScoped(context.Background(), w, 0, []string{"system", "ateme.Status"})
	if err != nil {
		t.Fatalf("walkScoped: %v", err)
	}
	if !scoped {
		t.Error("a scoped walk reported itself as a full one")
	}
	if w.fullCalls != 0 {
		t.Errorf("the whole slot was walked anyway (%d times)", w.fullCalls)
	}
	if got := len(w.underCalls); got != 2 {
		t.Errorf("branches read = %d, want 2 (%v)", got, w.underCalls)
	}
	if len(objs) != 2 {
		t.Fatalf("objects = %d, want 2", len(objs))
	}
}

func TestWalkScopedKeepsAnOverlappingObjectOnce(t *testing.T) {
	shared := obj("ateme", "Status", "Sat")
	w := &scopeWalker{under: map[string][]consumer.Object{
		"ateme.Status":       {shared, obj("ateme", "Status", "Lock")},
		"ateme.Status.Input": {shared},
	}}
	objs, _, err := walkScoped(context.Background(), w, 0, []string{"ateme.Status", "ateme.Status.Input"})
	if err != nil {
		t.Fatalf("walkScoped: %v", err)
	}
	if len(objs) != 2 {
		t.Fatalf("objects = %d, want 2 — the overlap was read twice", len(objs))
	}
}

func TestWalkScopedFallsBackWhenTheConnectorCannotScope(t *testing.T) {
	s := &slotWalker{}
	objs, scoped, err := walkScoped(context.Background(), s, 3, []string{"BOARD"})
	if err != nil {
		t.Fatalf("walkScoped: %v", err)
	}
	if scoped {
		t.Error("reported as scoped on a connector that walks slots")
	}
	if s.calls != 1 || len(objs) != 2 {
		t.Errorf("calls=%d objects=%d, want one full walk", s.calls, len(objs))
	}
}

func TestWalkScopedWithoutAPathWalksTheSlot(t *testing.T) {
	w := &scopeWalker{}
	_, scoped, err := walkScoped(context.Background(), w, 0, nil)
	if err != nil {
		t.Fatalf("walkScoped: %v", err)
	}
	if scoped || w.fullCalls != 1 || len(w.underCalls) != 0 {
		t.Errorf("no --path should read the slot: scoped=%v full=%d under=%v", scoped, w.fullCalls, w.underCalls)
	}
}

func TestWalkScopedNamesTheBranchThatFailed(t *testing.T) {
	w := &scopeWalker{underErr: errors.New("no such branch")}
	_, _, err := walkScoped(context.Background(), w, 0, []string{"nope"})
	if err == nil {
		t.Fatal("a branch that does not exist walked fine")
	}
	if got := err.Error(); got != "walk nope: no such branch" {
		t.Errorf("error = %q", got)
	}
}

func TestParsePathScopes(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"BOARD", []string{"BOARD"}},
		{"system,ateme.Status.Input", []string{"system", "ateme.Status.Input"}},
		{" system , ateme.Status ", []string{"system", "ateme.Status"}},
		{"system,,", []string{"system"}},
	}
	for _, c := range cases {
		got := parsePathScopes(c.in)
		if len(got) != len(c.want) {
			t.Fatalf("%q → %v, want %v", c.in, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%q → %v, want %v", c.in, got, c.want)
			}
		}
	}
}

func TestPathScopeSegmentsSplitsOnDots(t *testing.T) {
	segs := pathScopeSegments([]string{"system", "ateme.Status.Input"})
	if len(segs) != 2 || len(segs[0]) != 1 || len(segs[1]) != 3 {
		t.Fatalf("segments = %v", segs)
	}
	if segs[1][2] != "Input" {
		t.Errorf("last segment = %q", segs[1][2])
	}
}

func TestFilterByPathsMatchesAnyPrefix(t *testing.T) {
	objs := []consumer.Object{
		obj("system", "sysDescr"),
		obj("ateme", "Status", "Sat"),
		obj("ateme", "Channel", "Output"),
	}
	prefixes := pathScopeSegments([]string{"system", "ateme.Status"})
	got := filterByPaths(objs, prefixes)
	if len(got) != 2 {
		t.Fatalf("kept %d objects, want 2: %v", len(got), got)
	}
	if all := filterByPaths(objs, nil); len(all) != 3 {
		t.Errorf("no prefixes should keep everything, kept %d", len(all))
	}
	if none := filterByPaths(objs, pathScopeSegments([]string{"nope"})); len(none) != 0 {
		t.Errorf("an unmatched prefix kept %d objects", len(none))
	}
}
