package registry

// Bearer-gated served Query face tests (--auth-url with --serve,
// issue #946). Token minting follows the session/http authgate test
// harness — dhs never issues tokens — and the mirror is armed the
// production way: ServeAuthURL pointing at a mock RFC 8414
// Authorization Server whose JWKS carries the minting key, so the
// real NewKeyCache -> AuthGate path is exercised end to end.

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dhs/internal/amwa/codec/is10"
)

var serveAuthKey, _ = rsa.GenerateKey(rand.Reader, 2048)

// mockServeAS serves the RFC 8414 metadata + JWKS the mirror's
// KeyCache fetches — the shapes TestKeyCache pins in session/auth.
func mockServeAS(t *testing.T) *httptest.Server {
	t.Helper()
	mux := stdhttp.NewServeMux()
	var ts *httptest.Server
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                           ts.URL,
			"authorization_endpoint":           ts.URL + "/authorize",
			"token_endpoint":                   ts.URL + "/token",
			"jwks_uri":                         ts.URL + "/jwks",
			"registration_endpoint":            ts.URL + "/register",
			"response_types_supported":         []string{"code"},
			"code_challenge_methods_supported": []string{"S256", "plain"},
			"grant_types_supported":            []string{"authorization_code", "client_credentials"},
		})
	})
	mux.HandleFunc("/jwks", func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		_ = json.NewEncoder(w).Encode(is10.JWKS{Keys: []is10.JWK{{
			Kty: "RSA", Kid: "mirror-as-1",
			N: base64.RawURLEncoding.EncodeToString(serveAuthKey.N.Bytes()),
			E: base64.RawURLEncoding.EncodeToString([]byte{0x01, 0x00, 0x01}),
		}}})
	})
	ts = httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

// mintServeToken signs an RS512 token with the mock AS's key.
func mintServeToken(t *testing.T, claims map[string]any) string {
	t.Helper()
	enc := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return base64.RawURLEncoding.EncodeToString(b)
	}
	input := enc(map[string]string{"typ": "JWT", "alg": "RS512", "kid": "mirror-as-1"}) + "." + enc(claims)
	sum := sha512.Sum512([]byte(input))
	sig, err := rsa.SignPKCS1v15(rand.Reader, serveAuthKey, crypto.SHA512, sum[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return input + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// serveQueryClaims grants read on the whole Query API, addressed to
// the served face's advertise host (127.0.0.1 in these tests).
func serveQueryClaims(iss string) map[string]any {
	return map[string]any{
		"iss": iss, "sub": "u@test",
		"aud":       []string{"127.0.0.1"},
		"exp":       float64(time.Now().Add(10 * time.Minute).Unix()),
		"client_id": "c1",
		"scope":     "query",
		"x-nmos-query": map[string]any{
			"read": []string{"*"},
		},
	}
}

// freeLoopbackAddr reserves an ephemeral 127.0.0.1 port and returns
// it — for the status endpoint, whose bound address is not surfaced
// the way ServeAddr is.
func freeLoopbackAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

// startAuthedServedMirror is startServedMirror with the served face
// armed against the mock Authorization Server, plus a status endpoint.
func startAuthedServedMirror(t *testing.T, asURL, statusAddr string) (*Mirror, *pushSource) {
	t.Helper()
	plant := &fakePlant{}
	target := httptest.NewServer(plant.targetHandler())
	t.Cleanup(target.Close)
	push := newPushSource()
	src := httptest.NewServer(stdhttp.NotFoundHandler())
	src.Config.Handler = push.handler(t, func() string { return src.URL })
	t.Cleanup(src.Close)

	m, err := NewMirror(MirrorOptions{
		Source: src.URL, Target: target.URL, APIVer: "v1.3",
		ServeAddr: "127.0.0.1:0", ServeAuthURL: asURL,
		StatusAddr: statusAddr,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = m.Run(ctx) }()
	waitFor(t, 5*time.Second, func() bool { return m.ServeAddr() != "" }, "served face to bind")
	return m, push
}

// getWithToken GETs url with an optional Bearer token.
func getWithToken(t *testing.T, url, token string) (int, stdhttp.Header, []byte) {
	t.Helper()
	req, err := stdhttp.NewRequest(stdhttp.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := stdhttp.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, body
}

// TestMirrorServeAuthGate: an armed served face 401s tokenless and
// garbage-token reads with WWW-Authenticate, admits a valid token,
// keeps /x-nmos readable credential-less (IS-10 rule), gates the
// registration block, and reports serve_auth=true on /status.json.
func TestMirrorServeAuthGate(t *testing.T) {
	as := mockServeAS(t)
	statusAddr := freeLoopbackAddr(t)
	m, push := startAuthedServedMirror(t, as.URL, statusAddr)
	base := "http://" + m.ServeAddr()

	const nodeID = "f47ac10b-58cc-4372-a567-0e02b2c3d479"
	nodeDoc, _ := json.Marshal(validNode(nodeID))
	push.push("nodes", grainFrame("nodes", nodeID, "", string(nodeDoc)))

	good := mintServeToken(t, serveQueryClaims(as.URL))
	waitFor(t, 5*time.Second, func() bool {
		st, _, body := getWithToken(t, base+"/x-nmos/query/v1.3/nodes", good)
		return st == 200 && strings.Contains(string(body), nodeID)
	}, "mirrored node behind the armed gate with a valid token")

	// Tokenless: 401 + WWW-Authenticate, exactly the registry's gate.
	st, hdr, _ := getWithToken(t, base+"/x-nmos/query/v1.3/nodes", "")
	if st != 401 || !strings.HasPrefix(hdr.Get("WWW-Authenticate"), "Bearer ") {
		t.Errorf("tokenless = %d %q, want 401 + WWW-Authenticate: Bearer …", st, hdr.Get("WWW-Authenticate"))
	}
	// Garbage token: 401 invalid_token.
	st, hdr, _ = getWithToken(t, base+"/x-nmos/query/v1.3/nodes", "not.a.jws")
	if st != 401 || hdr.Get("WWW-Authenticate") == "" {
		t.Errorf("garbage token = %d %q, want 401 + WWW-Authenticate", st, hdr.Get("WWW-Authenticate"))
	}
	// The always-readable base path needs no token at all.
	if st, _, _ := getWithToken(t, base+"/x-nmos", ""); st != 200 {
		t.Errorf("/x-nmos tokenless = %d, want 200 (IS-10 always-readable)", st)
	}
	// The registration block sits behind the gate too: tokenless is
	// 401, not the disarmed face's 501.
	resp, err := stdhttp.Post(base+"/x-nmos/registration/v1.3/resource", "application/json",
		strings.NewReader(`{"type":"node","data":{}}`))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("tokenless registration POST = %d, want 401", resp.StatusCode)
	}

	// /status.json says the served face is armed.
	waitFor(t, 5*time.Second, func() bool {
		st, _, body := getWithToken(t, "http://"+statusAddr+"/status.json", "")
		return st == 200 && strings.Contains(string(body), `"serve_auth":true`)
	}, `"serve_auth":true on /status.json`)
}

// TestMirrorServeDisarmedStatus: a served-but-disarmed mirror reports
// an explicit serve_auth=false (the pre-#946 tokenless behaviour is
// pinned by the #940 serve tests).
func TestMirrorServeDisarmedStatus(t *testing.T) {
	plant := &fakePlant{}
	target := httptest.NewServer(plant.targetHandler())
	t.Cleanup(target.Close)
	push := newPushSource()
	src := httptest.NewServer(stdhttp.NotFoundHandler())
	src.Config.Handler = push.handler(t, func() string { return src.URL })
	t.Cleanup(src.Close)

	statusAddr := freeLoopbackAddr(t)
	m, err := NewMirror(MirrorOptions{
		Source: src.URL, Target: target.URL, APIVer: "v1.3",
		ServeAddr: "127.0.0.1:0", StatusAddr: statusAddr,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = m.Run(ctx) }()
	waitFor(t, 5*time.Second, func() bool { return m.ServeAddr() != "" }, "served face to bind")

	waitFor(t, 5*time.Second, func() bool {
		st, _, body := getWithToken(t, "http://"+statusAddr+"/status.json", "")
		return st == 200 && strings.Contains(string(body), `"serve_auth":false`)
	}, `"serve_auth":false on /status.json`)
}

// TestNewMirrorServeAuthRequiresServe: --auth-url without --serve is
// an operator error caught before anything runs.
func TestNewMirrorServeAuthRequiresServe(t *testing.T) {
	_, err := NewMirror(MirrorOptions{
		Source: "http://s:1", Target: "http://t:2",
		ServeAuthURL: "http://as:3",
	})
	if err == nil {
		t.Fatal("ServeAuthURL without ServeAddr must be rejected")
	}
	if !strings.Contains(err.Error(), "--serve") {
		t.Errorf("error should point the operator at --serve: %v", err)
	}
}
