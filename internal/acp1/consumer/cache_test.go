package acp1

import (
	"testing"
	"time"
)

// newTree is a tiny helper to build throwaway SlotTree values for cache
// tests. The content doesn't matter — we only exercise the cache
// bookkeeping.
func newTree(slot int) *SlotTree {
	return &SlotTree{Slot: slot}
}

func TestCache_PutGet(t *testing.T) {
	c := newSlotTreeCache(4, 0)
	c.Put(1, newTree(1))
	c.Put(2, newTree(2))

	if got, ok := c.Get(1); !ok || got.Slot != 1 {
		t.Errorf("Get(1): ok=%v got=%+v", ok, got)
	}
	if got, ok := c.Get(2); !ok || got.Slot != 2 {
		t.Errorf("Get(2): ok=%v got=%+v", ok, got)
	}
	if _, ok := c.Get(99); ok {
		t.Error("Get(99) should miss")
	}
	if c.Len() != 2 {
		t.Errorf("Len: got %d, want 2", c.Len())
	}
}

func TestCache_LRUEviction(t *testing.T) {
	c := newSlotTreeCache(3, 0)
	c.Put(1, newTree(1))
	c.Put(2, newTree(2))
	c.Put(3, newTree(3))

	// Touch slot 1 → becomes MRU.
	_, _ = c.Get(1)

	// Insert a 4th — LRU is now slot 2 (oldest untouched) → evicted.
	c.Put(4, newTree(4))

	if c.Len() != 3 {
		t.Errorf("Len: got %d, want 3", c.Len())
	}
	if _, ok := c.Get(2); ok {
		t.Error("slot 2 should have been evicted as LRU")
	}
	// Slot 1 was touched, still there.
	if _, ok := c.Get(1); !ok {
		t.Error("slot 1 was touched and should still be cached")
	}
	if _, ok := c.Get(4); !ok {
		t.Error("slot 4 should be cached")
	}
}

func TestCache_TTLExpiry(t *testing.T) {
	c := newSlotTreeCache(4, 10*time.Millisecond)
	c.Put(1, newTree(1))
	if _, ok := c.Get(1); !ok {
		t.Fatal("fresh entry should be present")
	}
	time.Sleep(15 * time.Millisecond)
	if _, ok := c.Get(1); ok {
		t.Error("entry should have expired after TTL")
	}
	if c.Len() != 0 {
		t.Errorf("expired entry should be pruned; Len=%d", c.Len())
	}
}

func TestCache_PutSameSlotReplaces(t *testing.T) {
	c := newSlotTreeCache(4, 0)
	first := newTree(5)
	second := newTree(5)
	second.BootMode = 1

	c.Put(5, first)
	c.Put(5, second)

	got, ok := c.Get(5)
	if !ok {
		t.Fatal("Get(5) miss after double Put")
	}
	if got.BootMode != 1 {
		t.Error("second Put did not replace first")
	}
	if c.Len() != 1 {
		t.Errorf("Len: got %d, want 1", c.Len())
	}
}

func TestCache_ClearAndDelete(t *testing.T) {
	c := newSlotTreeCache(4, 0)
	c.Put(1, newTree(1))
	c.Put(2, newTree(2))

	c.Delete(1)
	if _, ok := c.Get(1); ok {
		t.Error("Delete(1) left entry behind")
	}
	if c.Len() != 1 {
		t.Errorf("Len: got %d, want 1", c.Len())
	}

	c.Clear()
	if c.Len() != 0 {
		t.Errorf("Clear: Len=%d, want 0", c.Len())
	}
}

func TestCache_NilSafe(t *testing.T) {
	var c *slotTreeCache
	if _, ok := c.Get(1); ok {
		t.Error("nil cache Get should miss")
	}
	c.Put(1, newTree(1)) // should not panic
	c.Delete(1)          // should not panic
	c.Clear()            // should not panic
	if c.Len() != 0 {
		t.Errorf("nil Len: got %d, want 0", c.Len())
	}
}
