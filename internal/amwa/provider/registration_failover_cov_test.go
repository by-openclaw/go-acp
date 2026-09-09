package provider

// Registration under failure: a dependency chain that stops at the
// first refusal, a stale entry that will not be replaced, and the
// IS-04 §6.1 failover semantics AMWA test_15 and test_16 check.

import (
	"context"
	"encoding/json"
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

// typedRegistry answers POSTs per resource type and per attempt, so a
// test can fail exactly one step of the dependency chain.
type typedRegistry struct {
	ts *httptest.Server

	mu       sync.Mutex
	byType   map[string]int // type -> status to answer
	attempts map[string]int // type -> POSTs seen
	second   map[string]int // type -> status for the second attempt on
	health   int
	beats    int
	deletes  int
	blockHB  chan struct{}
}

func newTypedRegistry(t *testing.T) *typedRegistry {
	t.Helper()
	r := &typedRegistry{
		byType: map[string]int{}, attempts: map[string]int{},
		second: map[string]int{}, health: stdhttp.StatusOK,
	}
	mux := stdhttp.NewServeMux()
	mux.HandleFunc("/x-nmos/registration/v1.3/resource", func(w stdhttp.ResponseWriter, req *stdhttp.Request) {
		body, _ := io.ReadAll(io.LimitReader(req.Body, 1<<20))
		var env struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(body, &env)
		r.mu.Lock()
		r.attempts[env.Type]++
		code := r.byType[env.Type]
		if n, ok := r.second[env.Type]; ok && r.attempts[env.Type] > 1 {
			code = n
		}
		r.mu.Unlock()
		if code == 0 {
			code = stdhttp.StatusCreated
		}
		w.WriteHeader(code)
	})
	mux.HandleFunc("/x-nmos/registration/v1.3/resource/", func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		r.mu.Lock()
		r.deletes++
		r.mu.Unlock()
		w.WriteHeader(stdhttp.StatusNoContent)
	})
	mux.HandleFunc("/x-nmos/registration/v1.3/health/nodes/", func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		r.mu.Lock()
		r.beats++
		block, code := r.blockHB, r.health
		r.mu.Unlock()
		if block != nil {
			<-block
		}
		w.WriteHeader(code)
	})
	r.ts = httptest.NewServer(mux)
	t.Cleanup(r.ts.Close)
	return r
}

func (r *typedRegistry) set(fn func(*typedRegistry)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	fn(r)
}

func (r *typedRegistry) seen(typ string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.attempts[typ]
}

func (r *typedRegistry) deregistrations() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.deletes
}

func (r *typedRegistry) heartbeats() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.beats
}

// Registration is ordered by dependency — Sources before Flows, Flows
// before Senders — and a refusal anywhere stops the chain. Posting a
// Sender whose Flow the Registry rejected would leave the Query API
// describing a stream with no flow behind it.
func TestRegisterAllStopsAtTheFirstRefusal(t *testing.T) {
	// Each entry names the type that is refused and the one that comes
	// after it in IS-04 dependency order, which must therefore never be
	// posted.
	for _, tc := range []struct{ refused, next string }{
		{"source", "flow"},
		{"flow", "sender"},
		{"sender", "receiver"},
		{"receiver", ""},
	} {
		t.Run(tc.refused, func(t *testing.T) {
			reg := newTypedRegistry(t)
			reg.set(func(r *typedRegistry) { r.byType[tc.refused] = stdhttp.StatusInternalServerError })

			c := NewRegistrationClient(newLogTap().logger(), reg.ts.URL, "v1.3", fullBundle(t))
			if err := c.registerAll(context.Background()); err == nil {
				t.Fatalf("a refused %s must stop the chain", tc.refused)
			}
			if c.registered.Load() {
				t.Error("a chain that did not complete is not a registration")
			}
			if tc.next != "" && reg.seen(tc.next) != 0 {
				t.Errorf("%s was posted after %s was refused", tc.next, tc.refused)
			}
		})
	}
}

// IS-04 §4: a 200 means the Registry already holds this UUID from a
// session nobody deregistered. The Node DELETEs the stale entry and
// re-POSTs — and when that second POST is refused, the refusal is the
// answer rather than a registration nobody made.
func TestStaleRegistrationThatWillNotBeReplaced(t *testing.T) {
	reg := newTypedRegistry(t)
	reg.set(func(r *typedRegistry) {
		r.byType["node"] = stdhttp.StatusOK                  // stale entry
		r.second["node"] = stdhttp.StatusInternalServerError // and the retry fails
	})

	c := NewRegistrationClient(newLogTap().logger(), reg.ts.URL, "v1.3", validBundle())
	if err := c.registerAll(context.Background()); err == nil {
		t.Fatal("want the refused re-POST reported")
	}
	if reg.seen("node") != 2 {
		t.Errorf("the Node was POSTed %d times, want the stale entry replaced once", reg.seen("node"))
	}
}

// A resource this wire minor cannot express is not posted: the
// Registry would store something the Node cannot then serve, and the
// two faces would disagree about the same UUID.
func TestPostRefusesAResourceItCannotEncode(t *testing.T) {
	c := NewRegistrationClient(newLogTap().logger(), "http://registry.invalid:8235", "v1.3", validBundle())
	broken := c.bundle.Node
	broken.ID = "not-a-uuid"

	if _, err := c.postResourceOnce(context.Background(), is04.ResourceNode, &broken); err == nil {
		t.Fatal("want the encode refusal reported")
	}
}

// A watcher that has lost sight of every Registry is not a reason to
// abandon the one we are registered with: there is nothing better to
// switch to.
func TestNoSwitchWhenTheWatcherSeesNothing(t *testing.T) {
	src := &scriptedRegistries{}
	c := NewRegistrationClient(newLogTap().logger(), "", "v1.3", validBundle())
	c.SetWatcher(src)
	c.mu.Lock()
	c.currentRegistry = "a._nmos-register._tcp.local"
	c.mu.Unlock()

	if c.shouldSwitchToBetter() {
		t.Fatal("an empty watcher offers nothing better")
	}
}

// IS-04 §6.1 / AMWA test_16: on failover the Node heartbeats the new
// Registry first and only re-POSTs when that returns 404. A Node that
// re-registers everything on every failover fails the test outright.
func TestFailoverHeartbeatsBeforeReRegistering(t *testing.T) {
	reg := newTypedRegistry(t)
	c := NewRegistrationClient(newLogTap().logger(), reg.ts.URL, "v1.3", validBundle())
	runClient(t, c)
	waitUntil(t, "the initial registration", c.registered.Load)

	// The Registry forgets us: the heartbeat 404s, and the loop
	// rejoins through the heartbeat-first path.
	reg.set(func(r *typedRegistry) { r.health = stdhttp.StatusNotFound })
	waitUntil(t, "the 404 to be noticed", func() bool {
		return c.Stats()["reregister"] > 0
	})

	reg.set(func(r *typedRegistry) { r.health = stdhttp.StatusOK })
	waitUntil(t, "the rejoin", c.registered.Load)

	// A second failover is the one test_16 is really about: by now the
	// Node has registered before, so it probes with a heartbeat and
	// only re-POSTs if that 404s — it must not blindly re-register.
	posts := reg.seen("node")
	reg.set(func(r *typedRegistry) { r.health = stdhttp.StatusNotFound })
	waitUntil(t, "the second 404", func() bool { return c.Stats()["reregister"] > 1 })
	reg.set(func(r *typedRegistry) { r.health = stdhttp.StatusOK })
	waitUntil(t, "the second rejoin", c.registered.Load)
	if reg.seen("node") != posts {
		t.Errorf("the Node re-POSTed on failover (%d -> %d); a heartbeat was enough",
			posts, reg.seen("node"))
	}
}

// A cascade with nothing left to cascade to stops rather than
// spinning: every advertised Registry has been disqualified, and the
// next tick will look again.
func TestCascadeStopsWhenNothingIsLeft(t *testing.T) {
	failing := newTypedRegistry(t)
	src := &scriptedRegistries{candidates: []RegistryCandidate{{
		FullName: "failing._nmos-register._tcp.local", URL: failing.ts.URL,
		APIVer: "v1.3", APIProto: "http",
	}}}
	c := NewRegistrationClient(newLogTap().logger(), "", "v1.3", validBundle())
	c.SetWatcher(src)
	runClient(t, c)
	waitUntil(t, "the first registration", c.registered.Load)

	failing.set(func(r *typedRegistry) { r.health = stdhttp.StatusServiceUnavailable })
	waitUntil(t, "the only Registry to be disqualified", func() bool {
		return src.refusedCount() > 0 && !c.registered.Load()
	})
}

// The heartbeat cadence is live: IS-09 can hand a Node a new
// heartbeat_interval at any time, and the loop retimes itself rather
// than beating at the cadence it started with.
func TestHeartbeatCadenceIsRetimedLive(t *testing.T) {
	reg := newTypedRegistry(t)
	c := NewRegistrationClient(newLogTap().logger(), reg.ts.URL, "v1.3", validBundle())

	var slow atomic.Bool
	c.SetHeartbeatIntervalFn(func() time.Duration {
		if slow.Load() {
			return 5 * time.Second
		}
		return 100 * time.Millisecond
	})
	ctx, cancel := context.WithCancel(context.Background())
	go c.Run(ctx)
	t.Cleanup(cancel)

	waitUntil(t, "the first heartbeats", func() bool { return reg.heartbeats() > 1 })
	slow.Store(true)
	waitUntil(t, "the loop to retime itself", func() bool {
		before := reg.heartbeats()
		time.Sleep(300 * time.Millisecond)
		return reg.heartbeats() == before
	})
}

// A heartbeat the Registry refuses cascades to the next-best one
// without DELETEing anything: several real Registries share one
// resource map, and a stray DELETE there would remove the Node from
// the very view it is failing over into.
func TestHeartbeatFailureCascadesToTheNextRegistry(t *testing.T) {
	failing, working := newTypedRegistry(t), newTypedRegistry(t)

	src := &scriptedRegistries{candidates: []RegistryCandidate{{
		FullName: "failing._nmos-register._tcp.local", URL: failing.ts.URL,
		APIVer: "v1.3", APIProto: "http",
	}}}
	c := NewRegistrationClient(newLogTap().logger(), "", "v1.3", validBundle())
	c.SetWatcher(src)
	runClient(t, c)
	waitUntil(t, "the first registration", c.registered.Load)

	// The one we are on starts refusing heartbeats, and another is
	// advertised behind it.
	failing.set(func(r *typedRegistry) { r.health = stdhttp.StatusServiceUnavailable })
	src.set(func(s *scriptedRegistries) {
		s.candidates = append(s.candidates, RegistryCandidate{
			FullName: "working._nmos-register._tcp.local", URL: working.ts.URL,
			APIVer: "v1.3", APIProto: "http",
		})
	})

	// IS-04 §6.1: the new Registry is heartbeated first, not
	// re-POSTed, so what proves the cascade arrived is a heartbeat.
	waitUntil(t, "the cascade to reach the working Registry", func() bool {
		return working.heartbeats() > 0
	})
}

// A shutdown that arrives while a heartbeat is in flight is our own,
// not the Registry's: the Node goes straight to graceful
// deregistration rather than treating its own cancelled request as a
// Registry failure and skipping every shutdown DELETE.
func TestShutdownDuringAHeartbeatStillDeregisters(t *testing.T) {
	reg := newTypedRegistry(t)
	block := make(chan struct{})
	c := NewRegistrationClient(newLogTap().logger(), reg.ts.URL, "v1.3", validBundle())
	cancel := runClient(t, c)
	waitUntil(t, "the initial registration", c.registered.Load)

	reg.set(func(r *typedRegistry) { r.blockHB = block })
	time.Sleep(200 * time.Millisecond) // let the loop reach the blocked heartbeat
	cancel()
	close(block)

	waitUntil(t, "the graceful deregistration", func() bool { return !c.registered.Load() })
}

// Close waits for the shutdown DELETEs, but not forever: a Registry
// that has stopped answering must not hold a Node's process open.
func TestCloseGivesUpOnADeregistrationThatNeverFinishes(t *testing.T) {
	prev := deregisterWait
	deregisterWait = 10 * time.Millisecond
	t.Cleanup(func() { deregisterWait = prev })

	c := NewRegistrationClient(newLogTap().logger(), "http://registry.invalid:8235", "v1.3", validBundle())
	// Never Run, so the closed signal never arrives — the same state
	// as a deregistration that will not finish.
	if err := c.Close(); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("= %v, want the timeout reported", err)
	}
}
