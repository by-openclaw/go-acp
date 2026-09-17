package registry

import (
	"context"
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// scriptedTarget is a Registration API that answers whatever the test
// tells it to, and records the requests it was sent.
type scriptedTarget struct {
	mu       sync.Mutex
	requests []string // "<METHOD> <path>"

	// answer decides the reply for one request. Returning a Location
	// exercises the version-lock reconciliation.
	answer func(method, path string, n int) (status int, location string)
}

func (s *scriptedTarget) handler() stdhttp.Handler {
	return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		s.mu.Lock()
		s.requests = append(s.requests, r.Method+" "+r.URL.Path)
		n := len(s.requests)
		s.mu.Unlock()

		status, location := stdhttp.StatusCreated, ""
		if s.answer != nil {
			status, location = s.answer(r.Method, r.URL.Path, n)
		}
		if location != "" {
			w.Header().Set("Location", location)
		}
		w.WriteHeader(status)
	})
}

func (s *scriptedTarget) seen() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.requests...)
}

// mirrorTo builds a Mirror pointed at the given target. The source is
// never contacted — these tests drive the forward path directly.
func mirrorTo(t *testing.T, target string) *Mirror {
	t.Helper()
	m, err := NewMirror(MirrorOptions{Source: "http://source.invalid:1", Target: target, APIVer: "v1.3"})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// A registration the target already holds at a different minor is
// answered 409 with a Location naming that minor. The mirror deletes
// the target's copy at ITS minor and re-POSTs once at ours, so the
// target ends holding the resource at its true registered version.
func TestMirrorPostReconcilesAVersionConflict(t *testing.T) {
	target := &scriptedTarget{}
	target.answer = func(method, path string, n int) (int, string) {
		if method == stdhttp.MethodPost && n == 1 {
			return stdhttp.StatusConflict, "http://target/x-nmos/registration/v1.0/resource/nodes/" + fxNode
		}
		if method == stdhttp.MethodDelete {
			return stdhttp.StatusNoContent, ""
		}
		return stdhttp.StatusCreated, ""
	}
	ts := httptest.NewServer(target.handler())
	defer ts.Close()

	m := mirrorTo(t, ts.URL)
	m.postResource(context.Background(), "nodes", "v1.3", fxNode, json.RawMessage(`{"id":"`+fxNode+`"}`), false)

	seen := target.seen()
	if len(seen) != 3 {
		t.Fatalf("requests = %v, want POST, DELETE at the target's minor, POST", seen)
	}
	if !strings.Contains(seen[1], "DELETE") || !strings.Contains(seen[1], "/v1.0/") {
		t.Errorf("second request = %q, want the delete at the minor the target holds", seen[1])
	}
	if !strings.Contains(seen[2], "/v1.3/") {
		t.Errorf("third request = %q, want the re-POST at ours", seen[2])
	}
	if m.Stats().Forwarded != 1 {
		t.Errorf("Forwarded = %d, want the reconciled registration counted once", m.Stats().Forwarded)
	}
}

// A 409 whose Location names our own minor leaves nothing to
// reconcile — it is reported as a failure rather than retried.
func TestMirrorPostConflictWithNothingToReconcile(t *testing.T) {
	target := &scriptedTarget{answer: func(string, string, int) (int, string) {
		return stdhttp.StatusConflict, "http://target/x-nmos/registration/v1.3/resource/nodes/" + fxNode
	}}
	ts := httptest.NewServer(target.handler())
	defer ts.Close()

	m := mirrorTo(t, ts.URL)
	m.postResource(context.Background(), "nodes", "v1.3", fxNode, json.RawMessage(`{}`), false)
	if got := len(target.seen()); got != 1 {
		t.Errorf("requests = %d, want no retry", got)
	}
	if m.Stats().Failures == 0 {
		t.Error("an irreconcilable 409 must be counted as a failure")
	}
}

// A 409 with no usable Location falls back to the primary minor —
// the addressing everything was registered under before the upgrade.
func TestMirrorPostConflictWithoutALocation(t *testing.T) {
	target := &scriptedTarget{}
	target.answer = func(method, _ string, n int) (int, string) {
		if method == stdhttp.MethodPost && n == 1 {
			return stdhttp.StatusConflict, "not a registration path"
		}
		if method == stdhttp.MethodDelete {
			return stdhttp.StatusNoContent, ""
		}
		return stdhttp.StatusCreated, ""
	}
	ts := httptest.NewServer(target.handler())
	defer ts.Close()

	// The mirror's own minor differs from the resource's, so the
	// fallback has somewhere to go.
	m, err := NewMirror(MirrorOptions{Source: "http://source.invalid:1", Target: ts.URL, APIVer: "v1.0"})
	if err != nil {
		t.Fatal(err)
	}
	m.postResource(context.Background(), "nodes", "v1.3", fxNode, json.RawMessage(`{}`), false)

	seen := target.seen()
	if len(seen) != 3 || !strings.Contains(seen[1], "/v1.0/") {
		t.Errorf("requests = %v, want the delete at the primary minor", seen)
	}
}

// Every other refusal is a plain failure. A parent-missing 400 during
// the concurrent initial fill additionally arms the ordered resync —
// but only when the caller allows it, so a resync pass cannot arm
// another.
func TestMirrorPostRefusals(t *testing.T) {
	target := &scriptedTarget{answer: func(string, string, int) (int, string) {
		return stdhttp.StatusBadRequest, ""
	}}
	ts := httptest.NewServer(target.handler())
	defer ts.Close()

	m := mirrorTo(t, ts.URL)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.mu.Lock()
	m.runCtx = ctx // scheduleResync only arms while a run is live
	m.mu.Unlock()

	m.postResource(ctx, "flows", "v1.3", fxFlow, json.RawMessage(`{}`), true)
	m.mu.Lock()
	armed := m.resyncTimer != nil
	m.mu.Unlock()
	if !armed {
		t.Error("a parent-missing 400 during the fill must arm an ordered resync")
	}

	// A resource with no tracked minor is never forwarded with an
	// empty version segment — it is audited and counted.
	before := m.Stats().Failures
	m.postResource(ctx, "flows", "", fxFlow, json.RawMessage(`{}`), false)
	m.deleteResource(ctx, "flows", "", fxFlow)
	if err := m.sendHealth(ctx, fxNode, ""); err == nil {
		t.Error("a heartbeat without a minor must be refused")
	}
	if m.Stats().Failures != before+3 {
		t.Errorf("Failures = %d, want three skipped forwards", m.Stats().Failures-before)
	}
	if got := len(target.seen()); got != 1 {
		t.Errorf("the target saw %d requests; a versionless forward must not reach it", got)
	}

	// A document that cannot be encoded never reaches the wire either.
	before = m.Stats().Failures
	m.postResource(ctx, "flows", "v1.3", fxFlow, json.RawMessage(`{"broken":`), false)
	if m.Stats().Failures == before {
		t.Error("an unencodable document must be counted as a failure")
	}
}

// A delete answered 409 is retried once at the minor the target
// names: what matters is the resource being gone, and the target's
// own addressing is what removes it. 404 is success — the target
// never had it.
func TestMirrorDeleteReconcilesAndTreats404AsDone(t *testing.T) {
	target := &scriptedTarget{}
	target.answer = func(_ string, _ string, n int) (int, string) {
		if n == 1 {
			return stdhttp.StatusConflict, "/x-nmos/registration/v1.1/resource/nodes/" + fxNode
		}
		return stdhttp.StatusNotFound, ""
	}
	ts := httptest.NewServer(target.handler())
	defer ts.Close()

	m := mirrorTo(t, ts.URL)
	m.mu.Lock()
	m.targetNodes[fxNode] = true
	m.mu.Unlock()

	m.deleteResource(context.Background(), "nodes", "v1.3", fxNode)
	seen := target.seen()
	if len(seen) != 2 || !strings.Contains(seen[1], "/v1.1/") {
		t.Fatalf("requests = %v, want a retry at the minor the target holds", seen)
	}
	if m.Stats().Deleted != 1 {
		t.Errorf("Deleted = %d, want the 404 counted as done", m.Stats().Deleted)
	}
	m.mu.Lock()
	_, stillHeartbeated := m.targetNodes[fxNode]
	m.mu.Unlock()
	if stillHeartbeated {
		t.Error("a deleted node must not be heartbeated any more")
	}

	// Any other status is a plain failure, with no retry.
	other := &scriptedTarget{answer: func(string, string, int) (int, string) {
		return stdhttp.StatusInternalServerError, ""
	}}
	ts2 := httptest.NewServer(other.handler())
	defer ts2.Close()
	m2 := mirrorTo(t, ts2.URL)
	m2.deleteResource(context.Background(), "nodes", "v1.3", fxNode)
	if len(other.seen()) != 1 || m2.Stats().Failures == 0 {
		t.Errorf("a 500 delete = %v requests, %d failures", other.seen(), m2.Stats().Failures)
	}
}

// A heartbeat is counted when the target accepts it, reports an
// eviction on 404 so the caller can resync, and is a plain failure on
// anything else.
func TestMirrorSendHealthOutcomes(t *testing.T) {
	status := stdhttp.StatusOK
	var mu sync.Mutex
	ts := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.WriteHeader(status)
	}))
	defer ts.Close()

	m := mirrorTo(t, ts.URL)
	if err := m.sendHealth(context.Background(), fxNode, "v1.3"); err != nil {
		t.Fatalf("healthy heartbeat = %v", err)
	}
	if m.Stats().Heartbeats != 1 {
		t.Errorf("Heartbeats = %d", m.Stats().Heartbeats)
	}

	mu.Lock()
	status = stdhttp.StatusNotFound
	mu.Unlock()
	if err := m.sendHealth(context.Background(), fxNode, "v1.3"); err != errMirrorEvicted {
		t.Errorf("404 heartbeat = %v, want the eviction signal", err)
	}

	mu.Lock()
	status = stdhttp.StatusInternalServerError
	mu.Unlock()
	before := m.Stats().Failures
	if err := m.sendHealth(context.Background(), fxNode, "v1.3"); err == nil {
		t.Error("a 500 heartbeat must report an error")
	}
	if m.Stats().Failures != before+1 {
		t.Error("a failed heartbeat must be counted")
	}

	// A target that cannot be reached at all is a failure, not a panic.
	ts.Close()
	before = m.Stats().Failures
	if err := m.sendHealth(context.Background(), fxNode, "v1.3"); err == nil {
		t.Error("an unreachable target must report an error")
	}
	if m.Stats().Failures != before+1 {
		t.Error("an unreachable target must be counted")
	}
	m.postResource(context.Background(), "nodes", "v1.3", fxNode, json.RawMessage(`{}`), false)
	m.deleteResource(context.Background(), "nodes", "v1.3", fxNode)
	if m.Stats().Failures != before+3 {
		t.Errorf("Failures = %d, want the unreachable POST and DELETE counted too", m.Stats().Failures)
	}
}

// The registration base is derived from the target unless the
// operator pinned an explicit one — their pin outranks per-resource
// fidelity.
func TestMirrorRegistrationBase(t *testing.T) {
	m := mirrorTo(t, "http://target:8235/")
	if got := m.registrationBase("v1.1"); got != "http://target:8235/x-nmos/registration/v1.1" {
		t.Errorf("base = %q", got)
	}
	if got := m.registrationBase(""); got != "http://target:8235/x-nmos/registration/v1.3" {
		t.Errorf("base with no minor = %q, want the primary one", got)
	}

	pinned := mirrorTo(t, "http://target:8235/x-nmos/registration/v1.0")
	if got := pinned.registrationBase("v1.3"); got != "http://target:8235/x-nmos/registration/v1.0" {
		t.Errorf("a pinned base = %q, want it used verbatim", got)
	}
}

// The minor a 409 names is read out of its Location header, in either
// the absolute or the path-only form; anything else reads as unknown.
func TestVerFromLocation(t *testing.T) {
	for loc, want := range map[string]string{
		"http://target:8235/x-nmos/registration/v1.0/resource/nodes/x": "v1.0",
		"/x-nmos/registration/v1.3/resource/nodes/x":                   "v1.3",
		"/x-nmos/registration/v1.2":                                    "v1.2",
		"/x-nmos/query/v1.3/nodes/x":                                   "",
		"/x-nmos/registration/latest/resource/nodes/x":                 "",
		"": "",
	} {
		if got := verFromLocation(loc); got != want {
			t.Errorf("verFromLocation(%q) = %q, want %q", loc, got, want)
		}
	}
}

// The mirror's backoff wait ends with its context — a cancelled run
// never sits out the rest of a retry delay.
func TestMirrorSleepEndsWithTheContext(t *testing.T) {
	m := mirrorTo(t, "http://target:8235")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	m.sleep(ctx, time.Hour)
	if time.Since(start) > 5*time.Second {
		t.Error("sleep must end with its context")
	}
	// And it does wait when the context is live.
	m.sleep(context.Background(), time.Millisecond)
}
