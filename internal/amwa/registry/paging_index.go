package registry

// The order a Query page is cut from.
//
// IS-04 pages a collection by update_ts, newest first, between the
// cursors a client carries in paging.since / paging.until. The page was
// cut by walking every id of the type, comparing TAI strings and
// sorting every candidate per request — a page cost the collection's
// size (issue #1285; at the 65 535-resource target that is most of a
// second per page, and a first-page-only controller pays it for every
// collection).
//
// Each type now has an index: its ids ordered by (update_ts, id) with
// the timestamp parsed once, rebuilt lazily after the type changed. A
// page is a binary search for the cursor window plus the page itself;
// a filtered page scans the window from its anchored end and stops at
// the limit. The semantics are the ones the AMWA tests pin: newest
// first in the body, since-anchored pages ascending from the cursor,
// until-anchored pages descending from it, ties broken by id.

import (
	"sort"
	"sync"
	"sync/atomic"

	"dhs/internal/amwa/codec/is04"
)

// newPageIndexes is one index per type, all stale until first used.
func newPageIndexes() map[is04.ResourceType]*pageIndex {
	out := make(map[is04.ResourceType]*pageIndex, len(is04.AllResourceTypes))
	for _, t := range is04.AllResourceTypes {
		idx := &pageIndex{}
		idx.dirty.Store(true)
		out[t] = idx
	}
	return out
}

// pageEntry is one resource in update_ts order.
type pageEntry struct {
	sec, nsec int64
	ts        string
	id        string
}

// pageIndex is a type's entries sorted ascending by (sec, nsec, id).
// dirty means the type changed since it was built; entries is replaced
// whole on a rebuild, so a slice a reader took stays what it was.
type pageIndex struct {
	mu      sync.Mutex // the rebuild
	dirty   atomic.Bool
	entries []pageEntry
}

// less orders entries ascending by timestamp, then id.
func (e pageEntry) less(o pageEntry) bool {
	if e.sec != o.sec {
		return e.sec < o.sec
	}
	if e.nsec != o.nsec {
		return e.nsec < o.nsec
	}
	return e.id < o.id
}

// after reports whether the entry's timestamp is strictly after (sec, nsec).
func (e pageEntry) after(sec, nsec int64) bool {
	return e.sec > sec || (e.sec == sec && e.nsec > nsec)
}

// pageEntriesLocked is the type's order, rebuilt if the type changed.
// Caller MUST hold the store's read or write lock: the rebuild reads
// the update_ts bucket, which only a writer (under the write lock)
// changes, and marks the index clean under the index's own lock.
func (s *Store) pageEntriesLocked(t is04.ResourceType) []pageEntry {
	idx := s.pageIdx[t]
	idx.mu.Lock()
	defer idx.mu.Unlock()
	if idx.dirty.Swap(false) {
		bucket := s.updateTSByType[t]
		entries := make([]pageEntry, 0, len(bucket))
		for id, ts := range bucket {
			sec, nsec := splitTAI(ts)
			entries = append(entries, pageEntry{sec: sec, nsec: nsec, ts: ts, id: id})
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].less(entries[j]) })
		idx.entries = entries
	}
	return idx.entries
}

// touchPageIndex marks a type's order stale. Caller MUST hold the
// write lock (it is called where update_ts changes).
func (s *Store) touchPageIndex(t is04.ResourceType) {
	s.pageIdx[t].dirty.Store(true)
}

// pageWindow is the index range [lo, hi) of entries with
// since < ts <= until: the half-open cursor window IS-04 pages over.
// Both bounds are found by binary search.
func pageWindow(entries []pageEntry, since, until string) (lo, hi int) {
	ss, sn := splitTAI(since)
	us, un := splitTAI(until)
	lo = sort.Search(len(entries), func(i int) bool { return entries[i].after(ss, sn) })
	hi = sort.Search(len(entries), func(i int) bool { return entries[i].after(us, un) })
	if hi < lo {
		hi = lo
	}
	return lo, hi
}
