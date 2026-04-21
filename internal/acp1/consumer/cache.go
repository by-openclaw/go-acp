package acp1

import (
	"container/list"
	"sync"
	"time"

	"acp/internal/protocol"
)

// slotTreeCache is a bounded LRU + TTL cache for walked SlotTree
// entries. It replaces the bare map[int]*SlotTree the Plugin used
// earlier so long-running processes (acp watch, future acp-srv) don't
// accumulate stale trees forever.
//
// Storage strategy:
//   - entries: slot → cacheEntry index for O(1) lookup
//   - order:   doubly-linked list with MRU at Front, LRU at Back
//
// Eviction happens on Put when len(order) > maxSize. TTL is lazy —
// expired entries are only removed on Get to avoid a background
// goroutine (one less thing to Stop cleanly on Disconnect).
//
// All operations take the instance mutex — the plugin serialises its
// cache access anyway, so contention is nil in practice. Keeping the
// mutex on the cache itself means the plugin can release its own lock
// during network I/O without risking concurrent cache mutation.
//
// The zero cache is NOT ready for use — call newSlotTreeCache.
type slotTreeCache struct {
	mu      sync.Mutex
	entries map[int]*list.Element // slot → list element pointing at cacheEntry
	order   *list.List            // MRU→LRU; element.Value is *cacheEntry
	maxSize int                   // 0 = unbounded (not recommended)
	ttl     time.Duration         // 0 = no expiry
}

// cacheEntry is what each list element holds. Keeping the slot id in
// the entry lets us delete from the map when the list drops it.
type cacheEntry struct {
	slot    int
	tree    *SlotTree
	addedAt time.Time
}

// newSlotTreeCache returns an empty cache with the given size and TTL
// limits. maxSize <= 0 disables the LRU bound; ttl <= 0 disables the
// expiry check.
func newSlotTreeCache(maxSize int, ttl time.Duration) *slotTreeCache {
	return &slotTreeCache{
		entries: make(map[int]*list.Element),
		order:   list.New(),
		maxSize: maxSize,
		ttl:     ttl,
	}
}

// Get returns the tree for slot if cached and not expired, plus a
// found flag. An expired entry is removed as a side effect so callers
// don't see it again.
func (c *slotTreeCache) Get(slot int) (*SlotTree, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.entries[slot]
	if !ok {
		return nil, false
	}
	e := el.Value.(*cacheEntry)

	if c.ttl > 0 && time.Since(e.addedAt) > c.ttl {
		c.removeElement(el)
		return nil, false
	}
	// Touch: move to MRU end.
	c.order.MoveToFront(el)
	return e.tree, true
}

// Put inserts or updates the entry for slot. If the cache is over
// capacity after insertion, the LRU entry is evicted.
func (c *slotTreeCache) Put(slot int, tree *SlotTree) {
	if c == nil || tree == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if el, ok := c.entries[slot]; ok {
		e := el.Value.(*cacheEntry)
		e.tree = tree
		e.addedAt = time.Now()
		c.order.MoveToFront(el)
		return
	}

	e := &cacheEntry{slot: slot, tree: tree, addedAt: time.Now()}
	el := c.order.PushFront(e)
	c.entries[slot] = el

	// Shrink to maxSize by dropping the LRU. Loop because the caller
	// could have inserted many entries before we got the lock.
	for c.maxSize > 0 && c.order.Len() > c.maxSize {
		back := c.order.Back()
		if back == nil {
			break
		}
		c.removeElement(back)
	}
}

// Delete removes the cached entry for slot, if any.
func (c *slotTreeCache) Delete(slot int) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.entries[slot]; ok {
		c.removeElement(el)
	}
}

// Clear drops every entry. Called from Plugin.Disconnect.
func (c *slotTreeCache) Clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[int]*list.Element)
	c.order.Init()
}

// Len returns the current entry count. Used by tests and diagnostics.
func (c *slotTreeCache) Len() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.order.Len()
}

// UpdateObjectValue writes a fresh live value into the cached tree for
// an object identified by (slot, group, id). Called by the Plugin's
// announcement wrapper so live event values overwrite the snapshot
// captured at walk time — keeps walk output and cache readers in sync
// with whatever the device last told us.
//
// No-op when the slot isn't cached, the group is unknown, or the object
// wasn't walked. Takes the cache mutex so concurrent Gets and Puts stay
// consistent.
func (c *slotTreeCache) UpdateObjectValue(slot int, group string, id int, val protocol.Value) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.entries[slot]
	if !ok {
		return
	}
	e := el.Value.(*cacheEntry)
	for i := range e.tree.Objects {
		obj := &e.tree.Objects[i]
		if obj.Group == group && obj.ID == id {
			obj.Value = val
			return
		}
	}
}


// removeElement deletes an entry from both the order list and the
// lookup map. Must be called with c.mu held.
func (c *slotTreeCache) removeElement(el *list.Element) {
	e := el.Value.(*cacheEntry)
	c.order.Remove(el)
	delete(c.entries, e.slot)
}

// CacheConfig exposes the two knobs that matter for long-running
// processes. Defaults are sized for the ACP1 tool: 32 slots is enough
// for a fully-loaded Synapse rack with room for retries, and a 10-minute
// TTL keeps a watch session fresh enough for interactive use without
// re-walking on every get.
type CacheConfig struct {
	MaxSize int           // default 32
	TTL     time.Duration // default 10m
}

// defaultCacheConfig returns the sane-default cache sizing.
func defaultCacheConfig() CacheConfig {
	return CacheConfig{
		MaxSize: 32,
		TTL:     10 * time.Minute,
	}
}
