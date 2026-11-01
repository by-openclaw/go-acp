package virtuoso

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"dhs/internal/plugin"
	"dhs/internal/virtuoso/codec"
)

const (
	testUser = "tester"
	testPass = "not-a-real-secret"
)

// fake is one chassis: the API of the User Guide over the synthetic
// fixtures. Slot 1 is a card, slot 2 a card that hangs up, slot 3 answers
// 404, the rest answer 502.
type fake struct {
	t   *testing.T
	srv *httptest.Server

	mu           sync.Mutex
	schemas      map[int][]byte
	docs         map[int]map[string]any
	sessions     map[string]bool
	seq          int
	reqs         []string
	logins       int
	logouts      int
	hangUp       string // path whose next authorised request is dropped
	info         string // when set, the "info" object of every data answer
	statusOf     map[string]int
	ignoreWrites bool
	noCookie     bool
	plainBody    map[string]string
	inFlight     int
	maxFlight    int
}

func newFake(t *testing.T) *fake {
	t.Helper()
	f := &fake{
		t:         t,
		schemas:   map[int][]byte{0: fixtureBytes(t, "manager_schema.json"), 1: fixtureBytes(t, "card_schema.json")},
		docs:      map[int]map[string]any{0: fixtureDoc(t, "manager_data.json"), 1: fixtureDoc(t, "card_data.json")},
		sessions:  map[string]bool{},
		statusOf:  map[string]int{},
		plainBody: map[string]string{},
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func fixtureBytes(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "testdata", "fixtures", name))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func fixtureDoc(t *testing.T, name string) map[string]any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(fixtureBytes(t, name)))
	dec.UseNumber()
	var w struct {
		Data map[string]any `json:"data"`
	}
	if err := dec.Decode(&w); err != nil {
		t.Fatal(err)
	}
	return w.Data
}

func (f *fake) host() (string, int) {
	u, err := url.Parse(f.srv.URL)
	if err != nil {
		f.t.Fatal(err)
	}
	h, p, err := net.SplitHostPort(u.Host)
	if err != nil {
		f.t.Fatal(err)
	}
	port, _ := strconv.Atoi(p)
	return h, port
}

func (f *fake) expireSessions() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessions = map[string]bool{}
}

func (f *fake) requests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.reqs...)
}

func (f *fake) count(prefix string) int {
	n := 0
	for _, r := range f.requests() {
		if strings.HasPrefix(r, prefix) {
			n++
		}
	}
	return n
}

func (f *fake) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.inFlight++
	f.maxFlight = max(f.maxFlight, f.inFlight)
	target := r.URL.Path
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	f.reqs = append(f.reqs, r.Method+" "+target)
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.inFlight--
		f.mu.Unlock()
	}()

	if body, ok := f.plain(r.URL.Path); ok {
		_, _ = w.Write([]byte(body))
		return
	}
	if f.takeHangUp(r.URL.Path) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = conn.Close()
		}
		return
	}
	if code, ok := f.forced(r.URL.Path); ok {
		if code == http.StatusMovedPermanently {
			w.Header().Set("Location", "https://elsewhere.invalid/")
		}
		http.Error(w, `{"info":{"status":"failed","errors":[{"status":-1,"source":"","detail":"forced"}]}}`, code)
		return
	}
	switch r.URL.Path {
	case codec.LoginTarget:
		f.login(w, r)
		return
	case codec.LogoutTarget:
		f.mu.Lock()
		f.logouts++
		f.mu.Unlock()
		f.reply(w, nil)
		return
	}
	if !f.authed(r) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"info":{"status":"failed","errors":[{"status":-1,"source":"","detail":"unauthorized"}]}}`))
		return
	}
	slot, rest := splitSlot(r.URL.Path)
	switch slot {
	case 0, 1:
	case 2:
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = conn.Close()
		}
		return
	case 3:
		w.WriteHeader(http.StatusNotFound)
		return
	default:
		w.WriteHeader(http.StatusBadGateway)
		return
	}
	switch {
	case rest == "/api/schema.json":
		_, _ = w.Write(f.schemas[slot])
	case strings.HasPrefix(rest, "/api/data"):
		f.data(w, r, slot, strings.TrimSuffix(strings.TrimPrefix(rest, "/api/data"), ".json"))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// takeHangUp drops every authorised request to a path, which is what a
// device doing a restart looks like. It stays on: net/http repeats a GET
// once when a reused connection drops.
func (f *fake) takeHangUp(path string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hangUp == path && len(f.sessions) > 0
}

func (f *fake) plain(path string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.plainBody[path]
	return b, ok
}

func (f *fake) forced(path string) (int, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.statusOf[path]
	return c, ok
}

func splitSlot(p string) (int, string) {
	if !strings.HasPrefix(p, "/slot/") {
		return 0, p
	}
	rest := strings.TrimPrefix(p, "/slot/")
	n, tail, _ := strings.Cut(rest, "/")
	slot, _ := strconv.Atoi(n)
	return slot, "/" + tail
}

func (f *fake) login(w http.ResponseWriter, r *http.Request) {
	u, p, ok := r.BasicAuth()
	if r.Method != http.MethodPost || !ok || u != testUser || p != testPass {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	f.mu.Lock()
	f.logins++
	f.seq++
	id := fmt.Sprintf("session-%d", f.seq)
	f.sessions[id] = true
	noCookie := f.noCookie
	f.mu.Unlock()
	if !noCookie {
		http.SetCookie(w, &http.Cookie{Name: codec.SessionCookie, Value: id, Path: "/"})
	}
	f.reply(w, nil)
}

func (f *fake) authed(r *http.Request) bool {
	c, err := r.Cookie(codec.SessionCookie)
	if err != nil {
		return false
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sessions[c.Value]
}

func (f *fake) reply(w http.ResponseWriter, data any) {
	f.mu.Lock()
	info := f.info
	f.mu.Unlock()
	if info == "" {
		info = `{"status":"ok","errors":[],"warnings":[],"notes":[]}`
	}
	body := map[string]json.RawMessage{"info": json.RawMessage(info)}
	if info == "none" {
		delete(body, "info")
	}
	if data != nil {
		raw, err := json.Marshal(data)
		if err != nil {
			f.t.Error(err)
		}
		body["data"] = raw
	}
	_ = json.NewEncoder(w).Encode(body)
}

func (f *fake) data(w http.ResponseWriter, r *http.Request, slot int, sub string) {
	var segs []string
	for _, s := range strings.Split(strings.Trim(sub, "/"), "/") {
		if s != "" {
			segs = append(segs, s)
		}
	}
	f.mu.Lock()
	doc := f.docs[slot]
	node, ok := descend(doc, segs)
	ignore := f.ignoreWrites
	f.mu.Unlock()
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	switch r.Method {
	case http.MethodGet:
		out := node
		for i := len(segs) - 1; i >= 0; i-- {
			out = map[string]any{segs[i]: out}
		}
		f.reply(w, out)
	case http.MethodPatch:
		m, isMap := node.(map[string]any)
		if !isMap {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if !ignore {
			f.mu.Lock()
			for k, v := range r.URL.Query() {
				m[k] = typed(m[k], v[0])
			}
			f.mu.Unlock()
		}
		f.reply(w, map[string]any{"_e": "u"})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func typed(old any, text string) any {
	switch old.(type) {
	case bool:
		return text == "true"
	case json.Number:
		return json.Number(text)
	}
	return text
}

func descend(doc any, segs []string) (any, bool) {
	cur := doc
	for _, s := range segs {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		if cur, ok = m[s]; !ok {
			return nil, false
		}
	}
	return cur, true
}

// connected builds a plugin logged in to f.
func connected(t *testing.T, f *fake) *Plugin {
	t.Helper()
	p := built(t, Options{User: testUser, Pass: testPass})
	host, port := f.host()
	if err := p.Connect(context.Background(), host, port); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Disconnect() })
	return p
}

func built(t *testing.T, o Options) *Plugin {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(discard{}, nil))
	p := (&Factory{}).New(plugin.Deps{Logger: logger}).(*Plugin)
	p.SetOptions(o)
	p.wait = 2 * time.Second
	p.gap = time.Millisecond
	return p
}

type discard struct{}

func (discard) Write(b []byte) (int, error) { return len(b), nil }
