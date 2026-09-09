package provider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"dhs/internal/amwa/codec/is04"
)

// fakeRegistry is a Registration API a test drives: it records every
// POST and DELETE, and each answer is scripted.
type fakeRegistry struct {
	ts *httptest.Server

	mu         sync.Mutex
	posted     []string // "<type>:<id>"
	deleted    []string
	postCode   int  // 201 unless changed
	healthCode int  // 200 unless changed
	postFail   bool // refuse every POST with 500
}

func newFakeRegistry(t *testing.T) *fakeRegistry {
	t.Helper()
	r := &fakeRegistry{postCode: stdhttp.StatusCreated, healthCode: stdhttp.StatusOK}
	mux := stdhttp.NewServeMux()
	mux.HandleFunc("/x-nmos/registration/v1.3/resource", func(w stdhttp.ResponseWriter, req *stdhttp.Request) {
		body, _ := io.ReadAll(io.LimitReader(req.Body, 1<<20))
		var env struct {
			Type string `json:"type"`
			Data struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		_ = json.Unmarshal(body, &env)
		r.mu.Lock()
		r.posted = append(r.posted, env.Type+":"+env.Data.ID)
		code, fail := r.postCode, r.postFail
		r.mu.Unlock()
		if fail {
			w.WriteHeader(stdhttp.StatusInternalServerError)
			_, _ = w.Write([]byte(strings.Repeat("registry refused this resource ", 10)))
			return
		}
		w.WriteHeader(code)
	})
	mux.HandleFunc("/x-nmos/registration/v1.3/resource/", func(w stdhttp.ResponseWriter, req *stdhttp.Request) {
		if req.Method != stdhttp.MethodDelete {
			w.WriteHeader(stdhttp.StatusMethodNotAllowed)
			return
		}
		r.mu.Lock()
		r.deleted = append(r.deleted, strings.TrimPrefix(req.URL.Path, "/x-nmos/registration/v1.3/resource/"))
		r.mu.Unlock()
		w.WriteHeader(stdhttp.StatusNoContent)
	})
	mux.HandleFunc("/x-nmos/registration/v1.3/health/nodes/", func(w stdhttp.ResponseWriter, req *stdhttp.Request) {
		r.mu.Lock()
		code := r.healthCode
		r.mu.Unlock()
		w.WriteHeader(code)
	})
	r.ts = httptest.NewServer(mux)
	t.Cleanup(r.ts.Close)
	return r
}

func (r *fakeRegistry) snapshot() (posted, deleted []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.posted...), append([]string(nil), r.deleted...)
}

func (r *fakeRegistry) set(fn func(*fakeRegistry)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	fn(r)
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// runClient starts the loop on a fast cadence and stops it with the test.
func runClient(t *testing.T, c *RegistrationClient) context.CancelFunc {
	t.Helper()
	c.SetHeartbeatIntervalFn(func() time.Duration { return 100 * time.Millisecond })
	ctx, cancel := context.WithCancel(context.Background())
	go c.Run(ctx)
	t.Cleanup(cancel)
	return cancel
}

// The loop registers the whole bundle in IS-04 dependency order, then
// heartbeats; cancelling it deregisters the Node.
func TestRegistrationRunRegistersAndDeregisters(t *testing.T) {
	reg := newFakeRegistry(t)
	tap := newLogTap()
	c := NewRegistrationClient(tap.logger(), reg.ts.URL, "v1.3", validBundle())
	cancel := runClient(t, c)

	// The flag is set after the last POST of the pass, so waiting on
	// it is what makes the ordering assertions below deterministic —
	// waiting on the POST count alone races the flag.
	waitUntil(t, "the client to consider itself registered", c.registered.Load)
	posted, _ := reg.snapshot()
	if len(posted) < 2 {
		t.Fatalf("posted = %v, want at least the node and a device", posted)
	}
	if posted[0] != "node:"+validBundle().Node.ID {
		t.Errorf("first POST = %q, want the node itself", posted[0])
	}
	if posted[1] != "device:"+validBundle().Devices[0].ID {
		t.Errorf("second POST = %q, want a device", posted[1])
	}

	cancel()
	waitUntil(t, "the node to be deregistered", func() bool {
		_, deleted := reg.snapshot()
		for _, d := range deleted {
			if strings.HasSuffix(d, validBundle().Node.ID) {
				return true
			}
		}
		return false
	})
}

// A Registry that answers 200 to a POST already holds that resource: the
// client deletes and re-posts so the Registry's copy is the Node's.
func TestRegistrationReplacesExistingResource(t *testing.T) {
	reg := newFakeRegistry(t)
	reg.set(func(r *fakeRegistry) { r.postCode = stdhttp.StatusOK })
	c := NewRegistrationClient(nil, reg.ts.URL, "v1.3", validBundle())
	if _, ok := c.pickBase(); !ok {
		t.Fatal("no base")
	}
	if err := c.postResource(context.Background(), is04.ResourceNode, &c.bundle.Node); err != nil {
		t.Fatalf("postResource: %v", err)
	}
	posted, deleted := reg.snapshot()
	if len(posted) != 2 || len(deleted) != 1 {
		t.Errorf("posts=%v deletes=%v, want a replace (post, delete, post)", posted, deleted)
	}
	if !strings.HasSuffix(deleted[0], c.bundle.Node.ID) {
		t.Errorf("deleted %q, want the node", deleted[0])
	}
}

// A Registry that refuses the registration is reported with its own
// answer, truncated so a chatty error cannot flood the log.
func TestRegistrationPostRefused(t *testing.T) {
	reg := newFakeRegistry(t)
	reg.set(func(r *fakeRegistry) { r.postFail = true })
	c := NewRegistrationClient(nil, reg.ts.URL, "v1.3", validBundle())
	if _, ok := c.pickBase(); !ok {
		t.Fatal("no base")
	}
	err := c.postResource(context.Background(), is04.ResourceNode, &c.bundle.Node)
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("refused POST = %v", err)
	}
	if !strings.Contains(err.Error(), "...") {
		t.Errorf("a long refusal must be truncated: %v", err)
	}
	if c.registered.Load() {
		t.Error("a refused registration must not count as registered")
	}

	// registerAll stops at the first refusal.
	if err := c.registerAll(context.Background()); err == nil {
		t.Error("registerAll must report the refusal")
	}
	posted, _ := reg.snapshot()
	if len(posted) != 2 {
		t.Errorf("posted %v, want the node twice and nothing after the refusal", posted)
	}
}

// A Registry that has forgotten this Node (404 on the heartbeat) is
// re-registered from scratch, and one that fails outright is
// disqualified so the failover chain can move on.
func TestRegistrationHeartbeat404ReRegisters(t *testing.T) {
	reg := newFakeRegistry(t)
	tap := newLogTap()
	c := NewRegistrationClient(tap.logger(), reg.ts.URL, "v1.3", validBundle())
	runClient(t, c)
	waitUntil(t, "the initial registration", func() bool {
		posted, _ := reg.snapshot()
		return len(posted) >= 2
	})
	first, _ := reg.snapshot()

	reg.set(func(r *fakeRegistry) { r.healthCode = stdhttp.StatusNotFound })
	tap.wait(t, "heartbeat 404 — re-registering")
	waitUntil(t, "the bundle to be registered again", func() bool {
		posted, _ := reg.snapshot()
		return len(posted) > len(first)
	})
	if atomic.LoadUint64(&c.reregister) == 0 {
		t.Error("a Registry that forgot this Node must count as a re-registration")
	}
}

// A Registry that fails the heartbeat outright is reported, and the Node
// stops considering itself registered so the failover chain can run.
func TestRegistrationHeartbeatFailureUnregisters(t *testing.T) {
	reg := newFakeRegistry(t)
	tap := newLogTap()
	c := NewRegistrationClient(tap.logger(), reg.ts.URL, "v1.3", validBundle())
	runClient(t, c)
	waitUntil(t, "the initial registration", func() bool {
		posted, _ := reg.snapshot()
		return len(posted) >= 2
	})

	reg.set(func(r *fakeRegistry) { r.healthCode = stdhttp.StatusInternalServerError })
	tap.wait(t, "heartbeat failed")
	// The counter is incremented after the line is logged, so waiting
	// on the log and reading the counter in the same breath is a race
	// with the loop's own goroutine.
	waitUntil(t, "the failure to be counted", func() bool {
		return atomic.LoadUint64(&c.failures) > 0
	})
}

// Republish re-POSTs a changed resource while registered, and is dropped
// while not — an unregistered Node has nothing to update.
func TestRegistrationRepublish(t *testing.T) {
	reg := newFakeRegistry(t)
	c := NewRegistrationClient(nil, reg.ts.URL, "v1.3", validBundle())
	runClient(t, c)
	waitUntil(t, "the initial registration", func() bool { return c.registered.Load() })
	before, _ := reg.snapshot()

	c.Republish(is04.ResourceDevice, &c.bundle.Devices[0])
	// The counter is the last step of the republish, so waiting on it also
	// waits for the POST that precedes it.
	waitUntil(t, "the republished device", func() bool {
		return atomic.LoadUint64(&c.reregister) > 0
	})
	if posted, _ := reg.snapshot(); len(posted) <= len(before) {
		t.Errorf("republish counted but nothing was posted (%d, was %d)", len(posted), len(before))
	}
}

// A heartbeat cadence the System API lowers changes the loop's tick, and
// the tick is clamped at both ends: sub-second cadences poll fast, long
// ones no slower than a second.
func TestHeartbeatTickClamps(t *testing.T) {
	cases := map[time.Duration]time.Duration{
		20 * time.Millisecond:  50 * time.Millisecond, // floor
		400 * time.Millisecond: 200 * time.Millisecond,
		5 * time.Second:        time.Second, // ceiling
	}
	for cadence, want := range cases {
		if got := heartbeatTick(cadence); got != want {
			t.Errorf("heartbeatTick(%v) = %v, want %v", cadence, got, want)
		}
	}
}

// A DELETE the Registry refuses is counted as a failure and logged, and
// one it answers 404 to is fine — the resource is already gone.
func TestRegistrationDeleteOutcomes(t *testing.T) {
	var code atomic.Int32
	code.Store(stdhttp.StatusNoContent)
	ts := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		w.WriteHeader(int(code.Load()))
		_, _ = w.Write([]byte("gone wrong"))
	}))
	defer ts.Close()

	tap := newLogTap()
	c := NewRegistrationClient(tap.logger(), ts.URL, "v1.3", validBundle())
	if _, ok := c.pickBase(); !ok {
		t.Fatal("no base")
	}
	c.deleteResource(context.Background(), is04.ResourceNode, "n-1")
	if atomic.LoadUint64(&c.deletions) != 1 {
		t.Error("a 204 must count as a deletion")
	}

	code.Store(stdhttp.StatusNotFound)
	c.deleteResource(context.Background(), is04.ResourceNode, "n-1")
	if atomic.LoadUint64(&c.deletions) != 2 {
		t.Error("a 404 means already gone, which is a deletion")
	}

	code.Store(stdhttp.StatusInternalServerError)
	c.deleteResource(context.Background(), is04.ResourceNode, "n-1")
	if !tap.has("DELETE non-204") {
		t.Errorf("a refused DELETE must be logged; saw %v", tap.snapshot())
	}
	if atomic.LoadUint64(&c.failures) == 0 {
		t.Error("a refused DELETE must count as a failure")
	}

	// A Registry that cannot be reached at all.
	ts.Close()
	c.deleteResource(context.Background(), is04.ResourceNode, "n-1")
	if !tap.has("provider/node: DELETE") {
		t.Error("an unreachable Registry must be logged")
	}
}

// The Bearer token is attached to every request, and a token source that
// fails aborts the request rather than sending it unauthenticated.
func TestRegistrationApplyToken(t *testing.T) {
	seen := make(chan string, 4)
	ts := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		seen <- r.Header.Get("Authorization")
		w.WriteHeader(stdhttp.StatusCreated)
	}))
	defer ts.Close()

	c := NewRegistrationClient(nil, ts.URL, "v1.3", validBundle())
	if _, ok := c.pickBase(); !ok {
		t.Fatal("no base")
	}
	c.SetTokenSource(func(context.Context) (string, error) { return "tok-1", nil })
	if err := c.postResource(context.Background(), is04.ResourceNode, &c.bundle.Node); err != nil {
		t.Fatal(err)
	}
	if got := <-seen; got != "Bearer tok-1" {
		t.Errorf("Authorization = %q", got)
	}

	c.SetTokenSource(func(context.Context) (string, error) { return "", errors.New("no token") })
	err := c.postResource(context.Background(), is04.ResourceNode, &c.bundle.Node)
	if err == nil || !strings.Contains(err.Error(), "obtain access token") {
		t.Errorf("a token source that fails = %v", err)
	}
}

// resourceID reads the id of whichever resource kind it is handed, and
// says nothing about one it does not know.
func TestResourceID(t *testing.T) {
	b := validBundle()
	cases := map[is04.ResourceType]struct {
		data any
		want string
	}{
		is04.ResourceNode:   {&b.Node, b.Node.ID},
		is04.ResourceDevice: {&b.Devices[0], b.Devices[0].ID},
		is04.ResourceSource: {&is04.Source{ResourceCore: is04.ResourceCore{ID: "src"}}, "src"},
		is04.ResourceFlow:   {&is04.Flow{ResourceCore: is04.ResourceCore{ID: "flw"}}, "flw"},
		is04.ResourceSender: {&is04.Sender{ResourceCore: is04.ResourceCore{ID: "snd"}}, "snd"},
	}
	for typ, tc := range cases {
		if got := resourceID(typ, tc.data); got != tc.want {
			t.Errorf("resourceID(%s) = %q, want %q", typ, got, tc.want)
		}
	}
	if got := resourceID(is04.ResourceNode, "not a resource"); got != "" {
		t.Errorf("an unknown payload = %q, want empty", got)
	}
}
