package ccm

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dhs/internal/plugin"
	"dhs/internal/transport"
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

// The compliance boundary: a dhs extension is reachable ONLY under
// ExtensionPrefix. The same relative path under the CCM namespace stays a
// tree lookup (here a 404), so registering our own endpoints can never change
// what a CCM controller observes under /api/v1 — the original protocol is
// untouched by construction.
func TestExtensionNeverLeaksIntoCCMNamespace(t *testing.T) {
	s, hs := newTestServer(t, nil)
	s.HandleExtension(http.MethodGet, "/capabilities/", func(context.Context, *http.Request) (int, any, error) {
		return http.StatusOK, map[string]string{"dhs": "extension"}, nil
	})

	// Served under the dhs namespace (leading/trailing slashes normalised).
	resp, body := get(t, hs, ExtensionPrefix+"/capabilities")
	if resp.StatusCode != 200 {
		t.Fatalf("extension status = %d, want 200 (%s)", resp.StatusCode, body)
	}
	var got map[string]string
	if err := json.Unmarshal(body, &got); err != nil || got["dhs"] != "extension" {
		t.Errorf("extension body = %s, want the registered payload", body)
	}

	// The identical path under the CCM namespace is NOT the extension — it
	// resolves against the device tree like any other CCM path.
	resp, body = get(t, hs, DefaultPrefix+"/capabilities")
	if resp.StatusCode != 404 {
		t.Fatalf("CCM-namespace status = %d, want 404 — an extension leaked into /api/v1 (%s)", resp.StatusCode, body)
	}
	var msg GenericApiMessage
	if err := json.Unmarshal(body, &msg); err != nil || msg.Code != 404 {
		t.Errorf("CCM 404 must still be the §12 envelope, got %s", body)
	}

	// And the CCM surface itself is unaffected by having extensions mounted.
	if resp, _ := get(t, hs, DefaultPrefix+"/self"); resp.StatusCode != 200 {
		t.Errorf("/api/v1/self = %d after mounting an extension, want 200", resp.StatusCode)
	}
}

// selfSignedOptions is a TLS posture with an in-memory self-signed server
// certificate, so a test can turn HTTPS on without files. The provider itself
// never touches crypto/tls (architecture gate); the test may.
func selfSignedOptions(t *testing.T) transport.TLSOptions {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "dhs-ccm-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	return transport.TLSOptions{Enable: true, Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
}

// WithTLS takes a TLS posture, not a crypto/tls config: the posture is built
// by transport (TLS 1.2 floor, certificate), the provider only installs the
// result. A disabled posture leaves plain HTTP; a posture with no certificate
// is transport's own error; a real posture turns HTTPS on.
func TestWithTLSTakesAPosture(t *testing.T) {
	tr, _ := LoadTree([]byte(sampleTree))
	s := NewServer(plugin.Deps{}, tr, nil)
	if s.http.TLS != nil {
		t.Fatal("a fresh server must be plain HTTP")
	}
	if err := s.WithTLS(transport.TLSOptions{}); err != nil || s.http.TLS != nil {
		t.Errorf("disabled posture: err=%v tls=%v, want nil/nil (plain HTTP)", err, s.http.TLS)
	}
	if err := s.WithTLS(transport.TLSOptions{Enable: true}); err == nil {
		t.Error("a TLS posture with no certificate must be refused")
	}
	if err := s.WithTLS(selfSignedOptions(t)); err != nil || s.http.TLS == nil {
		t.Errorf("real posture: err=%v tls=%v, want HTTPS on", err, s.http.TLS)
	}
}

// --- §11 writes over HTTP ---

func do(t *testing.T, hs *httptest.Server, method, path, body string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, hs.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := hs.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	out, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return resp, out
}

// writeSpec is a device-shaped OpenAPI document: PUT declared on one resource
// template (200) and on the audio matrix main level (200, MatrixState);
// /self, /status views, info and current are GET only; no PATCH.
const writeSpec = `openapi: '3.1.2'
paths:
  /v1/self:
    get:
  /v1/io/ip/senders/{uuid}:
    get:
    put:
      requestBody:
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/IpSenderPut'
        required: true
      responses:
        '200':
          description: '200 response'
        '400':
          description: '400 response'
  /v1/io/ip/senders/{uuid}/status:
    get:
  /v1/matrix/audio/info:
    get:
  /v1/matrix/audio/current:
    get:
  /v1/matrix/audio/main:
    get:
    put:
      requestBody:
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/MatrixState'
      responses:
        '200':
          description: '200 response'
  /v1/matrix/data/output/current:
    get:
`

const writeTree = `{
  "self": {"productName":"BRIDGE"},
  "io/ip/senders/tx-1": {"uuid":"tx-1","name":"CAM 1","enable":false},
  "io/ip/senders/tx-1/status": {"state":"ok"},
  "io/ip/senders/video": [{"uuid":"tx-1"}],
  "matrix/audio/info": {"description":"Audio matrix","sources":[],"destinations":[]},
  "matrix/audio/current": {"IP000-00":"DM000-00"},
  "matrix/audio/main": {"IP000-00":"DM000-00"},
  "matrix/data/output/current": {"IP00":"DM00"}
}`

func newWriteServer(t *testing.T, spec []byte) (*Server, *httptest.Server) {
	t.Helper()
	tr, err := LoadTree([]byte(writeTree))
	if err != nil {
		t.Fatalf("LoadTree: %v", err)
	}
	s := NewServer(plugin.Deps{}, tr, spec)
	hs := httptest.NewServer(s.Handler())
	t.Cleanup(hs.Close)
	return s, hs
}

// A PUT the document declares answers the status it promises (200) with the
// resource as it now reads: the mutable fields replaced, uuid kept.
func TestWritePutAnswersAsDeclared(t *testing.T) {
	s, hs := newWriteServer(t, []byte(writeSpec))
	resp, body := do(t, hs, http.MethodPut, "/api/v1/io/ip/senders/tx-1", `{"enable":true}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT = %d %s, want 200 as the document declares", resp.StatusCode, body)
	}
	var res map[string]any
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatalf("200 body is not the resource: %s", body)
	}
	if _, still := res["name"]; still || res["enable"] != true || res["uuid"] != "tx-1" {
		t.Errorf("response = %v, want name dropped, enable set, uuid kept", res)
	}
	_, got := get(t, hs, "/api/v1/io/ip/senders/tx-1")
	if string(got) != string(body) {
		t.Errorf("read-back %s differs from the write response %s", got, body)
	}
	snap := s.Metrics().Snapshot()
	if snap.RxBytes == 0 || snap.TxBytes == 0 {
		t.Errorf("a write must count request and response bytes: %+v", snap)
	}
}

// A matrix level PUT (MatrixState) stores the map and returns it; the
// document says nothing about current following main, so current is
// untouched.
func TestWriteMatrixLevelStoresAndReturnsState(t *testing.T) {
	_, hs := newWriteServer(t, []byte(writeSpec))
	resp, body := do(t, hs, http.MethodPut, "/api/v1/matrix/audio/main", `{"IP000-00":"MA000-03","EM001-02":"IP002-00"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT main = %d %s, want 200", resp.StatusCode, body)
	}
	var state map[string]string
	if err := json.Unmarshal(body, &state); err != nil || state["IP000-00"] != "MA000-03" || state["EM001-02"] != "IP002-00" || len(state) != 2 {
		t.Errorf("returned state = %s, want the written map", body)
	}
	_, cur := get(t, hs, "/api/v1/matrix/audio/current")
	if string(cur) != `{"IP000-00":"DM000-00"}` {
		t.Errorf("current changed to %s; the document declares no such rule", cur)
	}
}

// MatrixState is an object of strings: a non-string value is a 400 envelope.
func TestWriteMatrixStateValuesMustBeStrings(t *testing.T) {
	_, hs := newWriteServer(t, []byte(writeSpec))
	resp, body := do(t, hs, http.MethodPut, "/api/v1/matrix/audio/main", `{"IP000-00":7}`)
	var msg GenericApiMessage
	_ = json.Unmarshal(body, &msg)
	if resp.StatusCode != http.StatusBadRequest || msg.Code != 400 || !strings.Contains(msg.Message, "IP000-00") {
		t.Errorf("non-string source -> %d %s, want 400 envelope naming the key", resp.StatusCode, body)
	}
}

// A body that is not a JSON object is a 400 with the envelope naming the
// schema: arrays, invalid JSON, null, scalars.
func TestWriteRejectsBadBodies(t *testing.T) {
	_, hs := newWriteServer(t, []byte(writeSpec))
	for _, b := range []string{`[1]`, `{`, `null`, `"str"`} {
		resp, body := do(t, hs, http.MethodPut, "/api/v1/io/ip/senders/tx-1", b)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("body %s -> %d, want 400", b, resp.StatusCode)
			continue
		}
		var msg GenericApiMessage
		if err := json.Unmarshal(body, &msg); err != nil || msg.Code != 400 || !strings.Contains(msg.Message, "IpSenderPut") {
			t.Errorf("body %s -> 400 without the §12 envelope naming the schema: %s", b, body)
		}
	}
}

// An operation the document does not declare is 405 with the envelope,
// whatever the target: PATCH anywhere (the shipped api.yml has none), PUT on
// GET-only paths (/self, a /status view, matrix info and current), the spec,
// a collection, an interior node. A PUT on a path that exists nowhere — an
// unknown path, or main/backup on a matrix the device serves single-level —
// is 404.
func TestWriteUndeclaredIs405UnknownIs404(t *testing.T) {
	_, hs := newWriteServer(t, []byte(writeSpec))
	for _, c := range []struct{ m, p string }{
		{http.MethodPatch, "/api/v1/io/ip/senders/tx-1"},
		{http.MethodPatch, "/api/v1/matrix/audio/main"},
		{http.MethodPut, "/api/v1/self"},
		{http.MethodPut, "/api/v1/io/ip/senders/tx-1/status"},
		{http.MethodPut, "/api/v1/matrix/audio/info"},
		{http.MethodPut, "/api/v1/matrix/audio/current"},
		{http.MethodPut, "/api/v1/matrix/data/output/current"},
		{http.MethodPut, "/api/v1/docs/api.yml"},
		{http.MethodPut, "/api/v1/io/ip/senders/video"},
		{http.MethodPut, "/api/v1/io/ip"},
	} {
		resp, body := do(t, hs, c.m, c.p, `{"a":"b"}`)
		var msg GenericApiMessage
		_ = json.Unmarshal(body, &msg)
		if resp.StatusCode != http.StatusMethodNotAllowed || msg.Code != 405 {
			t.Errorf("%s %s -> %d %s, want 405 envelope", c.m, c.p, resp.StatusCode, body)
		}
	}
	for _, p := range []string{"/api/v1/nope", "/api/v1/matrix/data/output/main"} {
		resp, body := do(t, hs, http.MethodPut, p, `{"a":"b"}`)
		var msg GenericApiMessage
		_ = json.Unmarshal(body, &msg)
		if resp.StatusCode != http.StatusNotFound || msg.Code != 404 {
			t.Errorf("PUT %s -> %d %s, want 404 envelope", p, resp.StatusCode, body)
		}
	}
}

// The document declares a PUT on a template the tree does not hold (an id
// that was never captured): declared, but nothing to write -> 404. A declared
// PUT whose stored body is not an object (a collection captured at a
// templated path) -> 405.
func TestWriteDeclaredButNotInTreeOrNotObject(t *testing.T) {
	_, hs := newWriteServer(t, []byte(writeSpec))
	resp, _ := do(t, hs, http.MethodPut, "/api/v1/io/ip/senders/ghost", `{"a":"b"}`)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("PUT on an uncaptured id -> %d, want 404", resp.StatusCode)
	}
	resp, _ = do(t, hs, http.MethodPut, "/api/v1/io/ip/senders/video", `{"a":"b"}`)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("PUT on a collection -> %d, want 405", resp.StatusCode)
	}
}

// Without an API document there is no write contract: every write is 405 and
// the tree is untouched — replay is GET-only.
func TestWriteWithoutSpecIsGetOnly(t *testing.T) {
	_, hs := newWriteServer(t, nil)
	resp, body := do(t, hs, http.MethodPut, "/api/v1/io/ip/senders/tx-1", `{"enable":true}`)
	var msg GenericApiMessage
	_ = json.Unmarshal(body, &msg)
	if resp.StatusCode != http.StatusMethodNotAllowed || msg.Code != 405 {
		t.Fatalf("PUT without a document -> %d %s, want 405 envelope", resp.StatusCode, body)
	}
	_, got := get(t, hs, "/api/v1/io/ip/senders/tx-1")
	if !strings.Contains(string(got), `"enable":false`) {
		t.Errorf("tree changed without a contract: %s", got)
	}
}

// A declared PUT whose document names no 2xx code still answers 200: the
// resource as it now reads is the only success there is.
func TestWriteDefaultsTo200WhenNoCodeDeclared(t *testing.T) {
	_, hs := newWriteServer(t, []byte("openapi: '3.1.2'\npaths:\n  /v1/io/ip/senders/{uuid}:\n    put:\n"))
	resp, _ := do(t, hs, http.MethodPut, "/api/v1/io/ip/senders/tx-1", `{"enable":true}`)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("PUT = %d, want 200", resp.StatusCode)
	}
}

// errReader fails the first read, so a body that cannot be read is a 400
// rather than a hang or a panic.
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("boom") }

func TestWriteBodyReadErrorIs400(t *testing.T) {
	tr, _ := LoadTree([]byte(writeTree))
	s := NewServer(plugin.Deps{}, tr, []byte(writeSpec))
	req := httptest.NewRequest(http.MethodPut, "/api/v1/io/ip/senders/tx-1", errReader{})
	status, body, err := s.handleWrite(context.Background(), req)
	if err != nil || status != http.StatusBadRequest {
		t.Fatalf("handleWrite = %d, %v; want 400", status, err)
	}
	if msg, ok := body.(GenericApiMessage); !ok || msg.Code != 400 {
		t.Errorf("body = %#v, want a 400 envelope", body)
	}
}
