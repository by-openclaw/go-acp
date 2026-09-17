package registry

import (
	"fmt"
	"testing"

	"dhs/internal/amwa/codec/is04"
)

// The operator's default-page-size lever exists because first-page-only
// controllers are real (Cerebrum's Network Media reader takes page one
// per collection and stops): on a plant where one device owns 208
// senders, everything registered earlier silently vanishes from such a
// controller. Three rules pinned here: the override applies when the
// client sends no paging.limit; an explicit client limit always wins;
// and clearing the override restores the spec-parity default.
func TestDefaultPageLimitOverride(t *testing.T) {
	s := NewStore()
	for i := 0; i < 5; i++ {
		n := validNode(fmt.Sprintf("f47ac10b-58cc-4372-a567-0e02b2c3d4%02d", i))
		if err := s.PutNode(n); err != nil {
			t.Fatalf("put node %d: %v", i, err)
		}
	}

	nodesOf := func(r PageResult) int { return len(r.Items.([]is04.Node)) }

	// No client limit, no override → spec default (all 5 fit).
	res := s.ListPaged(is04.ResourceNode, PageOptions{})
	if res.Limit != DefaultPageLimit || nodesOf(res) != 5 {
		t.Fatalf("baseline: limit=%d items=%d, want limit=%d items=5", res.Limit, nodesOf(res), DefaultPageLimit)
	}

	// Override 2 → no-limit requests now page at 2.
	s.SetDefaultPageLimit(2)
	res = s.ListPaged(is04.ResourceNode, PageOptions{})
	if res.Limit != 2 || nodesOf(res) != 2 {
		t.Errorf("override: limit=%d items=%d, want 2/2", res.Limit, nodesOf(res))
	}

	// An explicit client limit beats the override.
	res = s.ListPaged(is04.ResourceNode, PageOptions{Limit: 4})
	if res.Limit != 4 || nodesOf(res) != 4 {
		t.Errorf("client limit: limit=%d items=%d, want 4/4", res.Limit, nodesOf(res))
	}

	// Override above MaxPageLimit clamps like any other limit.
	s.SetDefaultPageLimit(MaxPageLimit + 500)
	res = s.ListPaged(is04.ResourceNode, PageOptions{})
	if res.Limit != MaxPageLimit {
		t.Errorf("clamp: limit=%d, want %d", res.Limit, MaxPageLimit)
	}

	// Clearing restores the spec default.
	s.SetDefaultPageLimit(0)
	res = s.ListPaged(is04.ResourceNode, PageOptions{})
	if res.Limit != DefaultPageLimit {
		t.Errorf("clear: limit=%d, want %d", res.Limit, DefaultPageLimit)
	}
}
