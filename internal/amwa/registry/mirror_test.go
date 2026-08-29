package registry

// Mirror tests: a fake source Registry (subscriptions + Query-WS
// grains) and a fake target Registration API, wired through the real
// Mirror. The Cerebrum-taught details are pinned here: explicit
// Content-Length on health POSTs, and full dependency-ordered resync
// after a 404 heartbeat.

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

	_ "dhs/internal/amwa/codec/is04/v13" // register the v1.3 codec
	amwahttp "dhs/internal/amwa/session/http"
)

func TestNewMirrorValidates(t *testing.T) {
	if _, err := NewMirror(MirrorOptions{}); err == nil {
		t.Error("empty options must be rejected")
	}
	if _, err := NewMirror(MirrorOptions{Source: "http://a:1", Target: "http://a:1"}); err == nil {
		t.Error("source == target must be rejected")
	}
	if m, err := NewMirror(MirrorOptions{Source: "http://a:1", Target: "http://b:1"}); err != nil || m == nil {
		t.Errorf("valid options rejected: %v", err)
	}
}

// grainFrame builds one Query-WS grain frame for topic with one row.
func grainFrame(topic, id, pre, post string) []byte {
	row := map[string]any{"path": id}
	if pre != "" {
		row["pre"] = json.RawMessage(pre)
	}
	if post != "" {
		row["post"] = json.RawMessage(post)
	}
	g := map[string]any{
		"grain_type": "event",
		"grain": map[string]any{
			"type":  "urn:x-nmos:format:data.event",
			"topic": "/" + topic + "/",
			"data":  []any{row},
		},
	}
	raw, _ := json.Marshal(g)
	return raw
}

// fakePlant is a source Registry (subscriptions + ws) and a target
// Registration API sharing one recorder.
type fakePlant struct {
	mu       sync.Mutex
	posts    []string // "topic:id" in arrival order at the TARGET
	deletes  []string // "topic/id"
	healths  []string // node ids
	healthCL []string // Content-Length header per health POST
	evict    int      // answer this many health POSTs with 404 first
}

func (p *fakePlant) targetHandler() stdhttp.Handler {
	return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		switch {
		case r.Method == stdhttp.MethodPost && strings.HasSuffix(r.URL.Path, "/resource"):
			body, _ := io.ReadAll(r.Body)
			var env struct {
				Type string          `json:"type"`
				Data json.RawMessage `json:"data"`
			}
			_ = json.Unmarshal(body, &env)
			var doc struct {
				ID string `json:"id"`
			}
			_ = json.Unmarshal(env.Data, &doc)
			p.mu.Lock()
			p.posts = append(p.posts, env.Type+":"+doc.ID)
			p.mu.Unlock()
			w.WriteHeader(stdhttp.StatusCreated)
		case r.Method == stdhttp.MethodDelete:
			parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/x-nmos/registration/v1.3/resource/"), "/")
			p.mu.Lock()
			p.deletes = append(p.deletes, strings.Join(parts, "/"))
			p.mu.Unlock()
			w.WriteHeader(stdhttp.StatusNoContent)
		case r.Method == stdhttp.MethodPost && strings.Contains(r.URL.Path, "/health/nodes/"):
			id := r.URL.Path[strings.LastIndexByte(r.URL.Path, '/')+1:]
			p.mu.Lock()
			p.healths = append(p.healths, id)
			p.healthCL = append(p.healthCL, r.Header.Get("Content-Length"))
			evict := p.evict > 0
			if evict {
				p.evict--
			}
			p.mu.Unlock()
			if evict {
				w.WriteHeader(stdhttp.StatusNotFound)
				return
			}
			w.WriteHeader(stdhttp.StatusOK)
		default:
			w.WriteHeader(stdhttp.StatusNotFound)
		}
	})
}

// sourceHandler serves POST /subscriptions and the ws endpoint. Each
// topic's socket first sends the frames in frames[topic], then holds
// open until the server closes.
func (p *fakePlant) sourceHandler(t *testing.T, srvURL func() string, frames map[string][][]byte) stdhttp.Handler {
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
			_, _ = fmt.Fprintf(w, `{"id":"sub-%s","ws_href":"%s","resource_path":"/%s"}`, topic, ws, topic)
		case strings.HasPrefix(r.URL.Path, "/ws/"):
			topic := strings.TrimPrefix(r.URL.Path, "/ws/")
			ws, err := amwahttp.AcceptWebSocket(w, r)
			if err != nil {
				t.Errorf("accept ws: %v", err)
				return
			}
			for _, f := range frames[topic] {
				if err := ws.SendText(f); err != nil {
					return
				}
			}
			// Hold open; read until the client closes.
			for {
				if _, err := ws.ReadText(); err != nil {
					return
				}
			}
		default:
			w.WriteHeader(stdhttp.StatusNotFound)
		}
	})
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", what)
}

func TestMirrorForwardsAndDeletes(t *testing.T) {
	plant := &fakePlant{}
	target := httptest.NewServer(plant.targetHandler())
	defer target.Close()

	frames := map[string][][]byte{
		"nodes":   {grainFrame("nodes", "n1", "", `{"id":"n1","label":"src-node"}`)},
		"devices": {grainFrame("devices", "d1", "", `{"id":"d1"}`)},
		"senders": {
			grainFrame("senders", "s1", "", `{"id":"s1","label":"S1"}`),
			grainFrame("senders", "s1", `{"id":"s1"}`, ""), // removed
		},
	}
	var src *httptest.Server
	src = httptest.NewServer((&fakePlant{}).sourceHandler(t, func() string { return src.URL }, frames))
	defer src.Close()
	// The source handler needs plant-independent frames only; reuse.
	src.Config.Handler = plant.sourceHandler(t, func() string { return src.URL }, frames)

	m, err := NewMirror(MirrorOptions{Source: src.URL, Target: target.URL, APIVer: "v1.3"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = m.Run(ctx) }()

	waitFor(t, 5*time.Second, func() bool {
		plant.mu.Lock()
		defer plant.mu.Unlock()
		hasNode, hasDel := false, len(plant.deletes) > 0
		for _, p := range plant.posts {
			if p == "node:n1" {
				hasNode = true
			}
		}
		return hasNode && hasDel
	}, "node POST + sender DELETE at target")

	plant.mu.Lock()
	defer plant.mu.Unlock()
	joined := strings.Join(plant.posts, ",")
	if !strings.Contains(joined, "device:d1") || !strings.Contains(joined, "sender:s1") {
		t.Errorf("posts = %v", plant.posts)
	}
	if plant.deletes[0] != "senders/s1" {
		t.Errorf("deletes = %v", plant.deletes)
	}
	st := m.Stats()
	if st.Forwarded < 3 || st.Deleted < 1 {
		t.Errorf("stats = %+v", st)
	}
}

func TestMirrorHeartbeatsWithContentLengthAndResyncsOn404(t *testing.T) {
	plant := &fakePlant{evict: 1} // first heartbeat answered 404
	target := httptest.NewServer(plant.targetHandler())
	defer target.Close()

	frames := map[string][][]byte{
		"nodes":   {grainFrame("nodes", "n1", "", `{"id":"n1"}`)},
		"sources": {grainFrame("sources", "src1", "", `{"id":"src1"}`)},
	}
	src := httptest.NewServer(stdhttp.NotFoundHandler())
	src.Config.Handler = plant.sourceHandler(t, func() string { return src.URL }, frames)
	defer src.Close()

	m, err := NewMirror(MirrorOptions{Source: src.URL, Target: target.URL, APIVer: "v1.3"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = m.Run(ctx) }()

	// One heartbeat interval must elapse; the first health POST is
	// 404'd which must trigger a resync (node + source re-POSTed).
	waitFor(t, 3*MirrorHeartbeatInterval, func() bool {
		return m.Stats().Resyncs >= 1 && m.Stats().Heartbeats >= 1
	}, "a 404-triggered resync followed by a healthy heartbeat")

	plant.mu.Lock()
	defer plant.mu.Unlock()
	for i, cl := range plant.healthCL {
		if cl != "0" {
			t.Errorf("health POST %d: Content-Length = %q, want \"0\" (411 trap)", i, cl)
		}
	}
	// Resync re-POSTs in dependency order: the node must be re-posted
	// before the source in the post-404 tail.
	joined := strings.Join(plant.posts, ",")
	if strings.Count(joined, "node:n1") < 2 || strings.Count(joined, "source:src1") < 2 {
		t.Errorf("expected resync re-POSTs, posts = %v", plant.posts)
	}
	last := plant.posts[len(plant.posts)-2:]
	if last[0] != "node:n1" || last[1] != "source:src1" {
		t.Errorf("resync order wrong, tail = %v", last)
	}
}
