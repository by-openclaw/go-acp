package acp2

import (
	"testing"
	"time"
)

func newTestTree(slot int) *WalkedTree {
	return &WalkedTree{Slot: slot, Labels: map[string]int{}}
}

// TestCache_PutEvictsLRU fills past the bound and asserts the least-recently
// used entry is evicted via removeElement, while a touched entry survives.
func TestCache_PutEvictsLRU(t *testing.T) {
	const max = 4
	c := newWalkedTreeCache(max, 0)

	for slot := 0; slot < max; slot++ {
		c.Put(slot, newTestTree(slot))
	}
	// Touch slot 0 so it becomes MRU and is NOT the eviction victim.
	if _, ok := c.Get(0); !ok {
		t.Fatal("slot 0 should be present before eviction")
	}
	// Insert one past the bound → evicts the LRU (slot 1).
	c.Put(max, newTestTree(max))

	if c.order.Len() != max {
		t.Fatalf("order len = %d, want %d after eviction", c.order.Len(), max)
	}
	if _, ok := c.Get(1); ok {
		t.Error("slot 1 should have been evicted as LRU")
	}
	if _, ok := c.Get(0); !ok {
		t.Error("slot 0 was touched and must survive eviction")
	}
	if _, ok := c.Get(max); !ok {
		t.Error("newest slot must be present")
	}
}

// TestCache_PutUpdatesExisting replaces an existing slot's tree without
// growing the cache.
func TestCache_PutUpdatesExisting(t *testing.T) {
	c := newWalkedTreeCache(4, 0)
	c.Put(1, newTestTree(1))
	updated := newTestTree(1)
	updated.Labels["x"] = 9
	c.Put(1, updated)

	if c.order.Len() != 1 {
		t.Fatalf("order len = %d, want 1", c.order.Len())
	}
	got, ok := c.Get(1)
	if !ok || got.Labels["x"] != 9 {
		t.Fatalf("Get(1) did not return updated tree: %v", got)
	}
}

// TestCache_Clear drops every entry and leaves the cache reusable.
func TestCache_Clear(t *testing.T) {
	c := newWalkedTreeCache(4, 0)
	c.Put(1, newTestTree(1))
	c.Put(2, newTestTree(2))
	c.Clear()

	if c.order.Len() != 0 {
		t.Fatalf("order len = %d, want 0 after Clear", c.order.Len())
	}
	if _, ok := c.Get(1); ok {
		t.Error("entry survived Clear")
	}
	// Still usable after clear.
	c.Put(3, newTestTree(3))
	if _, ok := c.Get(3); !ok {
		t.Error("cache not usable after Clear")
	}
}

// TestCache_TTLExpiryRemoves exercises the TTL branch in Get, which calls
// removeElement on an expired entry.
func TestCache_TTLExpiryRemoves(t *testing.T) {
	c := newWalkedTreeCache(4, time.Nanosecond)
	c.Put(1, newTestTree(1))
	// Force the addedAt into the past so any positive TTL is exceeded.
	el := c.entries[1]
	el.Value.(*treeCacheEntry).addedAt = time.Now().Add(-time.Hour)

	if _, ok := c.Get(1); ok {
		t.Error("expired entry should miss")
	}
	if c.order.Len() != 0 {
		t.Errorf("expired entry not removed: order len = %d", c.order.Len())
	}
}

// TestCache_NilReceiverSafe pins the nil-guard fast paths.
func TestCache_NilReceiverSafe(t *testing.T) {
	var c *walkedTreeCache
	if _, ok := c.Get(1); ok {
		t.Error("nil cache Get should miss")
	}
	c.Put(1, newTestTree(1)) // must not panic
	c.Clear()                // must not panic
}
