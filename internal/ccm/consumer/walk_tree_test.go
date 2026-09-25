package consumer

import (
	"context"
	transporthttp "dhs/internal/transport/http"
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
	return &Client{base: srv.URL, http: &transporthttp.Client{HTTP: srv.Client(), MaxBody: MaxBody}}
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

// The shapes a device takes when something is wrong, and the seeding
// rules a caller relies on. Each of these is a real field case: a
// bridge whose API root 404s, one whose root is a payload rather than a
// listing, one that answers a node with something that is not JSON, and
// one that keeps naming children.

// serverAnswering builds a device from a path→body map; a path not in
// the map 404s. status overrides the status for one path.
func serverAnswering(routes map[string]string) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Path
		if key == "/" {
			key = ""
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

func TestWalkTreeStopsWhenTheAPIRootCannotBeRead(t *testing.T) {
	// "The root may not exist" is the documented field reality. When no
	// start path was given there is nothing to walk, and saying so beats
	// returning an empty tree that reads like a device with no model.
	srv := serverAnswering(map[string]string{"/self": `{"app":"BRIDGE"}`})
	defer srv.Close()

	_, _, err := testClient(srv).WalkTree(context.Background())
	if err == nil {
		t.Fatal("a root that 404s must not produce an empty success")
	}
	if !strings.Contains(err.Error(), "API root") {
		t.Errorf("err = %v, want it to name the root", err)
	}
}

func TestWalkTreeStopsWhenTheAPIRootIsNotJSON(t *testing.T) {
	srv := serverAnswering(map[string]string{"": `<html>login</html>`})
	defer srv.Close()

	_, _, err := testClient(srv).WalkTree(context.Background())
	if err == nil {
		t.Fatal("a root that is not JSON must be an error")
	}
	if !strings.Contains(err.Error(), "classify") {
		t.Errorf("err = %v, want it to say it could not classify", err)
	}
}

func TestWalkTreeCapturesARootThatIsItselfAResource(t *testing.T) {
	// A device whose base returns a payload rather than a list of child
	// names: capture it and stop, rather than recursing into nothing.
	srv := serverAnswering(map[string]string{"": `{"app":{"productName":"BRIDGE"}}`})
	defer srv.Close()

	tree, deviations, err := testClient(srv).WalkTree(context.Background())
	if err != nil {
		t.Fatalf("WalkTree: %v", err)
	}
	if len(deviations) != 0 {
		t.Errorf("deviations = %v", deviations)
	}
	if tree.Len() != 1 {
		t.Fatalf("resources = %v, want just the root", tree.SortedPaths())
	}
	if _, ok := tree.Resources["/"]; !ok {
		t.Errorf("the root resource is keyed %v", tree.SortedPaths())
	}
}

func TestWalkTreeRecordsANodeItCannotClassify(t *testing.T) {
	// One bad node is a deviation; the rest of the device still walks.
	srv := serverAnswering(map[string]string{
		"":      `["self","broken"]`,
		"/self": `{"app":"BRIDGE"}`,
		// A body that is neither a listing nor valid JSON.
		"/broken": `{"unterminated":`,
	})
	defer srv.Close()

	tree, deviations, err := testClient(srv).WalkTree(context.Background())
	if err != nil {
		t.Fatalf("WalkTree: %v", err)
	}
	if tree.Len() != 1 {
		t.Errorf("resources = %v, want /self to survive", tree.SortedPaths())
	}
	if len(deviations) != 1 || !strings.Contains(deviations[0], "/broken") {
		t.Fatalf("deviations = %v, want one naming /broken", deviations)
	}
}

func TestWalkTreeVisitsEachPathOnce(t *testing.T) {
	// Two branches naming the same child — a device that cross-links, or
	// a caller seeding overlapping subtrees. Without the visited set the
	// walk fetches it twice; with a cycle it never returns.
	srv := serverAnswering(map[string]string{
		"":           `["a","b"]`,
		"/a":         `["shared"]`,
		"/b":         `["shared"]`,
		"/a/shared":  `{"x":1}`,
		"/b/shared":  `{"x":1}`,
	})
	defer srv.Close()

	// Seeding the same path twice must not double-walk it either.
	tree, deviations, err := testClient(srv).WalkTree(context.Background(), "/a", "/a")
	if err != nil {
		t.Fatalf("WalkTree: %v", err)
	}
	if len(deviations) != 0 {
		t.Errorf("deviations = %v", deviations)
	}
	if got := tree.SortedPaths(); len(got) != 1 || got[0] != "/a/shared" {
		t.Errorf("resources = %v", got)
	}
}

func TestSeededPathsAreNormalized(t *testing.T) {
	// A caller types what a URL looks like; the walk needs an
	// API-relative path. "io/" and "/io" are the same subtree, and a
	// bare "/" means "the root", not a child called "".
	srv := fakeTree()
	defer srv.Close()
	c := testClient(srv)

	for _, seed := range []string{"io", "/io", "/io/", "  /io  "} {
		tree, _, err := c.WalkTree(context.Background(), seed)
		if err != nil {
			t.Fatalf("WalkTree(%q): %v", seed, err)
		}
		if got := tree.SortedPaths(); len(got) != 2 {
			t.Errorf("WalkTree(%q) captured %v", seed, got)
		}
	}

	// A bare root seed walks the whole device, exactly as no seed does.
	tree, _, err := c.WalkTree(context.Background(), "/")
	if err != nil {
		t.Fatalf("WalkTree(/): %v", err)
	}
	if tree.Len() != 4 {
		t.Errorf("WalkTree(/) captured %v, want the whole device", tree.SortedPaths())
	}
}

func TestWalkTreeGivesUpOnADeviceThatNeverStops(t *testing.T) {
	// The guard is not a cap on real devices — the lab bridge has a few
	// hundred resources. It is for a device that keeps naming a child,
	// which is a walk that never returns and a process that grows until
	// it is killed. It must stop, say why, and hand back what it got.
	restore := maxWalkNodes
	maxWalkNodes = 50
	t.Cleanup(func() { maxWalkNodes = restore })

	var served int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		served++
		w.Header().Set("Content-Type", "application/json")
		// Every path is a branch naming one more child, so the paths
		// never repeat and the visited set never helps.
		_, _ = w.Write([]byte(`["deeper"]`))
	}))
	defer srv.Close()

	tree, deviations, err := testClient(srv).WalkTree(context.Background())
	if err != nil {
		t.Fatalf("a runaway is a deviation, not a failure: %v", err)
	}
	if len(deviations) != 1 || !strings.Contains(deviations[0], "runaway guard") {
		t.Fatalf("deviations = %v, want one naming the guard", deviations)
	}
	if served <= maxWalkNodes {
		t.Errorf("served %d nodes, expected to pass the %d guard", served, maxWalkNodes)
	}
	// It stopped, and what it walked is still a tree.
	if len(tree.Branches) == 0 {
		t.Error("the partial tree kept nothing")
	}
}
