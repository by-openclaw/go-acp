package registry

import (
	"context"
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"dhs/internal/amwa/codec/is04"
	"dhs/internal/amwa/session/query"
)

// A mirror refuses to run against a minor it has no codec for, and
// against an audit log it cannot open — both are startup failures, not
// something to discover mid-forward.
func TestMirrorRunRefusesBadConfiguration(t *testing.T) {
	m, err := NewMirror(MirrorOptions{Source: "http://source:1", Target: "http://target:2", APIVer: "v9.9"})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Run(context.Background()); err == nil || !contains(err.Error(), "unknown api-ver") {
		t.Errorf("Run at an unknown minor = %v", err)
	}

	// An audit path inside a file (not a directory) cannot be opened.
	dir := t.TempDir()
	blocker := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	m2, err := NewMirror(MirrorOptions{
		Source: "http://source:1", Target: "http://target:2", APIVer: "v1.3",
		AuditPath: filepath.Join(blocker, "audit.jsonl"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := m2.Run(context.Background()); err == nil || !contains(err.Error(), "audit log") {
		t.Errorf("Run with an unopenable audit log = %v", err)
	}
}

// The audit ring keeps a bounded tail, and writes through to the JSONL
// sink when the operator asked for one — that file is the evidence
// trail for what the external registry did.
func TestAuditorRingAndFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	a, err := newAuditor(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < auditRingSize+10; i++ {
		a.event("forward_failed", map[string]any{"n": i})
	}
	a.close()

	if got := len(a.recent()); got != auditRingSize {
		t.Errorf("ring holds %d events, want it bounded at %d", got, auditRingSize)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var lines int
	for _, b := range raw {
		if b == '\n' {
			lines++
		}
	}
	if lines != auditRingSize+10 {
		t.Errorf("the JSONL sink holds %d lines, want every event", lines)
	}

	// A mirror with no auditor yet records nothing rather than
	// panicking — the forward helpers run before Run installs one.
	var absent *auditor
	absent.event("forward_failed", nil)
	if absent.recent() != nil {
		t.Error("a nil auditor has no events")
	}
	absent.close()
}

// forwardRow is the mirror's dedupe point: the same document arriving
// on a second minor's socket, or re-delivered by a reconnect's sync
// grain, must not reach the target twice. A removal for something the
// cache never held is likewise already handled.
func TestForwardRowDeduplicates(t *testing.T) {
	target := &scriptedTarget{}
	ts := httptest.NewServer(target.handler())
	defer ts.Close()

	m := mirrorTo(t, ts.URL)
	tap := newRegistryLogTap()
	m.logger = tap.logger()
	ctx := context.Background()

	node := mustJSONBytes(t, validNode(fxNode))
	row := is04.GrainDataRow{Path: fxNode, Post: node}

	m.forwardRow(ctx, "nodes", "v1.3", row)
	m.forwardRow(ctx, "nodes", "v1.0", row) // the same document, another socket
	if got := len(target.seen()); got != 1 {
		t.Errorf("the target saw %d POSTs, want the duplicate collapsed", got)
	}
	m.mu.Lock()
	tracked := m.cacheVer["nodes"][fxNode]
	m.mu.Unlock()
	if tracked != "v1.3" {
		t.Errorf("tracked minor = %q, want the one it was first seen at", tracked)
	}

	// A removal reaches the target once; the second socket's copy of
	// the same removal finds nothing cached and stops there.
	removal := is04.GrainDataRow{Path: fxNode, Pre: node}
	m.forwardRow(ctx, "nodes", "v1.3", removal)
	m.forwardRow(ctx, "nodes", "v1.0", removal)
	deletes := 0
	for _, req := range target.seen() {
		if len(req) > 6 && req[:6] == "DELETE" {
			deletes++
		}
	}
	if deletes != 1 {
		t.Errorf("the target saw %d DELETEs, want the duplicate collapsed", deletes)
	}

	// A row with neither half is not a change the grammar defines.
	m.forwardRow(ctx, "nodes", "v1.3", is04.GrainDataRow{Path: fxNode})
	if !tap.has("neither pre nor post") {
		t.Errorf("an empty row must be reported; saw %v", tap.snapshot())
	}
}

// A removal that arrives before the resource was ever cached still
// addresses the minor the socket carried — there is no tracked one to
// use.
func TestForwardRowRemovalWithoutATrackedMinor(t *testing.T) {
	target := &scriptedTarget{answer: func(string, string, int) (int, string) {
		return stdhttp.StatusNoContent, ""
	}}
	ts := httptest.NewServer(target.handler())
	defer ts.Close()

	m := mirrorTo(t, ts.URL)
	m.logger = newRegistryLogTap().logger()
	node := mustJSONBytes(t, validNode(fxNode))
	m.mu.Lock()
	m.cache["nodes"][fxNode] = node // cached, but never version-stamped
	m.mu.Unlock()

	m.forwardRow(context.Background(), "nodes", "v1.1", is04.GrainDataRow{Path: fxNode, Pre: node})
	seen := target.seen()
	if len(seen) != 1 || !contains(seen[0], "/v1.1/") {
		t.Errorf("requests = %v, want the delete at the socket's own minor", seen)
	}
}

// A source that refuses the subscription is retried, not abandoned:
// the mirror backs off and resubscribes until its context ends.
func TestWatchTopicResubscribes(t *testing.T) {
	var attempts int
	src := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		attempts++
		w.WriteHeader(stdhttp.StatusInternalServerError)
	}))
	defer src.Close()

	codec, ok := is04.Get("v1.3")
	if !ok {
		t.Skip("the v1.3 codec is not registered in this binary")
	}
	qc, err := query.NewClient(src.URL, codec)
	if err != nil {
		t.Fatal(err)
	}

	m := mirrorTo(t, "http://target:8235")
	tap := newRegistryLogTap()
	m.logger = tap.logger()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		m.watchTopic(ctx, qc, "nodes", "v1.3")
		close(done)
	}()

	deadline := time.Now().Add(10 * time.Second)
	for !tap.has("subscribe failed") && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if !tap.has("subscribe failed") {
		t.Fatalf("the refused subscription must be reported; saw %v", tap.snapshot())
	}
	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("watchTopic must end with its context")
	}
}

// The status endpoint reports what the mirror holds — counters, the
// per-collection cache sizes that make a parity check one GET away,
// and the audit tail.
func TestServeStatusReportsTheMirror(t *testing.T) {
	m := mirrorTo(t, "http://target:8235")
	m.logger = newRegistryLogTap().logger()
	m.audit, _ = newAuditor("")
	m.audit.event("forward_failed", map[string]any{"topic": "nodes"})
	m.mu.Lock()
	m.started = time.Now()
	m.cache["nodes"][fxNode] = mustJSONBytes(t, validNode(fxNode))
	m.mu.Unlock()

	ln := freePort(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.serveStatus(ctx, ln)

	var body []byte
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := stdhttp.Get("http://" + ln + "/status.json")
		if err == nil {
			body, _ = readAllBody(resp)
			_ = resp.Body.Close()
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(body) == 0 {
		t.Fatal("the status endpoint never answered")
	}
	var status mirrorStatus
	if err := json.Unmarshal(body, &status); err != nil {
		t.Fatalf("decode: %v (%s)", err, body)
	}
	if status.Target != "http://target:8235" {
		t.Errorf("target = %q", status.Target)
	}
	if status.CacheCounts["nodes"] != 1 {
		t.Errorf("cache counts = %v, want the one cached node", status.CacheCounts)
	}
	if len(status.RecentAudit) != 1 {
		t.Errorf("audit tail = %v", status.RecentAudit)
	}
	if status.ServeAddr != "" {
		t.Errorf("serve_addr = %q, want it absent while serving is off", status.ServeAddr)
	}
}

// freePort returns a host:port nothing is listening on.
func freePort(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(stdhttp.NotFoundHandler())
	addr := srv.Listener.Addr().String()
	srv.Close()
	return addr
}

// A target holding none of our nodes is probed back to health with a
// resync rather than heartbeated into a 404 loop — the shape of a
// target that was restarted or wiped underneath us.
func TestHeartbeatLoopProbesAnEmptyTarget(t *testing.T) {
	target := &scriptedTarget{}
	ts := httptest.NewServer(target.handler())
	defer ts.Close()

	m := mirrorTo(t, ts.URL)
	tap := newRegistryLogTap()
	m.logger = tap.logger()
	m.mu.Lock()
	// One cached node the target has never accepted: nothing to
	// heartbeat, everything to re-register.
	m.cache["nodes"][fxNode] = mustJSONBytes(t, validNode(fxNode))
	m.cacheVer["nodes"][fxNode] = "v1.3"
	m.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		m.heartbeatLoop(ctx)
		close(done)
	}()

	deadline := time.Now().Add(3 * MirrorHeartbeatInterval)
	for !tap.has("target holds none of our nodes") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !tap.has("target holds none of our nodes") {
		t.Fatalf("the empty target must be probed; saw %v", tap.snapshot())
	}
	if m.Stats().Resyncs == 0 {
		t.Error("the probe must run a resync")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the heartbeat loop must end with its context")
	}
}

// A target that 404s a heartbeat for a node it accepted has evicted
// us; the mirror re-registers the whole catalogue in dependency order.
func TestHeartbeatLoopResyncsAfterAnEviction(t *testing.T) {
	target := &scriptedTarget{answer: func(method, path string, _ int) (int, string) {
		if method == stdhttp.MethodPost && contains(path, "/health/") {
			return stdhttp.StatusNotFound, ""
		}
		return stdhttp.StatusCreated, ""
	}}
	ts := httptest.NewServer(target.handler())
	defer ts.Close()

	m := mirrorTo(t, ts.URL)
	tap := newRegistryLogTap()
	m.logger = tap.logger()
	m.mu.Lock()
	m.cache["nodes"][fxNode] = mustJSONBytes(t, validNode(fxNode))
	m.cacheVer["nodes"][fxNode] = "v1.3"
	m.targetNodes[fxNode] = true // the target accepted it
	m.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.heartbeatLoop(ctx)

	deadline := time.Now().Add(3 * MirrorHeartbeatInterval)
	for !tap.has("target evicted node") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !tap.has("target evicted node") {
		t.Fatalf("a 404 heartbeat must read as an eviction; saw %v", tap.snapshot())
	}
	if m.Stats().Resyncs == 0 {
		t.Error("an eviction must trigger a full resync")
	}
}
