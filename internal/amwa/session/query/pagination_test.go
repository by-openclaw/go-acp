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

// pagedRegistry serves /x-nmos/query/v1.3/senders in pages of two with
// AMWA-style Link headers — the exact trap the IS-04-04 suite sets by
// dropping the mock's paging limit to 2.
func pagedRegistry(t *testing.T, total int) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if r.URL.Path != "/x-nmos/query/v1.3/senders" && r.URL.Path != "/x-nmos/query/v1.3/senders/" {
			w.WriteHeader(404)
			return
		}
		since := 0
		if s := r.URL.Query().Get("paging.since"); s != "" {
			since, _ = strconv.Atoi(s)
		}
		end := since + 2
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
	return srv
}

// TestFetchListFollowsPagination: a registry paging at limit 2 must
// still yield the whole collection. Before this, fetchListRaw took one
// page and a controller silently saw 2 senders of however many the
// plant had.
func TestFetchListFollowsPagination(t *testing.T) {
	srv := pagedRegistry(t, 7)
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
		t.Fatalf("got %d senders across pages, want 7 — pagination not followed", len(raw))
	}
}

// TestFetchListStopsOnLinkLoop: a server whose next link points back at
// an earlier page must not hang the client.
func TestFetchListStopsOnLinkLoop(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		// Always the same page, always a "next" pointing at itself.
		w.Header().Set("Link", fmt.Sprintf(`<%s/x-nmos/query/v1.3/senders>; rel="next"`, srv.URL))
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
	if len(raw) != 1 {
		t.Fatalf("loop guard failed: got %d items", len(raw))
	}
}
