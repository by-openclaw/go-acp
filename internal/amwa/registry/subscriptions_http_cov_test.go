package registry

import (
	"context"
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	httpsession "dhs/internal/amwa/session/http"
)

const (
	subQueryBase = "/x-nmos/query/v1.3"
	subPrefix    = subQueryBase + "/subscriptions/"
)

// newSubManager builds a manager over an empty store. A nil logger is
// deliberate: the manager resolves its own default rather than
// panicking on the first log line.
func newSubManager(t *testing.T) *SubscriptionManager {
	t.Helper()
	return NewSubscriptionManager(nil, NewStore(), "127.0.0.1:8235", "v1.3")
}

// callHandler runs one session handler and returns the status and the
// decoded body. Handlers are called directly rather than through a
// mux: a mux normalises away exactly the paths whose guards matter
// here (a subscription id that is empty, or that carries a slash).
func callHandler(t *testing.T, h httpsession.HandlerFunc, method, path, body string) (int, any) {
	t.Helper()
	var req *stdhttp.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	status, out, err := h(context.Background(), req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return status, out
}

// A subscription POST mints an id, returns 201 with a Location, and
// keeps only the parameters that are filters — pagination and the
// version-control keys are not.
func TestSubscriptionPostCreatesAndStripsControls(t *testing.T) {
	m := newSubManager(t)
	status, out := callHandler(t, m.HandlePost(subQueryBase), stdhttp.MethodPost, subQueryBase+"/subscriptions", `{
		"resource_path": "/senders",
		"persist": true,
		"max_update_rate_ms": 100,
		"params": {"label": "cam-1", "paging.limit": "10", "query.downgrade": "v1.0", "query.rql": "eq(label,cam-1)"}
	}`)
	if status != stdhttp.StatusCreated {
		t.Fatalf("POST = %d (%+v)", status, out)
	}
	wrapped, ok := out.(*httpsession.WithHeaders)
	if !ok {
		t.Fatalf("POST body = %T, want a Location-carrying response", out)
	}
	res, ok := wrapped.Body.(SubscriptionResource)
	if !ok {
		t.Fatalf("POST body = %T, want the SubscriptionResource", wrapped.Body)
	}
	if res.ID == "" || !strings.HasSuffix(wrapped.Headers["Location"], "/subscriptions/"+res.ID) {
		t.Errorf("Location = %q for id %q", wrapped.Headers["Location"], res.ID)
	}
	if !strings.HasPrefix(res.WSHref, "ws://127.0.0.1:8235"+subQueryBase) {
		t.Errorf("ws_href = %q", res.WSHref)
	}

	m.mu.Lock()
	sub := m.subs[res.ID]
	m.mu.Unlock()
	if sub == nil {
		t.Fatal("the subscription was not stored")
	}
	if sub.downgrade != "v1.0" {
		t.Errorf("downgrade = %q, want the opt-in the client asked for", sub.downgrade)
	}
	if _, has := sub.params["paging.limit"]; has {
		t.Error("pagination is not a filter")
	}
	if _, has := sub.params["query.downgrade"]; has {
		t.Error("the downgrade opt-in is not a filter")
	}
	if _, has := sub.params["query.rql"]; !has {
		t.Error("the RQL predicate must survive as a filter")
	}

	// A subscription whose params are all control keys filters nothing.
	_, out = callHandler(t, m.HandlePost(subQueryBase), stdhttp.MethodPost, subQueryBase+"/subscriptions",
		`{"resource_path":"/nodes","params":{"paging.limit":"10"}}`)
	res = out.(*httpsession.WithHeaders).Body.(SubscriptionResource)
	m.mu.Lock()
	params := m.subs[res.ID].params
	m.mu.Unlock()
	if params != nil {
		t.Errorf("params = %v, want no filter at all", params)
	}
}

// The POST refusals: a body it cannot read, a request that names no
// resource_path, and every malformed ancestry filter — validated at
// POST time with the same codes the Query API GET path uses.
func TestSubscriptionPostRefusals(t *testing.T) {
	m := newSubManager(t)
	post := m.HandlePost(subQueryBase)

	for name, tc := range map[string]struct {
		body string
		want int
	}{
		"a body that is not JSON": {`{not json`, stdhttp.StatusBadRequest},
		"no resource_path":        {`{"persist":true}`, stdhttp.StatusBadRequest},
		"ancestry on a kind that has no parents": {
			`{"resource_path":"/senders","params":{"query.ancestry_id":"` + fxSource + `","query.ancestry_type":"children"}}`,
			stdhttp.StatusNotImplemented,
		},
		"an ancestry id without a type": {
			`{"resource_path":"/sources","params":{"query.ancestry_id":"` + fxSource + `"}}`,
			stdhttp.StatusBadRequest,
		},
		"an ancestry type that is neither": {
			`{"resource_path":"/sources","params":{"query.ancestry_id":"` + fxSource + `","query.ancestry_type":"cousins"}}`,
			stdhttp.StatusBadRequest,
		},
		"generations that are not a positive integer": {
			`{"resource_path":"/sources","params":{"query.ancestry_id":"` + fxSource +
				`","query.ancestry_type":"children","query.ancestry_generations":"0"}}`,
			stdhttp.StatusBadRequest,
		},
	} {
		t.Run(name, func(t *testing.T) {
			status, _ := callHandler(t, post, stdhttp.MethodPost, subQueryBase+"/subscriptions", tc.body)
			if status != tc.want {
				t.Errorf("POST = %d, want %d", status, tc.want)
			}
		})
	}

	// A well-formed ancestry subscription is accepted and remembers
	// what it was asked for.
	status, out := callHandler(t, post, stdhttp.MethodPost, subQueryBase+"/subscriptions",
		`{"resource_path":"/sources","params":{"query.ancestry_id":"`+fxSource+
			`","query.ancestry_type":"parents","query.ancestry_generations":"2"}}`)
	if status != stdhttp.StatusCreated {
		t.Fatalf("ancestry POST = %d", status)
	}
	res := out.(*httpsession.WithHeaders).Body.(SubscriptionResource)
	m.mu.Lock()
	sub := m.subs[res.ID]
	m.mu.Unlock()
	if sub.ancestryID != fxSource || sub.ancestryType != ancestryParents || sub.ancestryGens != 2 {
		t.Errorf("ancestry filter = (%q, %q, %d)", sub.ancestryID, sub.ancestryType, sub.ancestryGens)
	}
}

// The subscription collection: listed, read back by id, and released
// by DELETE. An id the manager does not hold — and a path that names
// no id at all — is a 404 on every verb.
func TestSubscriptionListGetAndDelete(t *testing.T) {
	m := newSubManager(t)
	_, out := callHandler(t, m.HandlePost(subQueryBase), stdhttp.MethodPost, subQueryBase+"/subscriptions",
		`{"resource_path":"/nodes","params":{"label":"cam-1"}}`)
	id := out.(*httpsession.WithHeaders).Body.(SubscriptionResource).ID

	status, listed := callHandler(t, m.HandleList(), stdhttp.MethodGet, subQueryBase+"/subscriptions", "")
	if status != 0 {
		t.Fatalf("list = %d", status)
	}
	subs, ok := listed.([]any)
	if !ok || len(subs) != 1 {
		t.Fatalf("list = %+v, want the one subscription", listed)
	}
	// The params the client sent are echoed back as a JSON object.
	first, ok := subs[0].(SubscriptionResource)
	if !ok {
		t.Fatalf("listed entry = %T", subs[0])
	}
	params, ok := first.Params.(map[string]any)
	if !ok || params["label"] != "cam-1" {
		t.Errorf("echoed params = %+v", first.Params)
	}

	status, got := callHandler(t, m.HandleGetByID(subPrefix), stdhttp.MethodGet, subPrefix+id, "")
	if status != 0 || got.(SubscriptionResource).ID != id {
		t.Errorf("GET by id = %d, %+v", status, got)
	}

	for name, path := range map[string]string{
		"an id nobody created":  subPrefix + "does-not-exist",
		"no id at all":          subPrefix,
		"more than one segment": subPrefix + id + "/ws",
	} {
		t.Run(name, func(t *testing.T) {
			if status, _ := callHandler(t, m.HandleGetByID(subPrefix), stdhttp.MethodGet, path, ""); status != stdhttp.StatusNotFound {
				t.Errorf("GET %s = %d", path, status)
			}
			if status, _ := callHandler(t, m.HandleDeleteByID(subPrefix), stdhttp.MethodDelete, path, ""); status != stdhttp.StatusNotFound {
				t.Errorf("DELETE %s = %d", path, status)
			}
		})
	}

	if status, _ := callHandler(t, m.HandleDeleteByID(subPrefix), stdhttp.MethodDelete, subPrefix+id, ""); status != stdhttp.StatusNoContent {
		t.Errorf("DELETE = %d, want 204", status)
	}
	m.mu.Lock()
	_, still := m.subs[id]
	m.mu.Unlock()
	if still {
		t.Error("a released subscription must be gone")
	}
}

// A v1.0 subscriber sees the v1.0 shape of the resource: `secure`
// landed in v1.1 and must not appear on that tree.
func TestSubscriptionListForV10(t *testing.T) {
	m := NewSubscriptionManager(nil, NewStore(), "127.0.0.1:8235", "v1.0")
	base := "/x-nmos/query/v1.0"
	_, out := callHandler(t, m.HandlePost(base), stdhttp.MethodPost, base+"/subscriptions",
		`{"resource_path":"/nodes"}`)
	body, ok := out.(*httpsession.WithHeaders).Body.(map[string]any)
	if !ok {
		t.Fatalf("v1.0 POST body = %T", out.(*httpsession.WithHeaders).Body)
	}
	if _, has := body["secure"]; has {
		t.Error("v1.0 has no `secure` field")
	}

	_, listed := callHandler(t, m.HandleList(), stdhttp.MethodGet, base+"/subscriptions", "")
	entries := listed.([]any)
	if len(entries) != 1 {
		t.Fatalf("list = %+v", entries)
	}
	if _, has := entries[0].(map[string]any)["secure"]; has {
		t.Error("the v1.0 listing must not carry `secure` either")
	}
}

// The manager defaults the wire minor when the caller names none, and
// mints wss hrefs once the face is served over TLS.
func TestSubscriptionManagerDefaults(t *testing.T) {
	m := NewSubscriptionManager(nil, NewStore(), "host:8235", "")
	if m.apiVer == "" {
		t.Error("a manager with no minor must take the current one")
	}
	if m.logger == nil {
		t.Error("a nil logger must be resolved, not stored")
	}

	m.SetWSScheme("wss")
	_, out := callHandler(t, m.HandlePost("/x-nmos/query/"+m.apiVer), stdhttp.MethodPost,
		"/x-nmos/query/"+m.apiVer+"/subscriptions", `{"resource_path":"/nodes"}`)
	res := out.(*httpsession.WithHeaders).Body.(SubscriptionResource)
	if !strings.HasPrefix(res.WSHref, "wss://") {
		t.Errorf("ws_href = %q, want wss on a TLS face", res.WSHref)
	}
}

// The WebSocket upgrade answers 404 for a path that is not a
// subscription's `/ws`, and for a subscription that does not exist —
// it never upgrades a socket nobody subscribed.
func TestSubscriptionUpgradeRefusesUnknownPaths(t *testing.T) {
	m := newSubManager(t)
	upgrade := m.UpgradeHandler(subQueryBase)

	for name, path := range map[string]string{
		"a path that does not end in /ws": subPrefix + "some-id",
		"nothing past the prefix":         subPrefix,
		"a subscription nobody created":   subPrefix + "does-not-exist/ws",
	} {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			upgrade(rec, httptest.NewRequest(stdhttp.MethodGet, path, nil))
			if rec.Code != stdhttp.StatusNotFound {
				t.Errorf("upgrade %s = %d, want 404", path, rec.Code)
			}
		})
	}

	// A real subscription whose request is not a WebSocket handshake
	// is refused by the transport, and the manager reports it rather
	// than leaving a half-open socket.
	_, out := callHandler(t, m.HandlePost(subQueryBase), stdhttp.MethodPost, subQueryBase+"/subscriptions",
		`{"resource_path":"/nodes"}`)
	id := out.(*httpsession.WithHeaders).Body.(SubscriptionResource).ID
	rec := httptest.NewRecorder()
	upgrade(rec, httptest.NewRequest(stdhttp.MethodGet, subPrefix+id+"/ws", nil))
	if rec.Code == stdhttp.StatusSwitchingProtocols {
		t.Error("a plain GET must not be upgraded")
	}
}

// A subscription POST body that decodes but carries a params object
// of the wrong JSON shape filters nothing rather than failing.
func TestSubscriptionPostWithUnusableParams(t *testing.T) {
	m := newSubManager(t)
	status, out := callHandler(t, m.HandlePost(subQueryBase), stdhttp.MethodPost, subQueryBase+"/subscriptions",
		`{"resource_path":"/nodes","params":{"limit":10}}`)
	if status != stdhttp.StatusCreated {
		t.Fatalf("POST = %d", status)
	}
	res := out.(*httpsession.WithHeaders).Body.(SubscriptionResource)
	m.mu.Lock()
	params := m.subs[res.ID].params
	m.mu.Unlock()
	if params != nil {
		t.Errorf("params = %v, want nothing usable as a filter", params)
	}

	// And the resource still renders its params as an object on the
	// wire, never null.
	_, got := callHandler(t, m.HandleGetByID(subPrefix), stdhttp.MethodGet, subPrefix+res.ID, "")
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"params":{}`) {
		t.Errorf("rendered subscription = %s", raw)
	}
}
