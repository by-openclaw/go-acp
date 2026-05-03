package registry

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"acp/internal/amwa/codec/is04"
)

// IS-04 §6.1.6 — Query API pagination.
//
// Each registry-stored resource carries an `update_ts` assigned at the
// moment the registration is accepted (or refreshed). It is a TAI
// timestamp string in the canonical NMOS shape "<secs>:<nanos>" — the
// AMWA Testing tool computes its own TAI clock with the same leap-
// second math, then asserts that our X-Paging-Since/X-Paging-Until
// headers fall within the wall-clock window it recorded around each
// POST.
//
// Pagination contract (verified against
// nmostesting/suites/IS0402Test.py do_test_paged_response):
//   - Items are returned newest-first (DESC update_ts).
//   - X-Paging-Since is EXCLUSIVE lower bound (i.e. update_ts of the
//     boundary item NOT included in the page; "0:0" when the page
//     reaches the oldest end of the collection).
//   - X-Paging-Until is INCLUSIVE upper bound (update_ts of the
//     newest item in the page; "now" when the page reaches the head).
//   - Link header carries `rel="prev"` and `rel="next"` cursors that
//     re-encode `paging.until=<since>` and `paging.since=<until>`
//     respectively. `first`/`last` are optional but if emitted must
//     point at the natural extremes (`paging.since=0:0` and bare).
//   - X-Paging-Limit reports the limit ACTUALLY applied (which may be
//     smaller than the request when the server's max is lower).

// TAI - UTC leap-second offset, as of 2026. Last leap second was 2017-01-01,
// no announcement of any since (IERS bulletin C). Bake in as a single int
// so we don't need the full leap table; the test harness happens to use
// the same value when it reads its own wall clock.
const taiMinusUTCSecs = 37

// nowTAI returns the registry's current wall clock as a TAI timestamp
// in the NMOS "<secs>:<nanos>" shape.
func nowTAI() string {
	t := time.Now()
	return formatTAI(t)
}

func formatTAI(t time.Time) string {
	secs := t.Unix() + taiMinusUTCSecs
	nanos := t.Nanosecond()
	return fmt.Sprintf("%d:%d", secs, nanos)
}

// taiBeforeAll is the lower bound test harnesses use as paging.since
// when they want everything from the bottom up.
const taiBeforeAll = "0:0"

// taiCmp compares two NMOS TAI strings. Returns -1 / 0 / +1. Treats
// malformed strings as "0:0".
func taiCmp(a, b string) int {
	as, an := splitTAI(a)
	bs, bn := splitTAI(b)
	if as != bs {
		if as < bs {
			return -1
		}
		return 1
	}
	if an < bn {
		return -1
	} else if an > bn {
		return 1
	}
	return 0
}

func splitTAI(s string) (int64, int64) {
	if s == "" {
		return 0, 0
	}
	parts := strings.SplitN(s, ":", 2)
	secs, _ := strconv.ParseInt(parts[0], 10, 64)
	if len(parts) < 2 {
		return secs, 0
	}
	nanos, _ := strconv.ParseInt(parts[1], 10, 64)
	return secs, nanos
}

// PageOptions controls a paged ListPaged call.
type PageOptions struct {
	Since string // exclusive lower bound; "" means "0:0"
	Until string // inclusive upper bound; "" means "now"
	Limit int    // 0 ⇒ default; clamped to MaxLimit
	// Predicate is an optional filter applied AFTER timestamp range
	// and BEFORE limit. The page boundaries (X-Paging-Since /
	// X-Paging-Until) are computed against the filtered set so that
	// `?description=X&paging.limit=10` returns 10 matching items and
	// the returned cursors page through the filtered series only.
	// Receives the typed resource (is04.Node, is04.Sender, …); the
	// caller is expected to type-assert.
	Predicate func(resource any) bool
}

// PageResult is the typed return from Store.ListPaged.
type PageResult struct {
	// Items are the resources in the page, newest-first. Element type
	// is the protocol struct (is04.Node, is04.Sender, ...) so the RQL
	// filter and JSON encoder both walk the typed shape directly.
	Items any
	// Since / Until are the actual page boundary timestamps — these
	// become X-Paging-Since / X-Paging-Until verbatim.
	Since string
	Until string
	// Limit is the limit applied (may differ from PageOptions.Limit
	// when clamped).
	Limit int
}

// Default + max page size. AMWA test_21_4 expects "empty page" cases
// to report a limit too, so we always emit X-Paging-Limit. 100 is the
// nmos-cpp default and matches the test harness's expectation when no
// paging.limit is supplied.
const (
	DefaultPageLimit = 100
	MaxPageLimit     = 1000
)

// ListPaged returns a page of resources of type t whose update_ts
// falls in the half-open interval (Since, Until]. Page direction
// depends on which cursor the caller pinned:
//
//   - opts.Since set                     → ascending: oldest items
//                                          ABOVE the cursor first
//   - opts.Until set OR neither set      → descending: newest items
//                                          AT-OR-BELOW the cursor first
//
// The body is always returned newest-first per IS-04 §6.1.6. The
// X-Paging-{Since,Until} cursors anchor the side the caller asked for
// and derive the other from the candidate set boundaries.
func (s *Store) ListPaged(t is04.ResourceType, opts PageOptions) PageResult {
	limit := opts.Limit
	limitProvided := opts.Limit > 0 // 0 / negative → default
	if !limitProvided {
		limit = DefaultPageLimit
	}
	if limit > MaxPageLimit {
		limit = MaxPageLimit
	}
	sinceProvided := opts.Since != "" && opts.Since != taiBeforeAll
	untilProvided := opts.Until != ""
	since := opts.Since
	if since == "" {
		since = taiBeforeAll
	}
	until := opts.Until
	if until == "" {
		until = nowTAI()
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	idx := s.updateTSByType[t]
	type kv struct {
		id  string
		ts  string
		res any
	}
	cands := make([]kv, 0, len(idx))
	for id, ts := range idx {
		if taiCmp(ts, since) <= 0 || taiCmp(ts, until) > 0 {
			continue
		}
		var res any
		switch t {
		case is04.ResourceNode:
			if v, ok := s.nodes[id]; ok {
				res = v
			}
		case is04.ResourceDevice:
			if v, ok := s.devices[id]; ok {
				res = v
			}
		case is04.ResourceSource:
			if v, ok := s.sources[id]; ok {
				res = v
			}
		case is04.ResourceFlow:
			if v, ok := s.flows[id]; ok {
				res = v
			}
		case is04.ResourceSender:
			if v, ok := s.senders[id]; ok {
				res = v
			}
		case is04.ResourceReceiver:
			if v, ok := s.receivers[id]; ok {
				res = v
			}
		}
		if res == nil {
			continue
		}
		if opts.Predicate != nil && !opts.Predicate(res) {
			continue
		}
		cands = append(cands, kv{id: id, ts: ts, res: res})
	}

	var pageUntil string
	pageSince := since
	if sinceProvided {
		// Ascending: oldest above the cursor first; cursor pins Since.
		sort.Slice(cands, func(i, j int) bool {
			c := taiCmp(cands[i].ts, cands[j].ts)
			if c != 0 {
				return c < 0
			}
			return cands[i].id < cands[j].id
		})
		truncated := len(cands) > limit
		if truncated {
			cands = cands[:limit]
		}
		pageSince = since
		switch {
		case truncated:
			// Page is full and there are more matching items above —
			// pageUntil is the newest item in the page (the boundary
			// the next page picks up from).
			pageUntil = cands[len(cands)-1].ts
		case untilProvided:
			// Page wasn't truncated and the client supplied an
			// explicit ceiling — echo it. AMWA test_21_5 Query 7/8/9.
			pageUntil = until
		case len(cands) > 0:
			pageUntil = cands[len(cands)-1].ts
		default:
			pageUntil = since
		}
		// Re-sort DESC for the response body — IS-04 mandates
		// newest-first regardless of which cursor anchors the page.
		sort.Slice(cands, func(i, j int) bool {
			c := taiCmp(cands[i].ts, cands[j].ts)
			if c != 0 {
				return c > 0
			}
			return cands[i].id > cands[j].id
		})
	} else {
		// Descending: newest below the cursor first; cursor pins Until.
		sort.Slice(cands, func(i, j int) bool {
			c := taiCmp(cands[i].ts, cands[j].ts)
			if c != 0 {
				return c > 0
			}
			return cands[i].id > cands[j].id
		})
		if len(cands) > limit {
			pageSince = cands[limit].ts
			cands = cands[:limit]
		}
		// pageUntil semantics for the default (head-anchored) page:
		//   - explicit paging.until         → echo it back
		//   - otherwise                     → the registry's overall
		//     time-series head: max update_ts across ALL items of
		//     this type (not just the filtered cands), or now-TAI
		//     when the type bucket is empty.
		//
		// AMWA test_21_3 case 3 + test_21_5 Query 4 specifically
		// assert pageUntil ≥ ts of the newest registered resource,
		// regardless of whether the filter shrunk the body.
		switch {
		case untilProvided:
			pageUntil = until
		default:
			pageUntil = s.maxUpdateTSLocked(t)
			if pageUntil == "" {
				pageUntil = nowTAI()
			}
		}
	}

	ress := make([]any, 0, len(cands))
	for _, c := range cands {
		ress = append(ress, c.res)
	}
	return PageResult{
		Items: collectTyped(t, ress),
		Since: pageSince,
		Until: pageUntil,
		Limit: limit,
	}
}

// collectTyped converts a generic []any of typed resources back into
// the homogeneous typed slice ([]is04.Node, []is04.Sender, …) so the
// JSON encoder + RQL filter both walk the canonical typed shape.
func collectTyped(t is04.ResourceType, items []any) any {
	switch t {
	case is04.ResourceNode:
		out := make([]is04.Node, 0, len(items))
		for _, v := range items {
			if n, ok := v.(is04.Node); ok {
				out = append(out, n)
			}
		}
		return out
	case is04.ResourceDevice:
		out := make([]is04.Device, 0, len(items))
		for _, v := range items {
			if d, ok := v.(is04.Device); ok {
				out = append(out, d)
			}
		}
		return out
	case is04.ResourceSource:
		out := make([]is04.Source, 0, len(items))
		for _, v := range items {
			if s, ok := v.(is04.Source); ok {
				out = append(out, s)
			}
		}
		return out
	case is04.ResourceFlow:
		out := make([]is04.Flow, 0, len(items))
		for _, v := range items {
			if f, ok := v.(is04.Flow); ok {
				out = append(out, f)
			}
		}
		return out
	case is04.ResourceSender:
		out := make([]is04.Sender, 0, len(items))
		for _, v := range items {
			if s, ok := v.(is04.Sender); ok {
				out = append(out, s)
			}
		}
		return out
	case is04.ResourceReceiver:
		out := make([]is04.Receiver, 0, len(items))
		for _, v := range items {
			if r, ok := v.(is04.Receiver); ok {
				out = append(out, r)
			}
		}
		return out
	}
	return items
}

