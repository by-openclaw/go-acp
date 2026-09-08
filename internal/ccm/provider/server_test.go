package ccm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"dhs/internal/plugin"
)

func newTestServer(t *testing.T, spec []byte) (*Server, *httptest.Server) {
	t.Helper()
	tr, err := LoadTree([]byte(sampleTree))
	if err != nil {
		t.Fatalf("LoadTree: %v", err)
	}
	s := NewServer(plugin.Deps{}, tr, spec)
	hs := httptest.NewServer(s.Handler())
	t.Cleanup(hs.Close)
	return s, hs
}

func get(t *testing.T, hs *httptest.Server, path string) (*http.Response, []byte) {
	t.Helper()
	resp, err := hs.Client().Get(hs.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return resp, body
}

// A node GET returns the sorted child-name array — the self-describing walk
// contract a controller recurses on.
func TestServeNodeListsChildren(t *testing.T) {
	_, hs := newTestServer(t, nil)
	resp, body := get(t, hs, "/api/v1")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("content-type = %q, want application/json", ct)
	}
	var kids []string
	if err := json.Unmarshal(body, &kids); err != nil {
		t.Fatalf("root not a string array: %v (%s)", err, body)
	}
	if len(kids) != 3 || kids[0] != "io" {
		t.Errorf("root children = %v, want [io processing self]", kids)
	}
}

// A resource GET returns the captured body verbatim.
func TestServeResourceReturnsBody(t *testing.T) {
	_, hs := newTestServer(t, nil)
	resp, body := get(t, hs, "/api/v1/self")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var self map[string]any
	if err := json.Unmarshal(body, &self); err != nil {
		t.Fatalf("self not JSON: %v", err)
	}
	if self["productName"] != "BRIDGE" {
		t.Errorf("self = %v, want the captured identity", self)
	}
}

// An unknown path is the CCM {code,message} 404, not an empty body.
func TestServeAbsentIs404Envelope(t *testing.T) {
	_, hs := newTestServer(t, nil)
	resp, body := get(t, hs, "/api/v1/io/ip/senders/tx-9")
	if resp.StatusCode != 404 {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	var msg GenericApiMessage
	if err := json.Unmarshal(body, &msg); err != nil {
		t.Fatalf("404 body not the error envelope: %v (%s)", err, body)
	}
	if msg.Code != 404 || msg.Message == "" {
		t.Errorf("envelope = %+v, want code 404 + message", msg)
	}
}

// The OpenAPI document is served at the well-known spec path when present.
func TestServeSpec(t *testing.T) {
	spec := []byte("openapi: '3.1.2'\ninfo:\n  title: dhs CCM\n")
	_, hs := newTestServer(t, spec)
	resp, body := get(t, hs, "/api/v1/docs/api.yml")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/yaml" {
		t.Errorf("content-type = %q, want application/yaml", ct)
	}
	if string(body) != string(spec) {
		t.Errorf("spec served altered:\n%s", body)
	}
}

// With no spec configured the spec path is a 404, not a panic on nil.
func TestServeSpecAbsent(t *testing.T) {
	_, hs := newTestServer(t, nil)
	resp, _ := get(t, hs, "/api/v1/docs/api.yml")
	if resp.StatusCode != 404 {
		t.Errorf("status = %d, want 404 when no spec is served", resp.StatusCode)
	}
}

// Every response feeds the metrics connector, so --metrics-addr reports real
// traffic and send latency (footprint), not zeros.
func TestServeRecordsMetrics(t *testing.T) {
	s, hs := newTestServer(t, nil)
	get(t, hs, "/api/v1/self")
	get(t, hs, "/api/v1")
	snap := s.Metrics().Snapshot()
	if snap.TxFrames < 2 {
		t.Errorf("tx frames = %d, want >= 2", snap.TxFrames)
	}
	if snap.RxFrames < 2 {
		t.Errorf("rx frames = %d, want >= 2", snap.RxFrames)
	}
}

// Metrics is never nil even when deps carry no connector — WithDefaults fills
// it, so --metrics-addr always has a series.
func TestMetricsNonNil(t *testing.T) {
	tr, _ := LoadTree([]byte(sampleTree))
	if NewServer(plugin.Deps{}, tr, nil).Metrics() == nil {
		t.Fatal("Metrics must be non-nil")
	}
}

// Serve binds and returns cleanly when the context is cancelled.
func TestServeBindsAndStopsOnCancel(t *testing.T) {
	tr, _ := LoadTree([]byte(sampleTree))
	s := NewServer(plugin.Deps{}, tr, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, "127.0.0.1:0") }()
	time.Sleep(50 * time.Millisecond) // let it bind
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return after context cancel")
	}
}
