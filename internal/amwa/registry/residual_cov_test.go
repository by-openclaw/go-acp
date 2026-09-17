package registry

import (
	"encoding/json"
	"errors"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is04"
	httpsession "dhs/internal/amwa/session/http"
)

// errReader fails on the first read — a request body that dies
// mid-transfer, which the Registration API must answer rather than
// hang on.
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("connection reset") }

// A POST whose body cannot be read, one whose document carries no id,
// and one whose `data` is not an object are all refused — and none of
// them reaches the store.
func TestRegistrationPostMalformedBodies(t *testing.T) {
	store := NewStore()
	srv := httpsession.NewServer(nil)
	installRegistrationRoutes(srv, store, "/x-nmos/registration/v1.3", "v1.3")
	handler := srv.MuxHandler()

	post := func(body any) int {
		var req *stdhttp.Request
		switch b := body.(type) {
		case string:
			req = httptest.NewRequest(stdhttp.MethodPost, "/x-nmos/registration/v1.3/resource", strings.NewReader(b))
		default:
			req = httptest.NewRequest(stdhttp.MethodPost, "/x-nmos/registration/v1.3/resource", errReader{})
		}
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}

	if code := post(errReader{}); code != stdhttp.StatusBadRequest {
		t.Errorf("a body that cannot be read = %d, want 400", code)
	}

	noID := `{"type":"node","data":{"version":"0:0","label":"n","description":"d","tags":{}}}`
	if code := post(noID); code != stdhttp.StatusBadRequest {
		t.Errorf("a document with no id = %d, want 400", code)
	}

	notAnObject := `{"type":"node","data":[]}`
	if code := post(notAnObject); code != stdhttp.StatusBadRequest {
		t.Errorf("a `data` that is not an object = %d, want 400", code)
	}
	if len(store.ListNodes()) != 0 {
		t.Error("a refused registration must not reach the store")
	}
}

// A heartbeat for a Node the registry does not hold is a 404 — the
// signal a mirror reads as "you were evicted, re-register".
func TestRegistrationHeartbeatForAnUnknownNode(t *testing.T) {
	base, _ := registrationBase(t)
	resp, err := stdhttp.Post(base+"/health/nodes/"+fxAbsent, "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != stdhttp.StatusNotFound {
		t.Errorf("heartbeat for an unregistered Node = %d, want 404", resp.StatusCode)
	}
}

// A Query API served at a minor no codec is registered for still
// answers: the resource goes out in its canonical shape rather than
// the request failing.
func TestQueryPerIDGetFallsBackWithoutACodec(t *testing.T) {
	store := populated(t)
	srv := httpsession.NewServer(nil)
	installQueryRoutes(srv, store, nil, "/x-nmos/query/v9.9", "v9.9")
	rec := httptest.NewRecorder()
	srv.MuxHandler().ServeHTTP(rec, httptest.NewRequest(stdhttp.MethodGet, "/x-nmos/query/v9.9/nodes/"+fxNode, nil))

	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("GET = %d: %s", rec.Code, rec.Body)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v (%s)", err, rec.Body)
	}
	if body["id"] != fxNode {
		t.Errorf("body = %s", rec.Body)
	}
}

// Behind a TLS-terminating proxy the pagination cursors must name the
// scheme the client used, not the one that reached us — otherwise
// every `next` link walks a controller back onto plaintext.
func TestBuildLinkHeaderHonoursForwardedProto(t *testing.T) {
	store := populated(t)
	srv := httpsession.NewServer(nil)
	installQueryRoutes(srv, store, nil, "/x-nmos/query/v1.3", "v1.3")

	req := httptest.NewRequest(stdhttp.MethodGet, "/x-nmos/query/v1.3/nodes", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	srv.MuxHandler().ServeHTTP(rec, req)

	link := rec.Header().Get("Link")
	if !strings.Contains(link, "https://") {
		t.Errorf("Link = %q, want the forwarded scheme", link)
	}
}

// A version string with no separator at all is not a wire version,
// however long it is.
func TestParseAPIVerWithoutASeparator(t *testing.T) {
	if _, _, ok := parseAPIVer("v1234"); ok {
		t.Error("v1234 names no minor")
	}
	if maj, min, ok := parseAPIVer("v10.4"); !ok || maj != 10 || min != 4 {
		t.Errorf("parseAPIVer(v10.4) = %d, %d, %v", maj, min, ok)
	}
}

// Paging covers every collection, not just the two the HTTP tests
// happen to walk, and orders resources that share one update_ts by id
// so a page boundary is stable across requests.
func TestListPagedCoversEveryCollection(t *testing.T) {
	s := populated(t)
	for rt, want := range map[is04.ResourceType]int{
		is04.ResourceNode: 1, is04.ResourceDevice: 1, is04.ResourceSource: 1,
		is04.ResourceFlow: 1, is04.ResourceSender: 1, is04.ResourceReceiver: 1,
	} {
		page := s.ListPaged(rt, PageOptions{Limit: 10})
		if got := pageLen(t, rt, page); got != want {
			t.Errorf("ListPaged(%s) = %d items, want %d", rt, got, want)
		}
	}

	// Two Nodes stamped at the same instant: the id breaks the tie, in
	// both page directions, so neither is skipped at a boundary.
	const (
		tie  = "30000000-0000-4000-8000-000000000001"
		tie2 = "30000000-0000-4000-8000-000000000002"
	)
	for _, id := range []string{tie, tie2} {
		if err := s.PutNode(validNode(id)); err != nil {
			t.Fatal(err)
		}
	}
	s.mu.Lock()
	s.updateTSByType[is04.ResourceNode][tie] = "500:0"
	s.updateTSByType[is04.ResourceNode][tie2] = "500:0"
	s.updateTSByType[is04.ResourceNode][fxNode] = "400:0"
	s.mu.Unlock()

	desc := s.ListPaged(is04.ResourceNode, PageOptions{Limit: 2}).Items.([]is04.Node)
	if len(desc) != 2 || desc[0].ID != tie2 || desc[1].ID != tie {
		t.Errorf("head page = %v, want the tie broken by descending id", idsOf(desc))
	}
	asc := s.ListPaged(is04.ResourceNode, PageOptions{Since: "400:0", Limit: 2}).Items.([]is04.Node)
	if len(asc) != 2 || asc[0].ID != tie2 || asc[1].ID != tie {
		t.Errorf("ascending page = %v, want both tied resources", idsOf(asc))
	}
}

// pageLen reads the length of a typed page without the caller
// repeating the type switch.
func pageLen(t *testing.T, rt is04.ResourceType, page PageResult) int {
	t.Helper()
	switch rt {
	case is04.ResourceNode:
		return len(page.Items.([]is04.Node))
	case is04.ResourceDevice:
		return len(page.Items.([]is04.Device))
	case is04.ResourceSource:
		return len(page.Items.([]is04.Source))
	case is04.ResourceFlow:
		return len(page.Items.([]is04.Flow))
	case is04.ResourceSender:
		return len(page.Items.([]is04.Sender))
	case is04.ResourceReceiver:
		return len(page.Items.([]is04.Receiver))
	}
	t.Fatalf("unknown resource type %q", rt)
	return 0
}
