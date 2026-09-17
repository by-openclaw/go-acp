package registry

import (
	"encoding/json"
	"io"
	stdhttp "net/http"
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is04"
)

// queryBase boots the HTTP face over a store holding the whole
// resource chain, and returns the Query API root for v1.3.
func queryBase(t *testing.T) (string, *Store) {
	t.Helper()
	store := populated(t)
	addr, stop := startRegistryHTTP(t, store, nil)
	t.Cleanup(stop)
	return "http://" + addr + "/x-nmos/query/v1.3", store
}

// get performs one GET and returns the status and body.
func get(t *testing.T, url string) (int, []byte) {
	t.Helper()
	resp, err := stdhttp.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", url, err)
	}
	return resp.StatusCode, body
}

// The Query API root lists the collections it serves — the entry
// point a Controller walks before it knows any resource.
func TestQueryRootListsCollections(t *testing.T) {
	base, _ := queryBase(t)
	status, body := get(t, base+"/")
	if status != stdhttp.StatusOK {
		t.Fatalf("GET / = %d: %s", status, body)
	}
	var got []string
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode: %v (%s)", err, body)
	}
	for _, want := range []string{"nodes/", "senders/", "subscriptions/"} {
		if !slicesContain(got, want) {
			t.Errorf("root listing %v is missing %q", got, want)
		}
	}
}

func slicesContain(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

// The per-id GET answers with the resource in the URL minor's wire
// shape, and 404s for an id it doesn't hold, an empty id, and a path
// that carries more than one segment past the collection.
func TestQueryPerIDGet(t *testing.T) {
	base, _ := queryBase(t)

	for plural, id := range map[string]string{
		"nodes": fxNode, "devices": fxDevice, "sources": fxSource,
		"flows": fxFlow, "senders": fxSender, "receivers": fxReceiver,
	} {
		status, body := get(t, base+"/"+plural+"/"+id)
		if status != stdhttp.StatusOK {
			t.Errorf("GET %s/%s = %d: %s", plural, id, status, body)
			continue
		}
		var res map[string]any
		if err := json.Unmarshal(body, &res); err != nil {
			t.Errorf("decode %s: %v", plural, err)
			continue
		}
		if res["id"] != id {
			t.Errorf("GET %s/%s returned id %v", plural, id, res["id"])
		}
	}

	// An id nobody registered, and a path that carries more than one
	// segment past the collection, are both 404. (A path that stops at
	// the collection is routed to the listing before the per-id
	// handler sees it, so it never reaches this branch.)
	for _, path := range []string{
		"/nodes/" + fxAbsent,
		"/nodes/" + fxNode + "/devices",
	} {
		if status, body := get(t, base+path); status != stdhttp.StatusNotFound {
			t.Errorf("GET %s = %d: %s", path, status, body)
		}
	}
}

// IS-04 §6.1.5: a resource registered at one minor is invisible on
// another's tree unless the client opts in with query.downgrade.
func TestQueryPerIDGetIsVersionGated(t *testing.T) {
	base, store := queryBase(t)
	v10Node := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	if err := store.IngestRegistrationVersioned(
		envelope(t, is04.ResourceNode, validNode(v10Node)), "v1.0"); err != nil {
		t.Fatal(err)
	}

	if status, body := get(t, base+"/nodes/"+v10Node); status != stdhttp.StatusNotFound {
		t.Errorf("a v1.0 Node on the v1.3 tree = %d: %s", status, body)
	}
	if status, _ := get(t, base+"/nodes/"+v10Node+"?query.downgrade=v1.0"); status != stdhttp.StatusOK {
		t.Errorf("query.downgrade=v1.0 must reveal it, got %d", status)
	}
}

// IS-04 §6.1.6: paging.since must not follow paging.until — the
// request is the client's error, answered 400 rather than with an
// empty page (AMWA test_21_6).
func TestQueryPagingSinceAfterUntil(t *testing.T) {
	base, _ := queryBase(t)
	status, body := get(t, base+"/nodes?paging.since=200:0&paging.until=100:0")
	if status != stdhttp.StatusBadRequest {
		t.Fatalf("since > until = %d: %s", status, body)
	}
	if !strings.Contains(string(body), "paging.since must be <= paging.until") {
		t.Errorf("body = %s", body)
	}
}

// paging.limit=0 is a legal request for no items: the server still
// emits the four cursors, collapses since and until onto the cursor
// the client supplied, and echoes the limit verbatim (test_21_4).
func TestQueryPagingLimitZero(t *testing.T) {
	base, _ := queryBase(t)
	for name, query := range map[string]string{
		"until anchors":        "?paging.limit=0&paging.until=1600000000:0",
		"since anchors":        "?paging.limit=0&paging.since=1600000000:0",
		"the clock anchors it": "?paging.limit=0",
	} {
		t.Run(name, func(t *testing.T) {
			resp, err := stdhttp.Get(base + "/nodes" + query)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()
			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != stdhttp.StatusOK {
				t.Fatalf("status %d: %s", resp.StatusCode, body)
			}
			var items []map[string]any
			if err := json.Unmarshal(body, &items); err != nil {
				t.Fatalf("decode: %v (%s)", err, body)
			}
			if len(items) != 0 {
				t.Errorf("limit=0 returned %d items", len(items))
			}
			if got := resp.Header.Get("X-Paging-Limit"); got != "0" {
				t.Errorf("X-Paging-Limit = %q, want the client's own 0", got)
			}
			since := resp.Header.Get("X-Paging-Since")
			until := resp.Header.Get("X-Paging-Until")
			if since == "" || since != until {
				t.Errorf("cursors = since %q, until %q; want both on one anchor", since, until)
			}
			if link := resp.Header.Get("Link"); !strings.Contains(link, `rel="next"`) {
				t.Errorf("Link = %q", link)
			}
		})
	}
}

// Ancestry is defined for the two kinds that carry `parents`. Asking
// it of a Sender is 501; asking it wrongly of a Source is 400.
func TestQueryAncestryErrors(t *testing.T) {
	base, _ := queryBase(t)

	if status, body := get(t, base+"/senders?query.ancestry_id="+fxSource+"&query.ancestry_type=children"); status != stdhttp.StatusNotImplemented {
		t.Errorf("ancestry on senders = %d: %s", status, body)
	}
	for name, query := range map[string]string{
		"id without type":        "?query.ancestry_id=" + fxSource,
		"type without id":        "?query.ancestry_type=children",
		"a type that is neither": "?query.ancestry_id=" + fxSource + "&query.ancestry_type=cousins",
		"zero generations":       "?query.ancestry_id=" + fxSource + "&query.ancestry_type=children&query.ancestry_generations=0",
		"generations not a number": "?query.ancestry_id=" + fxSource +
			"&query.ancestry_type=children&query.ancestry_generations=many",
	} {
		t.Run(name, func(t *testing.T) {
			if status, body := get(t, base+"/sources"+query); status != stdhttp.StatusBadRequest {
				t.Errorf("= %d: %s", status, body)
			}
		})
	}

	// A well-formed ancestry query is answered with the selected set:
	// the fixture Flow is the Source's only child.
	status, body := get(t, base+"/flows?query.ancestry_id="+fxSource+"&query.ancestry_type=children&query.ancestry_generations=1")
	if status != stdhttp.StatusOK {
		t.Fatalf("ancestry query = %d: %s", status, body)
	}
}

// The RQL-lite predicate and the plain field filters both narrow the
// page, and both index the cursors on the filtered set.
func TestQueryFiltersThePage(t *testing.T) {
	base, store := queryBase(t)
	second := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	other := validNode(second)
	other.Label = "cam-2"
	if err := store.PutNode(other); err != nil {
		t.Fatal(err)
	}

	count := func(url string) int {
		t.Helper()
		status, body := get(t, url)
		if status != stdhttp.StatusOK {
			t.Fatalf("GET %s = %d: %s", url, status, body)
		}
		var items []map[string]any
		if err := json.Unmarshal(body, &items); err != nil {
			t.Fatalf("decode: %v (%s)", err, body)
		}
		return len(items)
	}

	if n := count(base + "/nodes"); n != 2 {
		t.Fatalf("unfiltered listing = %d nodes, want 2", n)
	}
	if n := count(base + "/nodes?label=cam-2"); n != 1 {
		t.Errorf("label filter = %d nodes, want 1", n)
	}
	if n := count(base + "/nodes?query.rql=eq(label,cam-2)"); n != 1 {
		t.Errorf("RQL filter = %d nodes, want 1", n)
	}
	if n := count(base + "/nodes?query.rql=eq(label,nobody)"); n != 0 {
		t.Errorf("an RQL predicate nothing satisfies = %d nodes, want 0", n)
	}
}
