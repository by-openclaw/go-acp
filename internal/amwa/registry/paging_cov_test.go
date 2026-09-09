package registry

import (
	"fmt"
	"testing"

	"dhs/internal/amwa/codec/is04"
)

// TAI timestamps are compared as two integers, never as strings: "9:0"
// is older than "10:0" even though it sorts after it. A string the
// registry cannot parse reads as the epoch rather than failing the
// comparison.
func TestTAIComparisonAndBump(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"9:0", "10:0", -1},
		{"10:0", "9:0", 1},
		{"10:0", "10:0", 0},
		{"10:1", "10:2", -1},
		{"10:2", "10:1", 1},
		{"", "0:0", 0},             // empty reads as the epoch
		{"nonsense", "0:0", 0},     // unparseable seconds read as 0
		{"10", "10:0", 0},          // seconds without nanos
		{"10:nonsense", "10:0", 0}, // unparseable nanos read as 0
		{taiBeforeAll, "1:0", -1},  // the harness's bottom cursor
	} {
		if got := taiCmp(tc.a, tc.b); got != tc.want {
			t.Errorf("taiCmp(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}

	for from, want := range map[string]string{
		"1:5":         "1:6",
		"1:999999999": "2:0", // the nanosecond field wraps into seconds
		"":            "0:1",
	} {
		if got := taiBump(from); got != want {
			t.Errorf("taiBump(%q) = %q, want %q", from, got, want)
		}
	}
}

// collectTyped hands the encoder a homogeneous typed slice per
// resource kind, drops anything of the wrong type, and passes an
// unknown kind through untouched.
func TestCollectTypedPerType(t *testing.T) {
	wrong := "not a resource"
	for _, tc := range []struct {
		rt   is04.ResourceType
		item any
		len  func(any) int
	}{
		{is04.ResourceNode, validNode(fxNode), func(v any) int { return len(v.([]is04.Node)) }},
		{is04.ResourceDevice, validDevice(fxDevice, fxNode), func(v any) int { return len(v.([]is04.Device)) }},
		{is04.ResourceSource, validSource(fxSource, fxDevice), func(v any) int { return len(v.([]is04.Source)) }},
		{is04.ResourceFlow, validFlow(fxFlow, fxSource, fxDevice), func(v any) int { return len(v.([]is04.Flow)) }},
		{is04.ResourceSender, validSender(fxSender, fxDevice), func(v any) int { return len(v.([]is04.Sender)) }},
		{is04.ResourceReceiver, validReceiver(fxReceiver, fxDevice), func(v any) int { return len(v.([]is04.Receiver)) }},
	} {
		if n := tc.len(collectTyped(tc.rt, []any{tc.item, wrong})); n != 1 {
			t.Errorf("collectTyped(%s) kept %d items, want the one of its own type", tc.rt, n)
		}
		if n := tc.len(collectTyped(tc.rt, nil)); n != 0 {
			t.Errorf("collectTyped(%s, nil) = %d items, want an empty typed slice", tc.rt, n)
		}
	}

	items := []any{wrong}
	if got := collectTyped(is04.ResourceType("gizmo"), items); len(got.([]any)) != 1 {
		t.Errorf("an unknown type passes its items through: %v", got)
	}
}

// ParentsIndex snapshots `parents` for the two kinds that carry it,
// and reports nil — "ancestry undefined here" — for every other kind.
func TestParentsIndexOnlyForSourcesAndFlows(t *testing.T) {
	s := populated(t)
	src := validSource(fxSource, fxDevice)
	src.Parents = []string{fxAbsent}
	if err := s.PutSource(src); err != nil {
		t.Fatal(err)
	}

	idx := s.ParentsIndex(is04.ResourceSource)
	if len(idx[fxSource]) != 1 || idx[fxSource][0] != fxAbsent {
		t.Errorf("source parents = %v", idx[fxSource])
	}
	// The snapshot is a copy: writing to it cannot reach the store.
	idx[fxSource][0] = "tampered"
	if got := s.ParentsIndex(is04.ResourceSource)[fxSource][0]; got != fxAbsent {
		t.Errorf("the index is not a snapshot: %q", got)
	}

	if idx := s.ParentsIndex(is04.ResourceFlow); len(idx) != 1 {
		t.Errorf("flow index = %v, want the one Flow", idx)
	}
	for _, rt := range []is04.ResourceType{
		is04.ResourceNode, is04.ResourceDevice, is04.ResourceSender, is04.ResourceReceiver,
	} {
		if idx := s.ParentsIndex(rt); idx != nil {
			t.Errorf("ParentsIndex(%s) = %v, want nil (ancestry undefined)", rt, idx)
		}
	}
}

// The traversal's edges, beyond the straight chain ancestry_test.go
// walks: a resource reachable along two paths is emitted once, a
// parent nobody registered is not a result, and an index the registry
// does not keep — or a direction it does not define — selects nothing.
func TestAncestrySetEdgeCases(t *testing.T) {
	const (
		grand   = "10000000-0000-4000-8000-000000000001"
		parentA = "10000000-0000-4000-8000-000000000002"
		parentB = "10000000-0000-4000-8000-000000000003"
		child   = "10000000-0000-4000-8000-000000000004"
	)
	// A diamond: the child has two parents, and both name the same
	// grandparent, so each walk reaches one id along two paths.
	index := map[string][]string{
		grand:   {},
		parentA: {grand},
		parentB: {grand},
		child:   {parentA, parentB},
	}

	up := ancestrySet(index, child, ancestryParents, 0)
	if len(up) != 3 || !up[parentA] || !up[parentB] || !up[grand] {
		t.Errorf("parents walk = %v, want both parents and the one grandparent", up)
	}
	down := ancestrySet(index, grand, ancestryChildren, 0)
	if len(down) != 3 || !down[child] {
		t.Errorf("children walk = %v, want both parents and the one child", down)
	}

	if got := ancestrySet(map[string][]string{child: {fxAbsent}}, child, ancestryParents, 0); len(got) != 0 {
		t.Errorf("a dangling parent reference selected %v", got)
	}
	if got := ancestrySet(nil, child, ancestryParents, 0); len(got) != 0 {
		t.Errorf("an index the registry does not keep selected %v", got)
	}
	if got := ancestrySet(index, child, "cousins", 0); len(got) != 0 {
		t.Errorf("an undefined direction selected %v", got)
	}
}

// pagedNodes fills a store with n Nodes, each one update_ts later than
// the last, and returns their ids oldest-first.
func pagedNodes(t *testing.T, s *Store, n int) []string {
	t.Helper()
	ids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("20000000-0000-4000-8000-%012d", i)
		if err := s.PutNode(validNode(id)); err != nil {
			t.Fatalf("put %d: %v", i, err)
		}
		ids = append(ids, id)
	}
	return ids
}

// Which cursor the client pinned decides the page direction: an
// explicit paging.since walks up from the cursor, anything else walks
// down from the head. The body is newest-first either way.
func TestListPagedCursorDirections(t *testing.T) {
	s := NewStore()
	ids := pagedNodes(t, s, 5)
	nodesOf := func(r PageResult) []is04.Node { return r.Items.([]is04.Node) }
	tsOf := func(id string) string {
		s.mu.RLock()
		defer s.mu.RUnlock()
		return s.updateTSByType[is04.ResourceNode][id]
	}

	// Head-anchored (no cursor): newest first, and the page's Since is
	// the boundary item the next page picks up from.
	page := s.ListPaged(is04.ResourceNode, PageOptions{Limit: 2})
	got := nodesOf(page)
	if len(got) != 2 || got[0].ID != ids[4] || got[1].ID != ids[3] {
		t.Fatalf("head page = %v, want the two newest", idsOf(got))
	}
	if page.Since != tsOf(ids[2]) {
		t.Errorf("page.Since = %q, want the boundary item's update_ts", page.Since)
	}
	if page.Until != tsOf(ids[4]) {
		t.Errorf("page.Until = %q, want the series head", page.Until)
	}

	// Ascending from a cursor, truncated: Until becomes the newest
	// item IN the page.
	page = s.ListPaged(is04.ResourceNode, PageOptions{Since: tsOf(ids[0]), Limit: 2})
	got = nodesOf(page)
	if len(got) != 2 || got[0].ID != ids[2] || got[1].ID != ids[1] {
		t.Fatalf("ascending page = %v, want the two above the cursor", idsOf(got))
	}
	if page.Since != tsOf(ids[0]) || page.Until != tsOf(ids[2]) {
		t.Errorf("ascending cursors = (%q, %q)", page.Since, page.Until)
	}

	// Ascending, not truncated, with an explicit ceiling: the ceiling
	// is echoed back (AMWA test_21_5).
	ceiling := taiBump(tsOf(ids[4]))
	page = s.ListPaged(is04.ResourceNode, PageOptions{Since: tsOf(ids[3]), Until: ceiling, Limit: 10})
	if len(nodesOf(page)) != 1 || page.Until != ceiling {
		t.Errorf("explicit ceiling: items=%d until=%q, want 1 and %q", len(nodesOf(page)), page.Until, ceiling)
	}

	// Ascending, not truncated, no ceiling: Until is the newest item.
	page = s.ListPaged(is04.ResourceNode, PageOptions{Since: tsOf(ids[3]), Limit: 10})
	if page.Until != tsOf(ids[4]) {
		t.Errorf("open-ended ascending until = %q, want %q", page.Until, tsOf(ids[4]))
	}

	// Ascending past the head selects nothing, and the page collapses
	// onto the cursor the client supplied.
	beyond := taiBump(tsOf(ids[4]))
	page = s.ListPaged(is04.ResourceNode, PageOptions{Since: beyond, Limit: 10})
	if len(nodesOf(page)) != 0 || page.Since != beyond || page.Until != beyond {
		t.Errorf("page above the head = %d items, cursors (%q, %q)", len(nodesOf(page)), page.Since, page.Until)
	}

	// A predicate filters the page, and the cursors index the filtered
	// series rather than the registry's order.
	page = s.ListPaged(is04.ResourceNode, PageOptions{
		Limit:     10,
		Predicate: func(res any) bool { return res.(is04.Node).ID == ids[1] },
	})
	if got := nodesOf(page); len(got) != 1 || got[0].ID != ids[1] {
		t.Errorf("filtered page = %v, want the one match", idsOf(got))
	}

	// Any limit above the registry's ceiling is clamped.
	page = s.ListPaged(is04.ResourceNode, PageOptions{Limit: MaxPageLimit + 1})
	if page.Limit != MaxPageLimit {
		t.Errorf("clamped limit = %d, want %d", page.Limit, MaxPageLimit)
	}

	// An empty type reports the wall clock as its head rather than an
	// empty cursor.
	page = s.ListPaged(is04.ResourceSender, PageOptions{})
	if page.Until == "" || len(page.Items.([]is04.Sender)) != 0 {
		t.Errorf("empty collection = %+v", page)
	}
}

func idsOf(nodes []is04.Node) []string {
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, n.ID)
	}
	return out
}

// A resource evicted between the index walk and the typed lookup is
// simply absent from the page — the index is pruned on delete, so the
// two agree; this pins that a stale index entry cannot panic.
func TestListPagedSkipsAnEvictedResource(t *testing.T) {
	s := NewStore()
	ids := pagedNodes(t, s, 2)
	s.mu.Lock()
	delete(s.nodes, ids[0]) // index entry left behind on purpose
	s.mu.Unlock()

	page := s.ListPaged(is04.ResourceNode, PageOptions{Limit: 10})
	got := page.Items.([]is04.Node)
	if len(got) != 1 || got[0].ID != ids[1] {
		t.Errorf("page = %v, want only the resource that is still stored", idsOf(got))
	}
}
