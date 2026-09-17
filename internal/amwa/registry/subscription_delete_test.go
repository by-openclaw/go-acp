package registry

import (
	"bytes"
	"encoding/json"
	"io"
	stdhttp "net/http"
	"strings"
	"testing"
)

// newSubscriptionForTest POSTs a subscription and returns its id.
func newSubscriptionForTest(t *testing.T, base string) string {
	t.Helper()
	body, _ := json.Marshal(SubscriptionRequest{ResourcePath: "/nodes", Persist: true})
	resp, err := stdhttp.Post(base+"/x-nmos/query/v1.3/subscriptions",
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /subscriptions: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != stdhttp.StatusCreated && resp.StatusCode != stdhttp.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST /subscriptions: HTTP %d: %s", resp.StatusCode, raw)
	}
	var got SubscriptionResource
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode subscription: %v", err)
	}
	if got.ID == "" {
		t.Fatal("subscription came back with no id")
	}
	return got.ID
}

// TestDeleteSubscription: IS-04's Query API lets a Controller release a
// subscription it is finished with.
//
// This was missing entirely — only GET was registered on the
// by-id prefix. It went unnoticed because a non-persistent subscription
// is also reaped when its WebSocket closes, so the common path cleans
// up without anyone calling DELETE.
func TestDeleteSubscription(t *testing.T) {
	store := NewStore()
	mgr := NewSubscriptionManager(nil, store, "127.0.0.1:0", "v1.3")
	base, stop := startRegistryHTTP(t, store, mgr)
	defer stop()

	id := newSubscriptionForTest(t, "http://"+base)

	req, _ := stdhttp.NewRequest(stdhttp.MethodDelete,
		"http://"+base+"/x-nmos/query/v1.3/subscriptions/"+id, nil)
	resp, err := stdhttp.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != stdhttp.StatusNoContent {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("DELETE returned HTTP %d, want 204: %s", resp.StatusCode, raw)
	}

	// Gone from the by-id route...
	g, err := stdhttp.Get("http://" + base + "/x-nmos/query/v1.3/subscriptions/" + id)
	if err != nil {
		t.Fatalf("GET after delete: %v", err)
	}
	defer func() { _ = g.Body.Close() }()
	if g.StatusCode != stdhttp.StatusNotFound {
		t.Fatalf("GET after DELETE returned HTTP %d, want 404", g.StatusCode)
	}

	// ...and from the listing, or a Controller still sees a
	// subscription it just released.
	l, err := stdhttp.Get("http://" + base + "/x-nmos/query/v1.3/subscriptions")
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	defer func() { _ = l.Body.Close() }()
	raw, _ := io.ReadAll(l.Body)
	if strings.Contains(string(raw), id) {
		t.Fatalf("deleted subscription %s is still listed: %s", id, raw)
	}
}

func TestDeleteUnknownSubscriptionIs404(t *testing.T) {
	store := NewStore()
	mgr := NewSubscriptionManager(nil, store, "127.0.0.1:0", "v1.3")
	base, stop := startRegistryHTTP(t, store, mgr)
	defer stop()

	req, _ := stdhttp.NewRequest(stdhttp.MethodDelete,
		"http://"+base+"/x-nmos/query/v1.3/subscriptions/does-not-exist", nil)
	resp, err := stdhttp.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != stdhttp.StatusNotFound {
		t.Fatalf("DELETE of an unknown id returned HTTP %d, want 404", resp.StatusCode)
	}
}

// TestSubscriptionCORSAdvertisesDelete is how the AMWA suite actually
// caught this. Access-Control-Allow-Methods is generated from the route
// table, so an unregistered verb surfaces as a CORS complaint
// (auto_query_19: "'DELETE' not in 'Access-Control-Allow-Methods'")
// rather than as a missing endpoint — several steps from the cause.
func TestSubscriptionCORSAdvertisesDelete(t *testing.T) {
	store := NewStore()
	mgr := NewSubscriptionManager(nil, store, "127.0.0.1:0", "v1.3")
	base, stop := startRegistryHTTP(t, store, mgr)
	defer stop()

	id := newSubscriptionForTest(t, "http://"+base)

	req, _ := stdhttp.NewRequest(stdhttp.MethodOptions,
		"http://"+base+"/x-nmos/query/v1.3/subscriptions/"+id, nil)
	resp, err := stdhttp.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("OPTIONS: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	allow := resp.Header.Get("Access-Control-Allow-Methods")
	for _, want := range []string{"GET", "DELETE", "OPTIONS"} {
		if !strings.Contains(allow, want) {
			t.Errorf("Access-Control-Allow-Methods = %q, missing %s", allow, want)
		}
	}
}
