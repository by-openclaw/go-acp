package acp2

import (
	"container/list"
	"sync"
	"time"
)

// walkedTreeCache is a bounded LRU + TTL cache for walked tree entries.
// Same pattern as acp1.slotTreeCache — see acp1/cache.go for design notes.
//
// All operations are mutex-protected. The plugin can release its own lock
// during network I/O without risking concurrent cache mutation.
type walkedTreeCache struct {
	mu      sync.RWMutex
	entries map[int]*list.Element // slot → list element
	order   *list.List            // MRU→LRU
	maxSize int
	ttl     time.Duration
}

type treeCacheEntry struct {
	slot    int
	tree    *WalkedTree
	addedAt time.Time
}

func newWalkedTreeCache(maxSize int, ttl time.Duration) *walkedTreeCache {
	return &walkedTreeCache{
		entries: make(map[int]*list.Element),
		order:   list.New(),
		maxSize: maxSize,
		ttl:     ttl,
	}
}

// Get returns the tree for slot if cached and not expired.
func (c *walkedTreeCache) Get(slot int) (*WalkedTree, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.entries[slot]
	if !ok {
		return nil, false
	}
	e := el.Value.(*treeCacheEntry)

	if c.ttl > 0 && time.Since(e.addedAt) > c.ttl {
		c.removeElement(el)
		return nil, false
	}
	c.order.MoveToFront(el)
	return e.tree, true
}

// Put inserts or updates the entry for slot.
func (c *walkedTreeCache) Put(slot int, tree *WalkedTree) {
	if c == nil || tree == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if el, ok := c.entries[slot]; ok {
		e := el.Value.(*treeCacheEntry)
		e.tree = tree
		e.addedAt = time.Now()
		c.order.MoveToFront(el)
		return
	}

	e := &treeCacheEntry{slot: slot, tree: tree, addedAt: time.Now()}
	el := c.order.PushFront(e)
	c.entries[slot] = el

	for c.maxSize > 0 && c.order.Len() > c.maxSize {
		back := c.order.Back()
		if back == nil {
			break
		}
		c.removeElement(back)
	}
}

// Clear drops every entry.
func (c *walkedTreeCache) Clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[int]*list.Element)
	c.order.Init()
}

func (c *walkedTreeCache) removeElement(el *list.Element) {
	e := el.Value.(*treeCacheEntry)
	c.order.Remove(el)
	delete(c.entries, e.slot)
}
