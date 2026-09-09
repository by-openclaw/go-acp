package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"dhs/internal/amwa/codec/is04"
	amwahttp "dhs/internal/amwa/session/http"
	"dhs/internal/amwa/session/query"
)

// A URL the http package cannot even build a request for is reported
// against the operation that tried, not swallowed: every forward verb
// answers the same way.
func TestMirrorForwardsRefuseAnUnbuildableRequest(t *testing.T) {
	m := mirrorTo(t, "http://tar\x7fget:8235") // a control byte: no URL
	tap := newRegistryLogTap()
	m.logger = tap.logger()
	ctx := context.Background()

	m.postResource(ctx, "nodes", "v1.3", fxNode, json.RawMessage(`{}`), false)
	m.deleteResource(ctx, "nodes", "v1.3", fxNode)
	if err := m.sendHealth(ctx, fxNode, "v1.3"); err == nil {
		t.Error("a heartbeat to an unbuildable URL must report an error")
	}
	if m.Stats().Failures != 3 {
		t.Errorf("Failures = %d, want one per refused build", m.Stats().Failures)
	}
	for _, want := range []string{"build POST", "build DELETE", "build health"} {
		if !tap.has(want) {
			t.Errorf("no report mentioning %q; saw %v", want, tap.snapshot())
		}
	}
}

// A version conflict on the live forward path arms the ordered resync
// as well as reconciling: a node delete cascades on the target, so the
// children it swept have to be re-POSTed.
func TestMirrorPostConflictArmsAResync(t *testing.T) {
	target := &scriptedTarget{}
	target.answer = func(method string, _ string, n int) (int, string) {
		if method == stdhttp.MethodPost && n == 1 {
			return stdhttp.StatusConflict, "/x-nmos/registration/v1.0/resource/nodes/" + fxNode
		}
		if method == stdhttp.MethodDelete {
			return stdhttp.StatusNoContent, ""
		}
		return stdhttp.StatusCreated, ""
	}
	ts := httptest.NewServer(target.handler())
	defer ts.Close()

	m := mirrorTo(t, ts.URL)
	m.logger = newRegistryLogTap().logger()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.mu.Lock()
	m.runCtx = ctx
	m.mu.Unlock()

	m.postResource(ctx, "nodes", "v1.3", fxNode, json.RawMessage(`{"id":"`+fxNode+`"}`), true)
	m.mu.Lock()
	armed := m.resyncTimer != nil
	m.mu.Unlock()
	if !armed {
		t.Error("a reconciled conflict must arm the ordered resync")
	}

	// Arming again extends the same timer rather than starting a
	// second pass.
	m.scheduleResync()
	m.mu.Lock()
	still := m.resyncTimer != nil
	m.mu.Unlock()
	if !still {
		t.Error("a second arm must keep the debounced pass")
	}
}

// A delete answered 409 with nothing to reconcile — no Location, or
// one naming the minor we already used — is a failure, not a retry
// loop.
func TestMirrorDeleteConflictWithNothingToReconcile(t *testing.T) {
	for name, location := range map[string]string{
		"no Location at all":              "",
		"a Location naming our own minor": "/x-nmos/registration/v1.3/resource/nodes/" + fxNode,
	} {
		t.Run(name, func(t *testing.T) {
			target := &scriptedTarget{answer: func(string, string, int) (int, string) {
				return stdhttp.StatusConflict, location
			}}
			ts := httptest.NewServer(target.handler())
			defer ts.Close()

			m := mirrorTo(t, ts.URL) // its primary minor is v1.3
			m.logger = newRegistryLogTap().logger()
			m.deleteResource(context.Background(), "nodes", "v1.3", fxNode)

			if got := len(target.seen()); got != 1 {
				t.Errorf("requests = %d, want no retry", got)
			}
			if m.Stats().Failures == 0 {
				t.Error("an irreconcilable conflict must be counted")
			}
		})
	}
}

// The ordered resync stops when its context does — a cancelled run
// must not keep POSTing a catalogue nobody is waiting for.
func TestMirrorResyncStopsWithItsContext(t *testing.T) {
	target := &scriptedTarget{}
	ts := httptest.NewServer(target.handler())
	defer ts.Close()

	m := mirrorTo(t, ts.URL)
	m.logger = newRegistryLogTap().logger()
	m.mu.Lock()
	m.cache["nodes"] = map[string]json.RawMessage{
		fxNode: mustJSONBytes(t, validNode(fxNode)),
	}
	m.cacheVer["nodes"] = map[string]string{fxNode: "v1.3"}
	m.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m.resync(ctx)
	if got := len(target.seen()); got != 0 {
		t.Errorf("a cancelled resync sent %d requests", got)
	}
}

// mirrorSource is a fake source Registry: it answers the paged REST
// reads the resync refresh makes, and hands out WebSocket hrefs whose
// sockets it can close on demand.
type mirrorSource struct {
	mu       sync.Mutex
	docs     map[string][]json.RawMessage // by topic
	closeWS  bool                         // close each socket right after accepting
	url      func() string
	upgrades int
}

func (s *mirrorSource) handler(t *testing.T) stdhttp.Handler {
	t.Helper()
	return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		switch {
		case r.Method == stdhttp.MethodPost && strings.HasSuffix(r.URL.Path, "/subscriptions"):
			var req struct {
				ResourcePath string `json:"resource_path"`
			}
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &req)
			topic := strings.Trim(req.ResourcePath, "/")
			ws := strings.Replace(s.url(), "http://", "ws://", 1) + "/ws/" + topic
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(stdhttp.StatusCreated)
			_, _ = fmt.Fprintf(w, `{"id":"sub-%s","ws_href":"%s","resource_path":"/%s"}`, topic, ws, topic)

		case strings.HasPrefix(r.URL.Path, "/ws/"):
			ws, err := amwahttp.AcceptWebSocket(w, r)
			if err != nil {
				return
			}
			s.mu.Lock()
			s.upgrades++
			closeIt := s.closeWS
			s.mu.Unlock()
			if closeIt {
				_ = ws.Close()
				return
			}
			for {
				if _, err := ws.ReadText(); err != nil {
					return
				}
			}

		default:
			// A paged collection read: /x-nmos/query/<ver>/<topic>.
			parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
			topic := parts[len(parts)-1]
			s.mu.Lock()
			docs := s.docs[topic]
			s.mu.Unlock()
			if docs == nil {
				docs = []json.RawMessage{}
			}
			w.Header().Set("Content-Type", "application/json")
			raw, err := json.Marshal(docs)
			if err != nil {
				stdhttp.Error(w, err.Error(), stdhttp.StatusInternalServerError)
				return
			}
			_, _ = w.Write(raw)
		}
	})
}

// newMirrorSource starts a fake source and returns it with its URL.
func newMirrorSource(t *testing.T) (*mirrorSource, string) {
	t.Helper()
	src := &mirrorSource{docs: map[string][]json.RawMessage{}}
	ts := httptest.NewServer(src.handler(t))
	t.Cleanup(ts.Close)
	src.url = func() string { return ts.URL }
	return src, ts.URL
}

// buildSourceClients gives a Mirror the per-minor Query clients Run
// would have built, so the refresh path can be driven without a run.
func buildSourceClients(t *testing.T, m *Mirror) {
	t.Helper()
	clients := map[string]*query.Client{}
	for _, ver := range mirrorSourceVersions(m.opts.APIVer) {
		codec, ok := is04.Get(ver)
		if !ok {
			continue // a minor this process has no codec for is not dialled
		}
		qc, err := query.NewClient(m.opts.Source, codec)
		if err != nil {
			t.Fatalf("query client for %s: %v", ver, err)
		}
		clients[ver] = qc
	}
	m.sourceClients = clients
}

// buildSourceClientsForEveryMinor is buildSourceClients for the
// cross-minor rules: every named minor gets a client, using the
// process's own codec, so a source that answers the same document on
// two minors' views can be exercised without a second codec being
// registered in the test binary.
func buildSourceClientsForEveryMinor(t *testing.T, m *Mirror, vers ...string) {
	t.Helper()
	codec, ok := is04.Get(m.opts.APIVer)
	if !ok {
		t.Fatalf("no codec for %s", m.opts.APIVer)
	}
	clients := map[string]*query.Client{}
	for _, ver := range vers {
		qc, err := query.NewClient(m.opts.Source, codec)
		if err != nil {
			t.Fatalf("query client for %s: %v", ver, err)
		}
		clients[ver] = qc
	}
	m.sourceClients = clients
}

// The resync refresh rebuilds the cache from the source's own REST
// views: a document with no id is skipped, a resource the first minor
// already claimed is not re-claimed by a later one, and a node that
// left the source stops being heartbeated.
func TestRefreshCacheFromSource(t *testing.T) {
	src, srcURL := newMirrorSource(t)
	node := mustJSONBytes(t, validNode(fxNode))
	src.mu.Lock()
	src.docs["nodes"] = []json.RawMessage{
		node,
		json.RawMessage(`{"label":"no id at all"}`),
		node, // the same resource again: the first minor keeps it
	}
	src.mu.Unlock()

	m, err := NewMirror(MirrorOptions{Source: srcURL, Target: "http://target.invalid:2", APIVer: "v1.3"})
	if err != nil {
		t.Fatal(err)
	}
	m.logger = newRegistryLogTap().logger()
	m.audit, _ = newAuditor("")

	// A node the target accepted earlier, and which the source no
	// longer carries, must stop being heartbeated.
	m.mu.Lock()
	m.targetNodes["aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"] = true
	m.targetNodes[fxNode] = true
	m.mu.Unlock()

	buildSourceClients(t, m)
	m.refreshCacheFromSource(context.Background())

	m.mu.Lock()
	cached := len(m.cache["nodes"])
	ver := m.cacheVer["nodes"][fxNode]
	_, stale := m.targetNodes["aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"]
	_, kept := m.targetNodes[fxNode]
	m.mu.Unlock()

	if cached != 1 {
		t.Errorf("cached %d nodes, want the one real document", cached)
	}
	if ver == "" {
		t.Error("the refreshed resource must carry the minor it was read at")
	}
	if stale {
		t.Error("a node the source no longer carries must stop being heartbeated")
	}
	if !kept {
		t.Error("a node still at the source keeps its acceptance mark")
	}
}

// The refresh reads every minor the process can decode, and no more:
// a minor with no codec has no client to read with, and a resource a
// lower minor already claimed is not re-claimed by a higher one.
func TestRefreshCacheAcrossMinors(t *testing.T) {
	prev := supportedVersions
	supportedVersions = func() []string { return []string{"v1.1", "v1.3", "v9.9"} }
	t.Cleanup(func() { supportedVersions = prev })

	src, srcURL := newMirrorSource(t)
	src.mu.Lock()
	src.docs["nodes"] = []json.RawMessage{mustJSONBytes(t, validNode(fxNode))}
	src.mu.Unlock()

	m, err := NewMirror(MirrorOptions{Source: srcURL, Target: "http://target.invalid:2", APIVer: "v1.3"})
	if err != nil {
		t.Fatal(err)
	}
	m.logger = newRegistryLogTap().logger()
	m.audit, _ = newAuditor("")
	// v1.1 and v1.3 both read; v9.9 has no client at all, the way a
	// minor this process has no codec for would not.
	buildSourceClientsForEveryMinor(t, m, "v1.1", "v1.3")

	m.refreshCacheFromSource(context.Background())

	m.mu.Lock()
	ver := m.cacheVer["nodes"][fxNode]
	cached := len(m.cache["nodes"])
	m.mu.Unlock()
	if cached != 1 {
		t.Errorf("cached %d nodes, want the one document", cached)
	}
	if ver != "v1.1" {
		t.Errorf("the resource is stamped %q, want the first minor that saw it", ver)
	}
}

// A source read that fails keeps the cached catalogue: a half-fetched
// one must never replace a whole one.
func TestRefreshCacheKeepsTheCatalogueOnFailure(t *testing.T) {
	ts := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		stdhttp.Error(w, "no", stdhttp.StatusInternalServerError)
	}))
	defer ts.Close()

	m, err := NewMirror(MirrorOptions{Source: ts.URL, Target: "http://target.invalid:2", APIVer: "v1.3"})
	if err != nil {
		t.Fatal(err)
	}
	tap := newRegistryLogTap()
	m.logger = tap.logger()
	m.audit, _ = newAuditor("")
	m.mu.Lock()
	m.cache["nodes"] = map[string]json.RawMessage{fxNode: mustJSONBytes(t, validNode(fxNode))}
	m.mu.Unlock()

	buildSourceClients(t, m)
	m.refreshCacheFromSource(context.Background())

	m.mu.Lock()
	kept := len(m.cache["nodes"])
	m.mu.Unlock()
	if kept != 1 {
		t.Errorf("the cached catalogue was replaced by a failed refresh: %d", kept)
	}
	if !tap.has("resync refresh failed") {
		t.Errorf("the failure must be reported; saw %v", tap.snapshot())
	}
}

// A source that hangs up resubscribes rather than giving up: the
// mirror is expected to outlive its source's restarts.
func TestMirrorResubscribesWhenTheWatchEnds(t *testing.T) {
	src, srcURL := newMirrorSource(t)
	src.mu.Lock()
	src.closeWS = true
	src.mu.Unlock()

	target := &scriptedTarget{}
	ts := httptest.NewServer(target.handler())
	defer ts.Close()

	m, err := NewMirror(MirrorOptions{Source: srcURL, Target: ts.URL, APIVer: "v1.3"})
	if err != nil {
		t.Fatal(err)
	}
	tap := newRegistryLogTap()
	m.logger = tap.logger()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- m.Run(ctx) }()

	deadline := time.Now().Add(20 * time.Second)
	for !tap.has("watch ended") && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !tap.has("watch ended") {
		t.Errorf("a source that hangs up must be resubscribed to; saw %v", tap.snapshot())
	}

	// Arm both debounced passes so the run has something to stop on
	// its way out: a timer left running would fire against a mirror
	// that is already gone.
	m.mu.Lock()
	m.serve = &mirrorServe{store: NewStore()}
	m.mu.Unlock()
	m.scheduleResync()
	m.scheduleServeReplay()
	m.mu.Lock()
	armed := m.resyncTimer != nil && m.serveReplayTimer != nil
	m.mu.Unlock()
	if !armed {
		t.Fatal("both debounced passes must be armed before the run ends")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("Run did not return after its context ended")
	}
	m.mu.Lock()
	stopped := m.resyncTimer == nil && m.serveReplayTimer == nil
	m.mu.Unlock()
	if !stopped {
		t.Error("a run that ended must leave no timer behind")
	}
}

// A mirror whose served face cannot bind fails the run outright — the
// operator sees it at startup, not as a mirror quietly serving nothing.
func TestMirrorRunRefusesAnOccupiedServeAddress(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()

	m, err := NewMirror(MirrorOptions{
		Source: "http://source.invalid:1", Target: "http://target.invalid:2",
		APIVer: "v1.3", ServeAddr: ln.Addr().String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	m.logger = newRegistryLogTap().logger()
	if err := m.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "serve listen") {
		t.Errorf("Run = %v, want the bind failure", err)
	}
}

// A minor the process has no codec for is skipped rather than dialled:
// the mirror subscribes once per minor it can actually decode.
func TestMirrorRunSkipsAMinorWithNoCodec(t *testing.T) {
	prev := supportedVersions
	supportedVersions = func() []string { return []string{"v1.3", "v9.9"} }
	t.Cleanup(func() { supportedVersions = prev })

	src, srcURL := newMirrorSource(t)
	_ = src
	target := &scriptedTarget{}
	ts := httptest.NewServer(target.handler())
	defer ts.Close()

	m, err := NewMirror(MirrorOptions{Source: srcURL, Target: ts.URL, APIVer: "v1.3"})
	if err != nil {
		t.Fatal(err)
	}
	m.logger = newRegistryLogTap().logger()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := m.Run(ctx); err != nil && !strings.Contains(err.Error(), "context") {
		t.Errorf("Run = %v", err)
	}
}

// A source URL the Query client cannot be built for fails the run at
// startup.
func TestMirrorRunRefusesAnUnusableSource(t *testing.T) {
	m, err := NewMirror(MirrorOptions{
		Source: "http://source\x7f.invalid:1", Target: "http://target.invalid:2", APIVer: "v1.3",
	})
	if err != nil {
		t.Fatal(err)
	}
	m.logger = newRegistryLogTap().logger()
	if err := m.Run(context.Background()); err == nil {
		t.Error("an unusable source must fail the run")
	}
}

// A face whose listener dies under it is reported: an operator asked
// for it, and it going away silently is how a mirror ends up looking
// healthy while answering nothing.
func TestServeUntilReportsAFaceThatStops(t *testing.T) {
	m := mirrorTo(t, "http://target:8235")
	tap := newRegistryLogTap()
	m.logger = tap.logger()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_ = ln.Close() // gone before the server ever accepts

	done := make(chan struct{})
	go func() {
		m.serveUntil(context.Background(), &stdhttp.Server{Handler: stdhttp.NotFoundHandler()}, ln, "test face")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("serveUntil must return when its listener is gone")
	}
	if !tap.has("test face failed") {
		t.Errorf("the stopped face must be reported; saw %v", tap.snapshot())
	}
}

// The status endpoint reports a bind it cannot have rather than
// leaving the operator watching a port nothing listens on.
func TestServeStatusReportsAnOccupiedAddress(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()

	m := mirrorTo(t, "http://target:8235")
	tap := newRegistryLogTap()
	m.logger = tap.logger()
	m.audit, _ = newAuditor("")

	done := make(chan struct{})
	go func() {
		m.serveStatus(context.Background(), ln.Addr().String())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("serveStatus must return when it cannot bind")
	}
	if !tap.has("status endpoint failed") {
		t.Errorf("the failed bind must be reported; saw %v", tap.snapshot())
	}
}
