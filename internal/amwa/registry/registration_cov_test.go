package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"dhs/internal/amwa/codec/is04"
	httpsession "dhs/internal/amwa/session/http"
)

// registrationBase boots the HTTP face over an empty store and
// returns the Registration API root for v1.3.
func registrationBase(t *testing.T) (string, *Store) {
	t.Helper()
	store := NewStore()
	addr, stop := startRegistryHTTP(t, store, nil)
	t.Cleanup(stop)
	return "http://" + addr + "/x-nmos/registration/v1.3", store
}

// post sends one registration envelope and returns the status,
// Location header and body.
func postResourceTo(t *testing.T, base string, rt is04.ResourceType, v any) (int, string, []byte) {
	t.Helper()
	body, err := is04.EncodeRegistration(rt, v)
	if err != nil {
		t.Fatalf("encode %s: %v", rt, err)
	}
	resp, err := stdhttp.Post(base+"/resource", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", rt, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := readAllBody(resp)
	return resp.StatusCode, resp.Header.Get("Location"), raw
}

func readAllBody(resp *stdhttp.Response) ([]byte, error) {
	buf := &bytes.Buffer{}
	_, err := buf.ReadFrom(resp.Body)
	return buf.Bytes(), err
}

func doRequest(t *testing.T, method, url string) (int, []byte) {
	t.Helper()
	req, err := stdhttp.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := stdhttp.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := readAllBody(resp)
	return resp.StatusCode, raw
}

// IS-04 §6.1.1: a first registration is 201 with a Location naming
// the resource, a re-registration of the same document is 200, and
// the response body echoes what was registered.
func TestRegistrationPostStatusAndLocation(t *testing.T) {
	base, store := registrationBase(t)

	status, loc, body := postResourceTo(t, base, is04.ResourceNode, validNode(fxNode))
	if status != stdhttp.StatusCreated {
		t.Fatalf("first POST = %d: %s", status, body)
	}
	if want := "/x-nmos/registration/v1.3/resource/nodes/" + fxNode; loc != want {
		t.Errorf("Location = %q, want %q", loc, want)
	}
	var echoed map[string]any
	if err := json.Unmarshal(body, &echoed); err != nil || echoed["id"] != fxNode {
		t.Errorf("response body = %s (%v)", body, err)
	}

	if status, _, body = postResourceTo(t, base, is04.ResourceNode, validNode(fxNode)); status != stdhttp.StatusOK {
		t.Errorf("re-registration = %d: %s", status, body)
	}
	if len(store.ListNodes()) != 1 {
		t.Errorf("store holds %d nodes, want 1", len(store.ListNodes()))
	}
}

// A body the Registration API cannot read, a resource whose parent is
// absent, and one already registered at a different minor are 400,
// 400 and 409 — never a silent accept.
func TestRegistrationPostRefusals(t *testing.T) {
	base, store := registrationBase(t)

	resp, err := stdhttp.Post(base+"/resource", "application/json", strings.NewReader(`{not json`))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != stdhttp.StatusBadRequest {
		t.Errorf("undecodable envelope = %d", resp.StatusCode)
	}

	// A Device whose Node was never registered.
	if status, _, body := postResourceTo(t, base, is04.ResourceDevice, validDevice(fxDevice, fxNode)); status != stdhttp.StatusBadRequest {
		t.Errorf("orphan device = %d: %s", status, body)
	}

	// Registered at v1.0 elsewhere, then POSTed to the v1.3 tree.
	if err := store.IngestRegistrationVersioned(
		envelope(t, is04.ResourceNode, validNode(fxNode)), "v1.0"); err != nil {
		t.Fatal(err)
	}
	if status, _, body := postResourceTo(t, base, is04.ResourceNode, validNode(fxNode)); status != stdhttp.StatusConflict {
		t.Errorf("api_ver conflict = %d: %s", status, body)
	}
}

// The resource subtree reads back and deregisters what was
// registered, and refuses a path that names no resource.
func TestRegistrationResourceSubtree(t *testing.T) {
	base, _ := registrationBase(t)
	if status, _, body := postResourceTo(t, base, is04.ResourceNode, validNode(fxNode)); status != stdhttp.StatusCreated {
		t.Fatalf("seed POST = %d: %s", status, body)
	}

	if status, body := doRequest(t, stdhttp.MethodGet, base+"/resource/nodes/"+fxNode); status != stdhttp.StatusOK {
		t.Errorf("read-back = %d: %s", status, body)
	}
	for _, path := range []string{
		"/resource/nodes/" + fxAbsent, // an id nobody registered
		"/resource/widgets/" + fxNode, // a collection IS-04 does not define
		"/resource/nodes",             // no id
	} {
		if status, body := doRequest(t, stdhttp.MethodGet, base+path); status != stdhttp.StatusNotFound {
			t.Errorf("GET %s = %d: %s", path, status, body)
		}
	}

	// DELETE removes it; a second DELETE is a 404.
	if status, body := doRequest(t, stdhttp.MethodDelete, base+"/resource/nodes/"+fxNode); status != stdhttp.StatusNoContent {
		t.Errorf("DELETE = %d: %s", status, body)
	}
	if status, _ := doRequest(t, stdhttp.MethodDelete, base+"/resource/devices/"+fxDevice); status != stdhttp.StatusNotFound {
		t.Error("deleting what was never registered must be a 404")
	}
}

// Heartbeats: POST refreshes the Node's health and answers with the
// timestamp, GET reads it back, and both refuse a Node the registry
// does not hold.
func TestRegistrationHealthEndpoint(t *testing.T) {
	base, _ := registrationBase(t)
	if status, _, body := postResourceTo(t, base, is04.ResourceNode, validNode(fxNode)); status != stdhttp.StatusCreated {
		t.Fatalf("seed POST = %d: %s", status, body)
	}

	resp, err := stdhttp.Post(base+"/health/nodes/"+fxNode, "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := readAllBody(resp)
	if resp.StatusCode != stdhttp.StatusOK {
		t.Fatalf("heartbeat = %d: %s", resp.StatusCode, raw)
	}
	var health is04.HealthResponse
	if err := json.Unmarshal(raw, &health); err != nil || health.Health == "" {
		t.Errorf("heartbeat body = %s (%v)", raw, err)
	}

	if status, body := doRequest(t, stdhttp.MethodGet, base+"/health/nodes/"+fxNode); status != stdhttp.StatusOK {
		t.Errorf("health read-back = %d: %s", status, body)
	}
	for _, path := range []string{
		"/health/nodes/" + fxAbsent,
		"/health/nodes/",
		"/health/nodes/" + fxNode + "/extra",
	} {
		if status, _ := doRequest(t, stdhttp.MethodGet, base+path); status != stdhttp.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, status)
		}
	}
}

// The Registration API index lists the two subtrees a client walks.
func TestRegistrationIndex(t *testing.T) {
	base, _ := registrationBase(t)
	status, body := doRequest(t, stdhttp.MethodGet, base+"/")
	if status != stdhttp.StatusOK {
		t.Fatalf("index = %d: %s", status, body)
	}
	var got []string
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode: %v (%s)", err, body)
	}
	if len(got) != 2 || got[0] != "resource/" || got[1] != "health/" {
		t.Errorf("index = %v", got)
	}
}

// BCP-003-02: an authenticated client may only update what it
// registered. A resource registered before auth was armed has no
// owner and is claimed by the first authenticated writer.
func TestRegistrationOwnershipIsEnforced(t *testing.T) {
	store := NewStore()
	srv := httpsession.NewServer(nil)
	installRegistrationRoutes(srv, store, "/x-nmos/registration/v1.3", "v1.3")
	handler := srv.MuxHandler()

	post := func(client string, v any) int {
		body, err := is04.EncodeRegistration(is04.ResourceNode, v)
		if err != nil {
			t.Fatal(err)
		}
		req := newPostRequest(t, "/x-nmos/registration/v1.3/resource", body)
		if client != "" {
			req = req.WithContext(httpsession.WithClientID(req.Context(), client))
		}
		rec := newRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}

	// First authenticated write claims the resource.
	if code := post("controller-a", validNode(fxNode)); code != stdhttp.StatusCreated {
		t.Fatalf("first write = %d", code)
	}
	if owner := store.Owner(fxNode); owner != "controller-a" {
		t.Errorf("owner = %q", owner)
	}

	// A different client is refused.
	updated := validNode(fxNode)
	updated.Label = "hijacked"
	if code := post("controller-b", updated); code != stdhttp.StatusForbidden {
		t.Errorf("another client's update = %d, want 403", code)
	}
	if got, _ := store.GetNode(fxNode); got.Label == "hijacked" {
		t.Error("the refused update reached the store")
	}

	// The owner itself may still update.
	if code := post("controller-a", updated); code != stdhttp.StatusOK {
		t.Errorf("the owner's own update = %d, want 200", code)
	}
}

// The discovery roots AMWA's auto_query / auto_registration checks
// walk: /x-nmos, then each face, then its version listing — with and
// without the trailing slash.
func TestAPIRootListings(t *testing.T) {
	srv := httpsession.NewServer(nil)
	installAPIRootRoutes(srv, []string{"v1.2", "v1.3"})
	handler := srv.MuxHandler()

	for path, want := range map[string][]string{
		"/x-nmos":               {"registration/", "query/"},
		"/x-nmos/":              {"registration/", "query/"},
		"/x-nmos/registration":  {"v1.2/", "v1.3/"},
		"/x-nmos/registration/": {"v1.2/", "v1.3/"},
		"/x-nmos/query":         {"v1.2/", "v1.3/"},
		"/x-nmos/query/":        {"v1.2/", "v1.3/"},
	} {
		rec := newRecorder()
		handler.ServeHTTP(rec, newGetRequest(t, path))
		if rec.Code != stdhttp.StatusOK {
			t.Errorf("GET %s = %d", path, rec.Code)
			continue
		}
		var got []string
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Errorf("GET %s: %v (%s)", path, err, rec.Body)
			continue
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("GET %s = %v, want %v", path, got, want)
		}
	}
}

// The GC evicts a Node whose heartbeat lapsed, says so, and stops
// when its context ends. A zero interval or threshold takes the
// spec-parity defaults rather than spinning.
func TestRunGCEvictsAndStops(t *testing.T) {
	store := NewStore()
	if err := store.PutNode(validNode(fxNode)); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	store.health[fxNode] = time.Now().Add(-time.Hour)
	store.mu.Unlock()

	tap := newRegistryLogTap()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runGC(ctx, store, tap.logger(), time.Millisecond, time.Second)
		close(done)
	}()

	deadline := time.Now().Add(10 * time.Second)
	for len(store.ListNodes()) > 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(store.ListNodes()) != 0 {
		t.Fatal("the GC did not evict a Node whose heartbeat lapsed")
	}
	// The line follows the eviction in the same goroutine, so poll for
	// it rather than reading it the instant the store went empty.
	for deadline = time.Now().Add(10 * time.Second); !tap.has("evicted stale nodes"); {
		if !time.Now().Before(deadline) {
			t.Fatalf("the eviction must be logged; saw %v", tap.snapshot())
		}
		time.Sleep(time.Millisecond)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the GC loop did not stop with its context")
	}

	// Zero interval / threshold fall back to the defaults; the loop
	// still stops on cancel.
	ctx2, cancel2 := context.WithCancel(context.Background())
	done2 := make(chan struct{})
	go func() {
		runGC(ctx2, store, nil, 0, 0)
		close(done2)
	}()
	cancel2()
	select {
	case <-done2:
	case <-time.After(10 * time.Second):
		t.Fatal("the defaulted GC loop did not stop with its context")
	}
}

// newGetRequest / newPostRequest build requests for the route table
// directly, without a listening socket.
func newGetRequest(t *testing.T, path string) *stdhttp.Request {
	t.Helper()
	return httptest.NewRequest(stdhttp.MethodGet, path, nil)
}

func newPostRequest(t *testing.T, path string, body []byte) *stdhttp.Request {
	t.Helper()
	req := httptest.NewRequest(stdhttp.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func newRecorder() *httptest.ResponseRecorder { return httptest.NewRecorder() }

// registryLogTap records the registry's own log lines so a test can
// assert on what it reported.
type registryLogTap struct {
	mu  sync.Mutex
	all []string
}

func newRegistryLogTap() *registryLogTap { return &registryLogTap{} }

func (l *registryLogTap) logger() *slog.Logger { return slog.New(l) }

func (l *registryLogTap) Enabled(context.Context, slog.Level) bool { return true }

func (l *registryLogTap) Handle(_ context.Context, r slog.Record) error {
	l.mu.Lock()
	l.all = append(l.all, r.Message)
	l.mu.Unlock()
	return nil
}

func (l *registryLogTap) WithAttrs([]slog.Attr) slog.Handler { return l }
func (l *registryLogTap) WithGroup(string) slog.Handler      { return l }

func (l *registryLogTap) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.all...)
}

func (l *registryLogTap) has(substr string) bool {
	for _, m := range l.snapshot() {
		if strings.Contains(m, substr) {
			return true
		}
	}
	return false
}
