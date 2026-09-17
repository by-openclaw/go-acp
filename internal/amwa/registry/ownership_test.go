package registry

// BCP-003-02 resource ownership (IS-04-02 test_33/test_33_1): an
// authenticated client may only update resources it registered; a
// different client's update answers 403. azp is normalised into
// client_id by the is10 codec, so one identity covers both tests.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	httpsession "dhs/internal/amwa/session/http"
)

// ownedPost drives the real registration route with a client identity
// stamped the way the auth gate does it.
func ownedPost(t *testing.T, srv *httpsession.Server, client string, body []byte) int {
	t.Helper()
	mux := srv.MuxHandler()
	req := httptest.NewRequest(stdhttp.MethodPost, "/x-nmos/registration/v1.3/resource", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if client != "" {
		req = req.WithContext(httpsession.WithClientID(context.Background(), client))
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	_, _ = io.Copy(io.Discard, rec.Body)
	return rec.Code
}

func TestRegistrationOwnershipRejectsMismatchedClient(t *testing.T) {
	store := NewStore()
	srv := httpsession.NewServer(nil)
	installRegistrationRoutes(srv, store, "/x-nmos/registration/v1.3", "v1.3")

	node := func(version string) []byte {
		raw, _ := json.Marshal(map[string]any{
			"type": "node",
			"data": map[string]any{
				"id": "0770e57e-0000-4000-8000-000000000033", "version": version,
				"label": "own", "description": "own", "tags": map[string]any{},
				"href": "http://h/", "hostname": "own", "caps": map[string]any{},
				"api": map[string]any{"versions": []string{"v1.3"}, "endpoints": []any{
					map[string]any{"host": "10.0.0.1", "port": 80, "protocol": "http"}}},
				"services": []any{}, "clocks": []any{}, "interfaces": []any{},
			},
		})
		return raw
	}

	// Client A registers.
	if code := ownedPost(t, srv, "client-a", node("1:0")); code != 201 {
		t.Fatalf("create by A = %d, want 201", code)
	}
	// Client A updates its own resource.
	if code := ownedPost(t, srv, "client-a", node("2:0")); code != 200 {
		t.Fatalf("update by A = %d, want 200", code)
	}
	// Client B must be refused.
	if code := ownedPost(t, srv, "client-b", node("3:0")); code != 403 {
		t.Fatalf("update by B = %d, want 403", code)
	}
	// Unauthenticated path (auth off) keeps historical behaviour.
	if code := ownedPost(t, srv, "", node("4:0")); code != 200 {
		t.Fatalf("update with auth off = %d, want 200", code)
	}
	if !strings.Contains(store.Owner("0770e57e-0000-4000-8000-000000000033"), "client-a") {
		t.Errorf("owner = %q, want client-a", store.Owner("0770e57e-0000-4000-8000-000000000033"))
	}
}
