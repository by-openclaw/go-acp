package registry

// Target-leg convergence + grain-grammar tests (fleet fixes on issue
// #946, round 3):
//
//   - identical re-applies emit NO grain — the store's identical-put
//     short-circuit, proven at store level AND on a live served-face
//     WS subscription (AMWA IS-04-02 test_24_1: `pre` belongs to
//     modified/removed only);
//   - a version-locked target (409 + Location, nmos-cpp style) is
//     reconciled deterministically for both DELETE and POST;
//   - resync refreshes the catalogue from per-minor exact-match REST
//     views, stamping every repopulated resource's minor;
//   - a registration URL is never emitted with an empty minor — the
//     forward is skipped and audited instead;
//   - a removal delivered on several minors' sockets reaches the
//     target exactly once, at the tracked minor.

import (
	"context"
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"dhs/internal/amwa/codec/is04"
	"dhs/internal/amwa/session/query"
)

// newTestMirror builds an unstarted mirror wired at a target URL with
// a live in-memory audit ring — for driving the target-leg functions
// directly.
func newTestMirror(t *testing.T, target string) *Mirror {
	t.Helper()
	m, err := NewMirror(MirrorOptions{
		Source: "http://source.invalid:1", Target: target, APIVer: "v1.3",
	})
	if err != nil {
		t.Fatal(err)
	}
	m.audit, _ = newAuditor("")
	return m
}

// recordingTarget captures every request and lets a test script the
// responses.
type recordingTarget struct {
	mu      sync.Mutex
	reqs    []string // "METHOD path"
	respond func(method, path string, w stdhttp.ResponseWriter) bool
}

func (rt *recordingTarget) handler() stdhttp.Handler {
	return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		rt.mu.Lock()
		rt.reqs = append(rt.reqs, r.Method+" "+r.URL.Path)
		rt.mu.Unlock()
		if rt.respond != nil && rt.respond(r.Method, r.URL.Path, w) {
			return
		}
		w.WriteHeader(stdhttp.StatusNotFound)
	})
}

func (rt *recordingTarget) requests() []string {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return append([]string(nil), rt.reqs...)
}

// TestStoreIdenticalPutEmitsNoChange: a byte-identical re-registration
// is a no-op at the store — no Change, no update_ts bump; a genuine
// modification still emits.
func TestStoreIdenticalPutEmitsNoChange(t *testing.T) {
	store := NewStore()
	var mu sync.Mutex
	var changes []Change
	store.AddListener(func(c Change) {
		mu.Lock()
		changes = append(changes, c)
		mu.Unlock()
	})

	const id = "0770e57e-0000-4000-8000-000000000024"
	node := validNode(id)
	if err := store.PutNode(node); err != nil {
		t.Fatal(err)
	}
	if err := store.PutNode(node); err != nil {
		t.Fatal(err) // identical re-put must stay a nil-error no-op
	}
	mu.Lock()
	n := len(changes)
	mu.Unlock()
	if n != 1 {
		t.Fatalf("changes after identical re-put = %d, want 1 (created only)", n)
	}
	node.Label = "renamed"
	if err := store.PutNode(node); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(changes) != 2 || changes[1].Kind != ChangeUpdated {
		t.Fatalf("changes after real modification = %+v, want a second, updated one", changes)
	}
}

// TestMirrorServeReplayEmitsNoGrain: on a live served-face WS
// subscription, the add arrives as a post-only grain, a serveReplay of
// identical documents emits NOTHING, and a genuine modification still
// arrives (with pre+post).
func TestMirrorServeReplayEmitsNoGrain(t *testing.T) {
	m, push, _ := startServedMirror(t)
	base := "http://" + m.ServeAddr()

	codec, ok := is04.Get("v1.3")
	if !ok {
		t.Fatal("v1.3 codec not registered")
	}
	qc, err := query.NewClient(base, codec)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sub, err := qc.Subscribe(ctx, query.SubscribeRequest{ResourcePath: "/nodes"})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	var mu sync.Mutex
	var rows []is04.GrainDataRow
	go func() {
		_ = query.Watch(ctx, sub.WSHref, func(g *is04.Grain) error {
			mu.Lock()
			rows = append(rows, g.Grain.Data...)
			mu.Unlock()
			return nil
		}, query.WatchOptions{})
	}()
	// The add must arrive LIVE (a pre-subscription add would ride the
	// sync grain, which legitimately carries pre) — wait for the
	// subscriber socket before pushing.
	waitFor(t, 5*time.Second, func() bool {
		return m.Stats().ServedWSSubs >= 1
	}, "served WS subscriber to connect")

	const nodeID = "f47ac10b-58cc-4372-a567-0e02b2c3d424"
	node := validNode(nodeID)
	nodeDoc, _ := json.Marshal(node)
	push.push("nodes", grainFrame("nodes", nodeID, "", string(nodeDoc)))

	// The live add must arrive post-only — no pre key on an added row.
	waitFor(t, 5*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		for _, r := range rows {
			if r.Path == nodeID {
				return true
			}
		}
		return false
	}, "added grain on the served WS")
	mu.Lock()
	for _, r := range rows {
		if r.Path == nodeID && len(r.Pre) > 0 {
			t.Errorf("added grain carries pre: %s", r.Pre)
		}
	}
	before := len(rows)
	mu.Unlock()

	// Replay of identical documents: zero grains.
	m.serveReplay()
	time.Sleep(900 * time.Millisecond)
	mu.Lock()
	after := len(rows)
	mu.Unlock()
	if after != before {
		t.Fatalf("serveReplay of identical docs emitted %d grain row(s) — want none", after-before)
	}

	// A real modification still flows, as modified (pre AND post).
	renamed := node
	renamed.Label = "renamed-after-replay"
	renamedDoc, _ := json.Marshal(renamed)
	push.push("nodes", grainFrame("nodes", nodeID, string(nodeDoc), string(renamedDoc)))
	waitFor(t, 5*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		for _, r := range rows[before:] {
			if r.Path == nodeID && len(r.Pre) > 0 && len(r.Post) > 0 {
				return true
			}
		}
		return false
	}, "modified grain after the quiet replay")
}

// TestMirrorDeleteConvergesOn409: a DELETE at the tracked minor that
// the target 409s (version-lock, Location naming the held minor) is
// retried once at the minor the TARGET knows and lands.
func TestMirrorDeleteConvergesOn409(t *testing.T) {
	const nodeID = "0770e57e-0000-4000-8000-000000000409"
	rt := &recordingTarget{}
	rt.respond = func(method, path string, w stdhttp.ResponseWriter) bool {
		if method != stdhttp.MethodDelete {
			return false
		}
		if strings.Contains(path, "/v1.0/") {
			w.Header().Set("Location", "/x-nmos/registration/v1.3/resource/nodes/"+nodeID)
			w.WriteHeader(stdhttp.StatusConflict)
			return true
		}
		if strings.Contains(path, "/v1.3/") {
			w.WriteHeader(stdhttp.StatusNoContent)
			return true
		}
		return false
	}
	ts := httptest.NewServer(rt.handler())
	t.Cleanup(ts.Close)

	m := newTestMirror(t, ts.URL)
	m.deleteResource(context.Background(), "nodes", "v1.0", nodeID)

	want := []string{
		"DELETE /x-nmos/registration/v1.0/resource/nodes/" + nodeID,
		"DELETE /x-nmos/registration/v1.3/resource/nodes/" + nodeID,
	}
	got := rt.requests()
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("target saw %v, want %v", got, want)
	}
	if st := m.Stats(); st.Deleted != 1 {
		t.Errorf("stats.Deleted = %d, want 1 (%+v)", st.Deleted, st)
	}
	if !hasKind(auditKinds(m), "target_ver_conflict") {
		t.Errorf("audit ring missing target_ver_conflict: %v", auditKinds(m))
	}
}

// TestMirrorPostConvergesOn409: a POST version-lock 409 is reconciled
// by deleting the target's copy at the minor IT names, then
// re-POSTing once at ours — the target ends holding the resource at
// its true registered minor.
func TestMirrorPostConvergesOn409(t *testing.T) {
	const nodeID = "0770e57e-0000-4000-8000-000000000410"
	var mu sync.Mutex
	held := true // the target holds nodeID registered under v1.3
	rt := &recordingTarget{}
	rt.respond = func(method, path string, w stdhttp.ResponseWriter) bool {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case method == stdhttp.MethodPost && strings.Contains(path, "/v1.0/") && strings.HasSuffix(path, "/resource"):
			if held {
				w.Header().Set("Location", "/x-nmos/registration/v1.3/resource/nodes/"+nodeID)
				w.WriteHeader(stdhttp.StatusConflict)
				return true
			}
			w.WriteHeader(stdhttp.StatusCreated)
			return true
		case method == stdhttp.MethodDelete && strings.Contains(path, "/v1.3/"):
			held = false
			w.WriteHeader(stdhttp.StatusNoContent)
			return true
		}
		return false
	}
	ts := httptest.NewServer(rt.handler())
	t.Cleanup(ts.Close)

	m := newTestMirror(t, ts.URL)
	doc := json.RawMessage(`{"id":"` + nodeID + `"}`)
	m.postResource(context.Background(), "nodes", "v1.0", nodeID, doc, false)

	want := []string{
		"POST /x-nmos/registration/v1.0/resource",
		"DELETE /x-nmos/registration/v1.3/resource/nodes/" + nodeID,
		"POST /x-nmos/registration/v1.0/resource",
	}
	got := rt.requests()
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("target saw %v, want %v", got, want)
	}
	if st := m.Stats(); st.Forwarded != 1 {
		t.Errorf("stats.Forwarded = %d, want 1 (%+v)", st.Forwarded, st)
	}
	if !hasKind(auditKinds(m), "target_ver_conflict") {
		t.Errorf("audit ring missing target_ver_conflict: %v", auditKinds(m))
	}
}

// TestMirrorResyncRefreshStampsMinors: resync re-fetches per-minor
// exact-match views from the source and every repopulated resource
// carries its true minor — into cacheVer AND into the registration
// URL the target sees.
func TestMirrorResyncRefreshStampsMinors(t *testing.T) {
	const nodeID = "f47ac10b-58cc-4372-a567-0e02b2c3d425"
	nodeDoc, _ := json.Marshal(validNode(nodeID))

	src := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if r.Method == stdhttp.MethodGet && strings.HasPrefix(r.URL.Path, "/x-nmos/query/v1.3/") {
			w.Header().Set("Content-Type", "application/json")
			if strings.HasSuffix(r.URL.Path, "/nodes") {
				_, _ = w.Write([]byte("[" + string(nodeDoc) + "]"))
				return
			}
			_, _ = w.Write([]byte("[]"))
			return
		}
		w.WriteHeader(stdhttp.StatusNotFound)
	}))
	t.Cleanup(src.Close)

	rt := &recordingTarget{}
	rt.respond = func(method, path string, w stdhttp.ResponseWriter) bool {
		if method == stdhttp.MethodPost && strings.HasSuffix(path, "/resource") {
			w.WriteHeader(stdhttp.StatusCreated)
			return true
		}
		return false
	}
	ts := httptest.NewServer(rt.handler())
	t.Cleanup(ts.Close)

	m := newTestMirror(t, ts.URL)
	codec, ok := is04.Get("v1.3")
	if !ok {
		t.Fatal("v1.3 codec not registered")
	}
	qc, err := query.NewClient(src.URL, codec)
	if err != nil {
		t.Fatal(err)
	}
	m.sourceClients = map[string]*query.Client{"v1.3": qc}

	m.resync(context.Background())

	m.mu.Lock()
	gotVer := m.cacheVer["nodes"][nodeID]
	_, cached := m.cache["nodes"][nodeID]
	m.mu.Unlock()
	if !cached || gotVer != "v1.3" {
		t.Fatalf("refresh stamped cacheVer=%q cached=%v, want v1.3/true", gotVer, cached)
	}
	found := false
	for _, r := range rt.requests() {
		if r == "POST /x-nmos/registration/v1.3/resource" {
			found = true
		}
	}
	if !found {
		t.Errorf("target requests %v missing the v1.3-addressed POST", rt.requests())
	}
}

// TestMirrorForwardSkipsEmptyVer: a resource without a tracked minor
// is never turned into a registration URL — the forward is skipped
// and audited.
func TestMirrorForwardSkipsEmptyVer(t *testing.T) {
	rt := &recordingTarget{}
	ts := httptest.NewServer(rt.handler())
	t.Cleanup(ts.Close)

	m := newTestMirror(t, ts.URL)
	m.postResource(context.Background(), "nodes", "", "some-id", json.RawMessage(`{"id":"x"}`), false)
	m.deleteResource(context.Background(), "nodes", "", "some-id")

	if got := rt.requests(); len(got) != 0 {
		t.Fatalf("target must see nothing for a minor-less forward, saw %v", got)
	}
	if !hasKind(auditKinds(m), "forward_missing_ver") {
		t.Errorf("audit ring missing forward_missing_ver: %v", auditKinds(m))
	}
	if st := m.Stats(); st.Failures != 2 {
		t.Errorf("stats.Failures = %d, want 2 (%+v)", st.Failures, st)
	}
}

// TestMirrorHeartbeatsOnlyAcceptedNodes: a node the target refused is
// not heartbeated (its 404 would masquerade as an eviction and drive
// a full-resync-per-tick thrash loop); an accepted node is.
func TestMirrorHeartbeatsOnlyAcceptedNodes(t *testing.T) {
	const refusedID = "f47ac10b-58cc-4372-a567-0e02b2c3d427"
	const acceptedID = "f47ac10b-58cc-4372-a567-0e02b2c3d428"
	rt := &recordingTarget{}
	var refuse bool
	rt.respond = func(method, path string, w stdhttp.ResponseWriter) bool {
		if method == stdhttp.MethodPost && strings.HasSuffix(path, "/resource") {
			if refuse {
				w.WriteHeader(stdhttp.StatusBadRequest)
			} else {
				w.WriteHeader(stdhttp.StatusCreated)
			}
			return true
		}
		return false
	}
	ts := httptest.NewServer(rt.handler())
	t.Cleanup(ts.Close)

	m := newTestMirror(t, ts.URL)
	ctx := context.Background()
	refusedDoc, _ := json.Marshal(validNode(refusedID))
	acceptedDoc, _ := json.Marshal(validNode(acceptedID))
	refuse = true
	m.forwardRow(ctx, "nodes", "v1.3", is04.GrainDataRow{Path: refusedID, Post: refusedDoc})
	refuse = false
	m.forwardRow(ctx, "nodes", "v1.3", is04.GrainDataRow{Path: acceptedID, Post: acceptedDoc})

	refs, unaccepted := m.nodeIDs()
	if len(refs) != 1 || refs[0].id != acceptedID || refs[0].ver != "v1.3" {
		t.Fatalf("heartbeat set = %+v, want only the accepted node at v1.3", refs)
	}
	if unaccepted != 1 {
		t.Errorf("unaccepted = %d, want 1 (the refused node)", unaccepted)
	}
}

// TestMirrorRemovalDedupe: a removal delivered on several minors'
// sockets (an unstamped source broadcasts deletions) reaches the
// target exactly once, at the tracked minor.
func TestMirrorRemovalDedupe(t *testing.T) {
	const nodeID = "f47ac10b-58cc-4372-a567-0e02b2c3d426"
	nodeDoc, _ := json.Marshal(validNode(nodeID))

	rt := &recordingTarget{}
	rt.respond = func(method, path string, w stdhttp.ResponseWriter) bool {
		switch method {
		case stdhttp.MethodPost:
			w.WriteHeader(stdhttp.StatusCreated)
			return true
		case stdhttp.MethodDelete:
			w.WriteHeader(stdhttp.StatusNoContent)
			return true
		}
		return false
	}
	ts := httptest.NewServer(rt.handler())
	t.Cleanup(ts.Close)

	m := newTestMirror(t, ts.URL)
	ctx := context.Background()
	m.forwardRow(ctx, "nodes", "v1.3", is04.GrainDataRow{Path: nodeID, Post: nodeDoc})
	removal := is04.GrainDataRow{Path: nodeID, Pre: nodeDoc}
	m.forwardRow(ctx, "nodes", "v1.3", removal)
	m.forwardRow(ctx, "nodes", "v1.0", removal) // second socket's copy of the same removal

	var deletes []string
	for _, r := range rt.requests() {
		if strings.HasPrefix(r, "DELETE ") {
			deletes = append(deletes, r)
		}
	}
	if len(deletes) != 1 || !strings.Contains(deletes[0], "/v1.3/") {
		t.Fatalf("deletes = %v, want exactly one at the tracked v1.3 minor", deletes)
	}
}
