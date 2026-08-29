package query

import (
	"context"
	"encoding/json"
	"fmt"
	stdhttp "net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	v13 "dhs/internal/amwa/codec/is04/v13"
)

// pagedRegistry emulates the AMWA-pinned IS-04 §6.1.6 paging
// orientation, the one our own registry implements: collections are
// newest-first; the Link cursors move through TIME (rel="next" =
// NEWER, rel="prev" = OLDER); an unanchored GET returns the head
// page; paging.until anchors a page of items at-or-below the cursor.
//
// The previous version of this mock treated next as an ascending
// index cursor — the double-mock blindness that let a client
// following next from the head pass its unit test while a live walk
// of a 211-sender plant returned 100 of 211 (2026-08-29).
func pagedRegistry(t *testing.T, total, pageSize int) *httptest.Server {
	t.Helper()
	// Item i has update index i; newest is total-1.
	var srv *httptest.Server
	srv = httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if r.URL.Path != "/x-nmos/query/v1.3/senders" && r.URL.Path != "/x-nmos/query/v1.3/senders/" {
			w.WriteHeader(404)
			return
		}
		// Anchor: highest index INCLUDED in this page.
		top := total - 1
		if u := r.URL.Query().Get("paging.until"); u != "" {
			var n int
			_, _ = fmt.Sscanf(u, "100:%d", &n)
			top = n
		}
		bottom := top - pageSize + 1
		if bottom < 0 {
			bottom = 0
		}
		var page []map[string]any
		for i := top; i >= bottom; i-- { // newest-first body
			page = append(page, map[string]any{"id": fmt.Sprintf("00000000-0000-4000-8000-%012d", i)})
		}
		links := ""
		if bottom > 0 {
			links = fmt.Sprintf(`<%s/x-nmos/query/v1.3/senders?paging.until=100:%d>; rel="prev"`, srv.URL, bottom-1)
		}
		// next always points at newer-than-this-page — from the head
		// page that is the future, which is legally an empty page.
		if links != "" {
			links += ", "
		}
		links += fmt.Sprintf(`<%s/x-nmos/query/v1.3/senders?paging.since=100:%d>; rel="next"`, srv.URL, top)
		w.Header().Set("Link", links)
		w.Header().Set("X-Paging-Limit", strconv.Itoa(pageSize))
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("paging.since") != "" {
			// A since above the head: nothing newer exists.
			_, _ = w.Write([]byte("[]"))
			return
		}
		_ = json.NewEncoder(w).Encode(page)
	}))
	return srv
}

// TestFetchListFollowsPagination: against the spec orientation the
// whole collection lies behind rel="prev" — the walk must follow it
// from the head and terminate cleanly, yielding every item once.
func TestFetchListFollowsPagination(t *testing.T) {
	srv := pagedRegistry(t, 7, 2)
	defer srv.Close()

	c, err := NewClient(srv.URL, v13.New())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := c.fetchListRaw(context.Background(), "senders", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 7 {
		t.Fatalf("got %d senders across pages, want 7 — the walk must follow rel=\"prev\" from the head", len(raw))
	}
	seen := map[string]bool{}
	for _, rb := range raw {
		var v struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(rb, &v)
		if seen[v.ID] {
			t.Fatalf("duplicate %s in walk", v.ID)
		}
		seen[v.ID] = true
	}
}

// TestFetchListAscendingNextRegistry: servers that treat next as an
// ascending index cursor (and emit no prev) still walk fully — the
// first page picks the chain.
func TestFetchListAscendingNextRegistry(t *testing.T) {
	const total, pageSize = 7, 2
	var srv *httptest.Server
	srv = httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		since := 0
		if s := r.URL.Query().Get("paging.since"); s != "" {
			since, _ = strconv.Atoi(s)
		}
		end := since + pageSize
		if end > total {
			end = total
		}
		var page []map[string]any
		for i := since; i < end; i++ {
			page = append(page, map[string]any{"id": fmt.Sprintf("00000000-0000-4000-8000-%012d", i)})
		}
		if end < total {
			w.Header().Set("Link",
				fmt.Sprintf(`<%s/x-nmos/query/v1.3/senders?paging.since=%d>; rel="next"`, srv.URL, end))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(page)
	}))
	defer srv.Close()

	c, _ := NewClient(srv.URL, v13.New())
	raw, err := c.fetchListRaw(context.Background(), "senders", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != total {
		t.Fatalf("got %d, want %d — next-chain fallback broken", len(raw), total)
	}
}

// TestFetchListSinglePageRegistry: a collection that fits one page
// (prev absent, next pointing at the empty future) yields exactly it.
func TestFetchListSinglePageRegistry(t *testing.T) {
	srv := pagedRegistry(t, 2, 100)
	defer srv.Close()

	c, _ := NewClient(srv.URL, v13.New())
	raw, err := c.fetchListRaw(context.Background(), "senders", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 2 {
		t.Fatalf("got %d, want 2", len(raw))
	}
}

// TestFetchListStopsOnLinkLoop: a server whose chain link points back
// at an earlier page must not hang the client.
func TestFetchListStopsOnLinkLoop(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		w.Header().Set("Link", fmt.Sprintf(`<%s/x-nmos/query/v1.3/senders>; rel="prev"`, srv.URL))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"00000000-0000-4000-8000-000000000001"}]`))
	}))
	defer srv.Close()

	c, err := NewClient(srv.URL, v13.New())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := c.fetchListRaw(context.Background(), "senders", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < 1 || len(raw) > 2 {
		t.Fatalf("loop guard failed: got %d items", len(raw))
	}
}

// TestFetchListIgnoringRegistry: a registry that ignores paging
// entirely (full array, no Link) still works — one page and stop.
func TestFetchListIgnoringRegistry(t *testing.T) {
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"00000000-0000-4000-8000-000000000001"},{"id":"00000000-0000-4000-8000-000000000002"}]`))
	}))
	defer srv.Close()

	c, _ := NewClient(srv.URL, v13.New())
	raw, err := c.fetchListRaw(context.Background(), "senders", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 2 {
		t.Fatalf("got %d, want 2", len(raw))
	}
}
