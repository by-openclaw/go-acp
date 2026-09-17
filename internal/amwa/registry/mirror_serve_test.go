package registry

// Served Query face tests (--serve, issue #940): the mirror is fed
// grains from a fake source and must answer as a read-only Registry —
// REST reads, WS subscription grains on later updates, refused
// registrations — with every served interaction landing in the audit
// ring. The consumer side is exercised through the real
// session/query client, the same code `dhs consumer nmos watch` runs.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
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

// pushSource is a fake source Registry whose per-topic Query-WS frames
// are pushed on demand — the serve tests need a row to arrive AFTER a
// subscriber connected to the mirror's served face, which the
// send-everything-up-front sourceHandler cannot stage.
type pushSource struct {
	ch map[string]chan []byte
}

func newPushSource() *pushSource {
	p := &pushSource{ch: make(map[string]chan []byte, len(mirrorTopics))}
	for _, tp := range mirrorTopics {
		p.ch[tp] = make(chan []byte, 16)
	}
	return p
}

func (p *pushSource) push(topic string, frame []byte) { p.ch[topic] <- frame }

func (p *pushSource) handler(t *testing.T, srvURL func() string) stdhttp.Handler {
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
			ws := strings.Replace(srvURL(), "http://", "ws://", 1) + "/ws/" + topic
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(stdhttp.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"sub-` + topic + `","ws_href":"` + ws + `","resource_path":"/` + topic + `"}`))
		case strings.HasPrefix(r.URL.Path, "/ws/"):
			topic := strings.TrimPrefix(r.URL.Path, "/ws/")
			ws, err := amwahttp.AcceptWebSocket(w, r)
			if err != nil {
				t.Logf("accept ws (teardown race?): %v", err)
				return
			}
			// The reader goroutine notices the mirror closing its socket
			// (test teardown) so this handler returns and the httptest
			// server's Close is never left waiting on it.
			done := make(chan struct{})
			go func() {
				defer close(done)
				for {
					if _, err := ws.ReadText(); err != nil {
						return
					}
				}
			}()
			for {
				select {
				case f := <-p.ch[topic]:
					if err := ws.SendText(f); err != nil {
						return
					}
				case <-done:
					return
				}
			}
		default:
			w.WriteHeader(stdhttp.StatusNotFound)
		}
	})
}

// startServedMirror boots a mirror with a served face against a push
// source + recording target and waits for the serve listener to bind.
func startServedMirror(t *testing.T) (*Mirror, *pushSource, context.CancelFunc) {
	t.Helper()
	plant := &fakePlant{}
	target := httptest.NewServer(plant.targetHandler())
	t.Cleanup(target.Close)
	push := newPushSource()
	src := httptest.NewServer(stdhttp.NotFoundHandler())
	src.Config.Handler = push.handler(t, func() string { return src.URL })
	t.Cleanup(src.Close)

	m, err := NewMirror(MirrorOptions{
		Source: src.URL, Target: target.URL, APIVer: "v1.3",
		ServeAddr: "127.0.0.1:0",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	// Cancel BEFORE the httptest servers close (t.Cleanup is LIFO) so
	// the mirror's sockets drop first and no source handler lingers.
	t.Cleanup(cancel)
	go func() { _ = m.Run(ctx) }()
	waitFor(t, 5*time.Second, func() bool { return m.ServeAddr() != "" }, "served face to bind")
	return m, push, cancel
}

// auditKinds snapshots the kinds in the mirror's in-memory audit ring.
func auditKinds(m *Mirror) []string {
	m.mu.Lock()
	a := m.audit
	m.mu.Unlock()
	evs := a.recent()
	out := make([]string, 0, len(evs))
	for _, ev := range evs {
		out = append(out, ev.Kind)
	}
	return out
}

func hasKind(kinds []string, want string) bool {
	for _, k := range kinds {
		if k == want {
			return true
		}
	}
	return false
}

// getBody GETs url and returns (status, body).
func getBody(t *testing.T, url string) (int, []byte) {
	t.Helper()
	resp, err := stdhttp.Get(url)
	if err != nil {
		return 0, nil
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}

// TestMirrorServeQueryFace: mirrored rows are readable on the served
// Query API; the device arrives BEFORE its parent node to force the
// ordered store replay; a removed row disappears; served reads land
// serve_query audit events + counters.
func TestMirrorServeQueryFace(t *testing.T) {
	m, push, _ := startServedMirror(t)
	base := "http://" + m.ServeAddr()

	const nodeID = "f47ac10b-58cc-4372-a567-0e02b2c3d479"
	const devID = "12345678-1234-4abc-9def-1234567890ab"
	nodeDoc, _ := json.Marshal(validNode(nodeID))
	devDoc, _ := json.Marshal(validDevice(devID, nodeID))

	// Device first: its store ingest fails (parent node not landed) and
	// must be repaired by the debounced dependency-ordered replay.
	push.push("devices", grainFrame("devices", devID, "", string(devDoc)))
	push.push("nodes", grainFrame("nodes", nodeID, "", string(nodeDoc)))

	waitFor(t, 5*time.Second, func() bool {
		status, body := getBody(t, base+"/x-nmos/query/v1.3/nodes")
		return status == 200 && strings.Contains(string(body), nodeID)
	}, "mirrored node on the served Query API")
	waitFor(t, 5*time.Second, func() bool {
		status, body := getBody(t, base+"/x-nmos/query/v1.3/devices")
		return status == 200 && strings.Contains(string(body), devID)
	}, "mirrored device (via ordered replay) on the served Query API")

	// Removed row → resource gone from the served face.
	push.push("devices", grainFrame("devices", devID, string(devDoc), ""))
	waitFor(t, 5*time.Second, func() bool {
		status, body := getBody(t, base+"/x-nmos/query/v1.3/devices")
		return status == 200 && !strings.Contains(string(body), devID)
	}, "deleted device to vanish from the served Query API")

	if !hasKind(auditKinds(m), "serve_query") {
		t.Errorf("audit ring missing serve_query: %v", auditKinds(m))
	}
	if st := m.Stats(); st.ServedQueries == 0 {
		t.Errorf("stats.ServedQueries = 0, want > 0 (%+v)", st)
	}
}

// TestMirrorServeWSSubscription: a Query-WS subscriber on the served
// face gets the sync grain, then a live grain when a mirrored row
// updates AFTER the subscription — proof mirror-driven store writes
// drive the same fan-out registration-driven ones do. Open + close
// land serve_ws_subscribe / serve_ws_close audit events.
func TestMirrorServeWSSubscription(t *testing.T) {
	m, push, _ := startServedMirror(t)
	base := "http://" + m.ServeAddr()

	const nodeID = "f47ac10b-58cc-4372-a567-0e02b2c3d479"
	node := validNode(nodeID)
	nodeDoc, _ := json.Marshal(node)
	push.push("nodes", grainFrame("nodes", nodeID, "", string(nodeDoc)))
	waitFor(t, 5*time.Second, func() bool {
		status, body := getBody(t, base+"/x-nmos/query/v1.3/nodes")
		return status == 200 && strings.Contains(string(body), nodeID)
	}, "mirrored node before subscribing")

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
		t.Fatalf("subscribe on served face: %v", err)
	}

	var mu sync.Mutex
	var frames []string
	watchCtx, watchCancel := context.WithCancel(ctx)
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		_ = query.Watch(watchCtx, sub.WSHref, func(g *is04.Grain) error {
			raw, _ := json.Marshal(g)
			mu.Lock()
			frames = append(frames, string(raw))
			mu.Unlock()
			return nil
		}, query.WatchOptions{})
	}()

	// Sync grain first — the pre-subscription catalogue.
	waitFor(t, 5*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		for _, f := range frames {
			if strings.Contains(f, nodeID) {
				return true
			}
		}
		return false
	}, "sync grain for the mirrored node")

	// Update AFTER subscribing — must arrive as a live grain.
	renamed := node
	renamed.Label = "renamed-after-subscribe"
	renamedDoc, _ := json.Marshal(renamed)
	push.push("nodes", grainFrame("nodes", nodeID, string(nodeDoc), string(renamedDoc)))
	waitFor(t, 5*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		for _, f := range frames {
			if strings.Contains(f, "renamed-after-subscribe") {
				return true
			}
		}
		return false
	}, "live grain for the post-subscription update")

	if !hasKind(auditKinds(m), "serve_ws_subscribe") {
		t.Errorf("audit ring missing serve_ws_subscribe: %v", auditKinds(m))
	}
	if st := m.Stats(); st.ServedWSSubs == 0 {
		t.Errorf("stats.ServedWSSubs = 0, want > 0 (%+v)", st)
	}

	// Close the consumer socket — the served face must audit the close.
	watchCancel()
	<-watchDone
	waitFor(t, 5*time.Second, func() bool {
		return hasKind(auditKinds(m), "serve_ws_close")
	}, "serve_ws_close audit event")
}

// TestMirrorServeRejectsRegistration: the served face is Query-only —
// a registration attempt answers 501 with an operator-readable body,
// lands mirror_write_rejected in the audit ring, and the discovery
// root never advertises a registration face.
func TestMirrorServeRejectsRegistration(t *testing.T) {
	m, _, _ := startServedMirror(t)
	base := "http://" + m.ServeAddr()

	env := []byte(`{"type":"node","data":{"id":"f47ac10b-58cc-4372-a567-0e02b2c3d479"}}`)
	resp, err := stdhttp.Post(base+"/x-nmos/registration/v1.3/resource", "application/json", bytes.NewReader(env))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != stdhttp.StatusNotImplemented {
		t.Fatalf("registration POST status = %d, want 501 (%s)", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "read-only mirror") {
		t.Errorf("rejection body should say why: %s", body)
	}
	if !hasKind(auditKinds(m), "mirror_write_rejected") {
		t.Errorf("audit ring missing mirror_write_rejected: %v", auditKinds(m))
	}
	if st := m.Stats(); st.ServedRejects == 0 {
		t.Errorf("stats.ServedRejects = 0, want > 0 (%+v)", st)
	}

	// Discovery root lists the query face only.
	status, rootBody := getBody(t, base+"/x-nmos")
	if status != 200 || !strings.Contains(string(rootBody), "query/") || strings.Contains(string(rootBody), "registration/") {
		t.Errorf("/x-nmos = %d %s, want query/ only", status, rootBody)
	}
}
