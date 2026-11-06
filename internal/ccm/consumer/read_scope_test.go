package consumer

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	dhsc "dhs/internal/consumer"
)

// An import reads every row before it writes it. Inside a read scope one
// GET of a resource serves all its fields; a write inside the scope is
// seen by the next read; outside the scope every read goes to the device.
func TestAReadScopeReadsEachResourceOnce(t *testing.T) {
	var mu sync.Mutex
	gets := 0
	doc := `{"name":"in 1","enable":true,"skew":"ClassA"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.URL.Path == "/self":
			_, _ = w.Write([]byte(`{"app":{"productName":"X","productVersion":"1"}}`))
		case r.URL.Path == "/docs/api.yml":
			_, _ = w.Write([]byte("openapi: 3.1.1\npaths:\n  /rx:\n    get:\n      operationId: G\n    put:\n      operationId: S\n"))
		case r.URL.Path == "/rx" && r.Method == http.MethodPut:
			b, _ := io.ReadAll(r.Body)
			doc = string(b)
		case r.URL.Path == "/rx":
			gets++
			_, _ = w.Write([]byte(doc))
		default:
			http.Error(w, "no", http.StatusNotFound)
		}
	}))
	defer srv.Close()
	restore := dialClient
	dialClient = func(string) *Client { return testClient(srv) }
	t.Cleanup(func() { dialClient = restore })
	p := testPluginConnected(t, srv)
	ctx := context.Background()

	var _ dhsc.ReadScoper = p
	get := func(field string) dhsc.Value {
		t.Helper()
		v, err := p.GetValue(ctx, dhsc.ValueRequest{Path: "rx." + field})
		if err != nil {
			t.Fatalf("get %s: %v", field, err)
		}
		return v
	}
	count := func() int { mu.Lock(); defer mu.Unlock(); return gets }

	end := p.ReadScope(ctx)
	get("name")
	get("enable")
	get("skew")
	if n := count(); n != 1 {
		t.Fatalf("three fields of one resource in a scope: %d GETs, want 1", n)
	}
	if _, err := p.SetValue(ctx, dhsc.ValueRequest{Path: "rx.name"}, dhsc.Value{Kind: dhsc.KindString, Str: "in 2"}); err != nil {
		t.Fatal(err)
	}
	if v := get("name"); v.Str != "in 2" {
		t.Errorf("read after a write in the scope = %q, want the written value", v.Str)
	}
	end()

	before := count()
	get("name")
	get("enable")
	if n := count() - before; n != 2 {
		t.Errorf("outside the scope: %d GETs for two reads, want 2", n)
	}
	var d map[string]any
	_ = json.Unmarshal([]byte(doc), &d)
	if d["skew"] != "ClassA" {
		t.Errorf("the write changed another field: %v", d)
	}
}
