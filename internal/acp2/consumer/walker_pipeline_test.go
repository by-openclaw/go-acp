package acp2

import (
	"context"
	"errors"
	"testing"
)

// The pipelined walk's contract is that overlapping the round-trips
// changes only the TIMING, never the result: children are still visited
// in the order the device listed them, and a failure is still reported
// at the point the serial walk would have reported it. These tests pin
// the parts of that contract reachable without a live session; the
// loopback walk suites cover the fetch path itself.

func TestWalkerConcurrencyDefaultAndOverride(t *testing.T) {
	if got := (&Walker{}).concurrency(); got != defaultWalkConcurrency {
		t.Errorf("unset Concurrency = %d, want the default %d", got, defaultWalkConcurrency)
	}
	// 1 is the escape hatch: a device that dislikes being pushed is
	// slowed here, never by changing the transport.
	if got := (&Walker{Concurrency: 1}).concurrency(); got != 1 {
		t.Errorf("Concurrency=1 = %d, want 1 (serial walk)", got)
	}
	if got := (&Walker{Concurrency: 64}).concurrency(); got != 64 {
		t.Errorf("Concurrency=64 = %d, want 64", got)
	}
}

func TestPrefetchChildrenNoChildren(t *testing.T) {
	w := &Walker{logger: testLogger()}
	if got := w.prefetchChildren(context.Background(), 1, nil, nil); len(got) != 0 {
		t.Errorf("prefetch of no children returned %d entries, want 0", len(got))
	}
}

// The in-flight bound is clamped to the number of children, so a wide
// Concurrency never allocates a semaphore larger than the work. Only
// reachable above the serial default, hence the explicit Concurrency.
func TestPrefetchChildrenClampsLimitToChildCount(t *testing.T) {
	w := &Walker{logger: testLogger(), Concurrency: 8}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ids := []uint32{1, 2}
	got := w.prefetchChildren(ctx, 1, ids, nil)
	if len(got) != len(ids) {
		t.Fatalf("got %d entries, want %d", len(got), len(ids))
	}
}

func TestPrefetchChildrenCancelledFillsEveryEntry(t *testing.T) {
	w := &Walker{logger: testLogger()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ids := []uint32{10, 11, 12}
	got := w.prefetchChildren(ctx, 1, ids, nil)
	if len(got) != len(ids) {
		t.Fatalf("got %d entries, want %d — one per child", len(got), len(ids))
	}
	// No holes: the caller must never have to test for a missing entry.
	for i, f := range got {
		if f == nil {
			t.Fatalf("entry %d is nil; a cancelled child must carry its reason", i)
		}
		if !errors.Is(f.err, context.Canceled) {
			t.Errorf("entry %d err = %v, want context.Canceled", i, f.err)
		}
	}
}

func TestPluginSetWalkConcurrency(t *testing.T) {
	p := &Plugin{logger: testLogger()}

	// Before Connect there is no walker, so the choice is remembered and
	// applied to the next session rather than dropped.
	p.SetWalkConcurrency(4)
	if p.walkConcurrency != 4 {
		t.Errorf("remembered concurrency = %d, want 4", p.walkConcurrency)
	}

	// With a live walker it takes effect immediately, so an operator can
	// back a device off without reconnecting.
	p.walker = NewWalker(nil, testLogger())
	p.SetWalkConcurrency(7)
	if p.walker.Concurrency != 7 {
		t.Errorf("live walker concurrency = %d, want 7", p.walker.Concurrency)
	}
	if p.walkConcurrency != 7 {
		t.Errorf("remembered concurrency = %d, want 7", p.walkConcurrency)
	}
}

func TestWalkObjectReportsPrefetchError(t *testing.T) {
	w := &Walker{logger: testLogger()}
	tree := &WalkedTree{Slot: 1, Labels: map[string]int{}}

	sentinel := errors.New("get_object(7): boom")
	err := w.walkObject(context.Background(), 1, 7, nil, tree, &fetched{err: sentinel})
	if !errors.Is(err, sentinel) {
		t.Fatalf("walkObject err = %v, want the prefetch error surfaced verbatim", err)
	}
	// A failed fetch contributes nothing to the tree.
	if len(tree.Objects) != 0 {
		t.Errorf("tree gained %d objects from a failed fetch, want 0", len(tree.Objects))
	}
}
