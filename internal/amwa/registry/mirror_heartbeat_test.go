package registry

// Heartbeats keep their cadence whatever else the mirror is doing
// (#1311). A fill or a repair takes as long as the catalogue is large;
// a target expires a node that stays silent for its window. These
// tests pit one against the other: a paced pass that outlasts the
// target's expiry several times over.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// expiringTarget is a registration face with a real heartbeat window:
// a node that goes unheartbeated for expiry is evicted with everything
// under it, and a device whose node it does not hold is refused (400).
type expiringTarget struct {
	expiry time.Duration

	mu        sync.Mutex
	nodes     map[string]time.Time // node id -> last sign of life
	devices   map[string]string    // device id -> node id
	evictions []string
	refused   []string
	posted    []string               // every id POSTed, in arrival order
	beats     map[string][]time.Time // accepted heartbeats per node
}

func newExpiringTarget(expiry time.Duration) *expiringTarget {
	return &expiringTarget{
		expiry:  expiry,
		nodes:   map[string]time.Time{},
		devices: map[string]string{},
		beats:   map[string][]time.Time{},
	}
}

// sweep evicts every node whose window ran out. Caller holds mu.
func (e *expiringTarget) sweep(now time.Time) {
	for id, seen := range e.nodes {
		if now.Sub(seen) <= e.expiry {
			continue
		}
		delete(e.nodes, id)
		e.evictions = append(e.evictions, id)
		for dev, node := range e.devices {
			if node == id {
				delete(e.devices, dev)
			}
		}
	}
}

func (e *expiringTarget) handler() stdhttp.Handler {
	return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		now := time.Now()
		e.mu.Lock()
		defer e.mu.Unlock()
		e.sweep(now)
		switch {
		case r.Method == stdhttp.MethodPost && strings.HasSuffix(r.URL.Path, "/resource"):
			body, _ := io.ReadAll(r.Body)
			var env struct {
				Type string `json:"type"`
				Data struct {
					ID     string `json:"id"`
					NodeID string `json:"node_id"`
				} `json:"data"`
			}
			_ = json.Unmarshal(body, &env)
			e.posted = append(e.posted, env.Data.ID)
			if env.Type == "node" {
				e.nodes[env.Data.ID] = now
				w.WriteHeader(stdhttp.StatusCreated)
				return
			}
			if _, held := e.nodes[env.Data.NodeID]; !held {
				e.refused = append(e.refused, env.Data.ID)
				w.WriteHeader(stdhttp.StatusBadRequest)
				return
			}
			e.devices[env.Data.ID] = env.Data.NodeID
			w.WriteHeader(stdhttp.StatusCreated)
		case r.Method == stdhttp.MethodPost && strings.Contains(r.URL.Path, "/health/nodes/"):
			id := r.URL.Path[strings.LastIndexByte(r.URL.Path, '/')+1:]
			if _, held := e.nodes[id]; !held {
				w.WriteHeader(stdhttp.StatusNotFound)
				return
			}
			e.nodes[id] = now
			e.beats[id] = append(e.beats[id], now)
			w.WriteHeader(stdhttp.StatusOK)
		default:
			w.WriteHeader(stdhttp.StatusNotFound)
		}
	})
}

// held reports how many devices the target holds, and what went wrong.
func (e *expiringTarget) held() (devices int, evictions, refused []string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.devices), append([]string(nil), e.evictions...), append([]string(nil), e.refused...)
}

// widestGap is the longest silence between two accepted heartbeats of
// one node inside [from, to].
func (e *expiringTarget) widestGap(node string, from, to time.Time) time.Duration {
	e.mu.Lock()
	defer e.mu.Unlock()
	last, widest := from, time.Duration(0)
	for _, at := range e.beats[node] {
		if at.Before(from) || at.After(to) {
			continue
		}
		if gap := at.Sub(last); gap > widest {
			widest = gap
		}
		last = at
	}
	if gap := to.Sub(last); gap > widest {
		widest = gap
	}
	return widest
}

// The pace below spreads a pass over about two and a half seconds:
// twenty requests leave at once (the bucket's burst), the rest at
// twenty a second.
const (
	slowPassPace    = 20
	slowPassDevices = 70
	testHeartbeat   = 50 * time.Millisecond
	testExpiry      = time.Second
)

func deviceDocs(node string, n int) []string {
	docs := make([]string, 0, n)
	for i := 0; i < n; i++ {
		docs = append(docs, fmt.Sprintf(`{"id":"dev-%03d","node_id":%q}`, i, node))
	}
	return docs
}

// The first fill of a large plant outlasts the target's window. The
// node it lands first must be heartbeated while its devices follow —
// otherwise the target evicts it mid-fill and refuses the rest.
func TestMirrorHeartbeatsWhileItFills(t *testing.T) {
	target := newExpiringTarget(testExpiry)
	tsrv := httptest.NewServer(target.handler())
	defer tsrv.Close()

	docs := map[string][]string{
		"nodes":   {`{"id":"n1"}`},
		"devices": deviceDocs("n1", slowPassDevices),
	}
	plant := &fakePlant{}
	src := httptest.NewServer(stdhttp.NotFoundHandler())
	defer src.Close()
	ws := plant.sourceHandler(t, func() string { return src.URL }, nil)
	src.Config.Handler = stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if r.Method == stdhttp.MethodGet && strings.Contains(r.URL.Path, "/x-nmos/query/") {
			parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
			topic, ver := parts[len(parts)-1], parts[len(parts)-2]
			w.Header().Set("Content-Type", "application/json")
			if ver != "v1.3" {
				_, _ = io.WriteString(w, "[]")
				return
			}
			_, _ = io.WriteString(w, "["+strings.Join(docs[topic], ",")+"]")
			return
		}
		ws.ServeHTTP(w, r)
	})

	m, err := NewMirror(MirrorOptions{Source: src.URL, Target: tsrv.URL, APIVer: "v1.3", TargetPace: slowPassPace})
	if err != nil {
		t.Fatal(err)
	}
	m.heartbeatEvery = testHeartbeat
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := time.Now()
	go func() { _ = m.Run(ctx) }()

	waitFor(t, 15*time.Second, func() bool {
		n, _, _ := target.held()
		return n == slowPassDevices
	}, "the fill to land every device")
	if took := time.Since(started); took < 2*testExpiry {
		t.Fatalf("the fill took %v — it must outlast the target's %v window for this test to mean anything", took, testExpiry)
	}
	_, evictions, refused := target.held()
	if len(evictions) != 0 || len(refused) != 0 {
		t.Errorf("during the fill the target evicted %v and refused %d device(s) — the node went unheartbeated", evictions, len(refused))
	}
	if st := m.Stats(); st.Failures != 0 || st.Resyncs != 0 || st.Heartbeats == 0 {
		t.Errorf("stats = %+v, want heartbeats, no failure, no resync", st)
	}
}

// evictOnce makes the target drop one node the first time that node is
// heartbeated, as a target that lost it would, then defers to it.
func evictOnce(target *expiringTarget, node string) (stdhttp.Handler, func() time.Time) {
	var mu sync.Mutex
	var at time.Time
	h := target.handler()
	handler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if strings.HasSuffix(r.URL.Path, "/health/nodes/"+node) {
			mu.Lock()
			first := at.IsZero()
			if first {
				at = time.Now()
			}
			mu.Unlock()
			if first {
				target.mu.Lock()
				delete(target.nodes, node)
				target.mu.Unlock()
			}
		}
		h.ServeHTTP(w, r)
	})
	return handler, func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return at
	}
}

// One node is evicted; re-registering what hung under it takes longer than
// the target's window. The nodes the target still holds must be
// heartbeated throughout — a loop that waits for the repair lets them
// expire and the repair never ends.
func TestMirrorHeartbeatsWhileItRepairs(t *testing.T) {
	target := newExpiringTarget(testExpiry)
	target.nodes["a"] = time.Now()
	target.nodes["b"] = time.Now()
	h, evictedAt := evictOnce(target, "a")
	tsrv := httptest.NewServer(h)
	defer tsrv.Close()

	m, err := NewMirror(MirrorOptions{Source: "http://source.invalid:1", Target: tsrv.URL, APIVer: "v1.3", TargetPace: slowPassPace})
	if err != nil {
		t.Fatal(err)
	}
	m.logger = newRegistryLogTap().logger()
	m.heartbeatEvery = testHeartbeat
	m.mu.Lock()
	for _, id := range []string{"a", "b"} {
		m.cache["nodes"][id] = json.RawMessage(fmt.Sprintf(`{"id":%q}`, id))
		m.cacheVer["nodes"][id] = "v1.3"
		m.targetNodes[id] = true
	}
	for i, doc := range deviceDocs("a", slowPassDevices) {
		id := fmt.Sprintf("dev-%03d", i)
		m.cache["devices"][id] = json.RawMessage(doc)
		m.cacheVer["devices"][id] = "v1.3"
	}
	m.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		m.heartbeatLoop(ctx)
		close(done)
	}()

	waitFor(t, 15*time.Second, func() bool {
		n, _, _ := target.held()
		return n == slowPassDevices
	}, "the repair to land every device")
	repaired := time.Now()
	if took := repaired.Sub(evictedAt()); took < 2*testExpiry {
		t.Fatalf("the repair took %v — it must outlast the target's %v window for this test to mean anything", took, testExpiry)
	}
	if gap := target.widestGap("b", evictedAt(), repaired); gap > testExpiry/2 {
		t.Errorf("node b went %v without a heartbeat while node a was repaired — heartbeats must not wait for a repair", gap)
	}
	_, evictions, refused := target.held()
	if len(evictions) != 0 || len(refused) != 0 {
		t.Errorf("during the repair the target evicted %v and refused %d device(s)", evictions, len(refused))
	}
	// The evicted node is the target's again, and heartbeated again.
	waitFor(t, 5*time.Second, func() bool {
		target.mu.Lock()
		defer target.mu.Unlock()
		return len(target.beats["a"]) > 0
	}, "node a to be heartbeated after its re-registration")
	if st := m.Stats(); st.Resyncs != 1 {
		t.Errorf("resyncs = %d, want the one repair of the one eviction", st.Resyncs)
	}
	// Only what the target lost was sent again: node a and its devices.
	target.mu.Lock()
	posted := append([]string(nil), target.posted...)
	target.mu.Unlock()
	if len(posted) != 1+slowPassDevices || posted[0] != "a" {
		t.Errorf("%d POSTs starting with %q, want node a then its %d devices", len(posted), posted[0], slowPassDevices)
	}
	for _, id := range posted {
		if id == "b" {
			t.Error("node b was re-registered: the target never lost it")
		}
	}

	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the heartbeat loop must end with its context, repair included")
	}
}

// holdingTarget accepts every POST but holds the first one until told.
type holdingTarget struct {
	mu      sync.Mutex
	posts   int
	release chan struct{}
	once    sync.Once
}

func (h *holdingTarget) letGo() { h.once.Do(func() { close(h.release) }) }

func (h *holdingTarget) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.posts
}

func (h *holdingTarget) handler() stdhttp.Handler {
	return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		h.mu.Lock()
		h.posts++
		first := h.posts == 1
		h.mu.Unlock()
		if first {
			<-h.release
		}
		w.WriteHeader(stdhttp.StatusCreated)
	})
}

// fillingMirror is a mirror with one cached node whose first pass is
// stuck in the target, plus the handles that free it and tell when it
// has ended.
func fillingMirror(ctx context.Context, t *testing.T) (*Mirror, *holdingTarget, chan struct{}) {
	t.Helper()
	target := &holdingTarget{release: make(chan struct{})}
	tsrv := httptest.NewServer(target.handler())
	t.Cleanup(tsrv.Close)
	t.Cleanup(target.letGo) // runs first: a failed test must not leave Close waiting

	m := mirrorTo(t, tsrv.URL)
	m.logger = newRegistryLogTap().logger()
	m.mu.Lock()
	m.cache["nodes"]["n1"] = json.RawMessage(`{"id":"n1"}`)
	m.cacheVer["nodes"]["n1"] = "v1.3"
	m.mu.Unlock()

	passed := make(chan struct{})
	go func() {
		m.fill(ctx, "initial_fill")
		close(passed)
	}()
	waitFor(t, 5*time.Second, func() bool { return target.count() == 1 }, "the first pass to reach the target")
	return m, target, passed
}

// Fills never overlap. Repairs asked for while one runs come back at
// once and are folded into ONE more pass after it.
func TestMirrorFoldsRepairsAskedForDuringAFill(t *testing.T) {
	m, target, passed := fillingMirror(context.Background(), t)

	asked := make(chan struct{})
	go func() {
		m.resync(context.Background())
		m.resync(context.Background())
		close(asked)
	}()
	select {
	case <-asked:
	case <-time.After(5 * time.Second):
		t.Fatal("a repair asked for during a fill must not wait for it")
	}
	if got := target.count(); got != 1 {
		t.Fatalf("%d requests reached the target while the first pass was held, want 1", got)
	}

	target.letGo()
	select {
	case <-passed:
	case <-time.After(5 * time.Second):
		t.Fatal("the fill must end once the target answers")
	}
	if got := target.count(); got != 2 {
		t.Errorf("%d POSTs, want 2 — the first pass and one more for the two repairs asked for", got)
	}
	if st := m.Stats(); st.Resyncs != 2 {
		t.Errorf("resyncs = %d, want both requests counted", st.Resyncs)
	}
	// The mirror is idle again: the next repair runs.
	m.resync(context.Background())
	if got := target.count(); got != 3 {
		t.Errorf("%d POSTs after a repair on an idle mirror, want 3", got)
	}
}

// A mirror that is stopping does not start the pass it owes.
func TestMirrorDropsTheOwedPassWhenItStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	m, target, passed := fillingMirror(ctx, t)

	m.resync(ctx) // owed
	cancel()
	target.letGo()
	select {
	case <-passed:
	case <-time.After(5 * time.Second):
		t.Fatal("the fill must end with its context")
	}
	if got := target.count(); got != 1 {
		t.Errorf("%d POSTs, want 1 — no pass after the mirror stopped", got)
	}
	m.mu.Lock()
	filling, owed := m.filling, !m.owed.empty()
	m.mu.Unlock()
	if filling || owed {
		t.Errorf("filling=%v owed=%v after the stop, want both clear", filling, owed)
	}
}
