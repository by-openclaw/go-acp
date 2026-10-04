package registry

// The mirror fills its target before it listens: one walk of the
// source, forwarded parent-first. The subscriptions that follow run
// concurrently and deliver their SYNC grains in no particular order;
// they must find nothing to add.

import (
	"context"
	"encoding/json"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// strictTarget is a registration face that does what a real one does:
// it refuses (400) a child whose parent it does not hold.
type strictTarget struct {
	mu      sync.Mutex
	held    map[string]bool // "type:id"
	posts   []string        // accepted, in arrival order
	refused []string
}

// parentOf names, for each resource of the test catalogue, the
// resource a strict target must already hold.
var parentOf = map[string]string{
	"device:d1":   "node:n1",
	"source:src1": "device:d1",
	"flow:f1":     "source:src1",
	"sender:s1":   "flow:f1",
	"receiver:r1": "device:d1",
}

func (s *strictTarget) handler() stdhttp.Handler {
	return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if r.Method == stdhttp.MethodPost && strings.HasSuffix(r.URL.Path, "/resource") {
			body, _ := io.ReadAll(r.Body)
			var env struct {
				Type string `json:"type"`
				Data struct {
					ID string `json:"id"`
				} `json:"data"`
			}
			_ = json.Unmarshal(body, &env)
			key := env.Type + ":" + env.Data.ID
			s.mu.Lock()
			defer s.mu.Unlock()
			if parent, has := parentOf[key]; has && !s.held[parent] {
				s.refused = append(s.refused, key)
				w.WriteHeader(stdhttp.StatusBadRequest)
				return
			}
			s.held[key] = true
			s.posts = append(s.posts, key)
			w.WriteHeader(stdhttp.StatusCreated)
			return
		}
		w.WriteHeader(stdhttp.StatusOK) // health and anything else
	})
}

func TestMirrorFillsParentFirstBeforeItListens(t *testing.T) {
	docs := map[string][]string{
		"nodes":     {`{"id":"n1","label":"node"}`},
		"devices":   {`{"id":"d1","node_id":"n1"}`},
		"sources":   {`{"id":"src1","device_id":"d1"}`},
		"flows":     {`{"id":"f1","source_id":"src1"}`},
		"senders":   {`{"id":"s1","flow_id":"f1"}`},
		"receivers": {`{"id":"r1","device_id":"d1"}`},
	}
	ids := map[string]string{"nodes": "n1", "devices": "d1", "sources": "src1", "flows": "f1", "senders": "s1", "receivers": "r1"}

	target := &strictTarget{held: map[string]bool{}}
	tsrv := httptest.NewServer(target.handler())
	defer tsrv.Close()

	// The source: REST listings at v1.3 (nothing at the other minors),
	// and on every subscription a SYNC grain for its topic — sent only
	// once the test releases them, all six topics together.
	release := make(chan struct{})
	var once sync.Once
	letGo := func() { once.Do(func() { close(release) }) }
	frames := map[string][][]byte{}
	for topic, list := range docs {
		frames[topic] = [][]byte{grainFrame(topic, ids[topic], list[0], list[0])}
	}
	plant := &fakePlant{}
	src := httptest.NewServer(stdhttp.NotFoundHandler())
	defer src.Close()
	defer letGo() // a failed run must not leave the held sockets blocking Close
	ws := plant.sourceHandler(t, func() string { return src.URL }, frames)
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
		if strings.HasPrefix(r.URL.Path, "/ws/") {
			<-release // hold every SYNC until the fill has had its turn
		}
		ws.ServeHTTP(w, r)
	})

	m, err := NewMirror(MirrorOptions{Source: src.URL, Target: tsrv.URL, APIVer: "v1.3"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = m.Run(ctx) }()

	// The fill alone lands the whole catalogue, in dependency order.
	waitFor(t, 5*time.Second, func() bool {
		target.mu.Lock()
		defer target.mu.Unlock()
		return len(target.posts) == 6
	}, "the initial fill to reach the target")
	target.mu.Lock()
	order := strings.Join(target.posts, " ")
	target.mu.Unlock()
	if order != "node:n1 device:d1 source:src1 flow:f1 sender:s1 receiver:r1" {
		t.Errorf("fill order = %q, want parent-first", order)
	}

	// Now the SYNC grains arrive, all at once: nothing is re-sent,
	// nothing is refused, nothing needed repairing.
	letGo()
	time.Sleep(300 * time.Millisecond)
	target.mu.Lock()
	posts, refused := len(target.posts), append([]string(nil), target.refused...)
	target.mu.Unlock()
	if posts != 6 || len(refused) != 0 {
		t.Errorf("after the SYNC grains: %d posts, refused %v — want the six of the fill and none refused", posts, refused)
	}
	if st := m.Stats(); st.Failures != 0 || st.Resyncs != 0 || st.Forwarded != 6 {
		t.Errorf("stats = %+v, want 6 forwarded, no failure, no resync", st)
	}
}
