package consumer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
)

// fakeTree is a minimal recursive CCM device: nodes answer with child
// names, resources answer with objects. It mirrors the live BRIDGE shape.
func fakeTree() *httptest.Server {
	routes := map[string]string{
		"":                `["self","io","misc"]`, // GET of the base (path "/")
		"/self":           `{"app":{"productName":"BRIDGE","productVersion":"7.0.2"}}`,
		"/io":             `["ip","sdi"]`,
		"/io/ip":          `["senders"]`,
		"/io/ip/senders":  `[{"uuid":"s-1","name":"tx"}]`,
		"/io/sdi":         `[{"uuid":"sdi-7","name":"SDI Input 7"}]`,
		"/misc":           `["reference","gone"]`, // "gone" 404s → deviation
		"/misc/reference": `{"locked":true}`,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Path
		if key == "/" {
			key = "" // GET of the base
		}
		body, ok := routes[key]
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	})
	return httptest.NewServer(mux)
}

func testClient(srv *httptest.Server) *Client {
	return &Client{base: srv.URL, http: srv.Client()}
}

func TestWalkTreeFull(t *testing.T) {
	srv := fakeTree()
	defer srv.Close()
	c := testClient(srv)

	tree, deviations, err := c.WalkTree(context.Background())
	if err != nil {
		t.Fatalf("WalkTree: %v", err)
	}

	got := tree.SortedPaths()
	want := []string{"/io/ip/senders", "/io/sdi", "/misc/reference", "/self"}
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("resources = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("resources = %v, want %v", got, want)
		}
	}

	// The one bad child must surface as a deviation, not abort the walk.
	if len(deviations) != 1 {
		t.Fatalf("deviations = %v, want exactly one (/misc/gone)", deviations)
	}
	if want := "/misc/gone"; !strings.Contains(deviations[0], want) {
		t.Fatalf("deviation = %q, want it to name %q", deviations[0], want)
	}

	// Branch skeleton captured (root + /io + /io/ip + /misc).
	if len(tree.Branches) != 4 {
		t.Fatalf("branches = %v, want 4", tree.Branches)
	}
}

func TestWalkTreeSeededStartPath(t *testing.T) {
	srv := fakeTree()
	defer srv.Close()
	c := testClient(srv)

	// Seed an explicit subtree — the "root doesn't exist, I know the node
	// path" case. Only that subtree is captured.
	tree, deviations, err := c.WalkTree(context.Background(), "/io")
	if err != nil {
		t.Fatalf("WalkTree(/io): %v", err)
	}
	if len(deviations) != 0 {
		t.Fatalf("deviations = %v, want none", deviations)
	}
	got := tree.SortedPaths()
	want := []string{"/io/ip/senders", "/io/sdi"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("resources = %v, want %v", got, want)
	}
	// /self and /misc/* must NOT appear — we seeded only /io.
	if _, ok := tree.Resources["/self"]; ok {
		t.Fatalf("/self leaked into a /io-seeded walk")
	}
}
