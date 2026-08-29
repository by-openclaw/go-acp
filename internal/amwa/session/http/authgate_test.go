package http

// AuthGate tests: the resource-server rules of IS-10 exercised over
// the real dispatch path (MuxHandler + httptest), token minting done
// test-side — dhs never issues tokens.

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"
	"time"

	"dhs/internal/amwa/codec/is10"
)

var gateKey, _ = rsa.GenerateKey(rand.Reader, 2048)

func gateJWK() is10.JWK {
	return is10.JWK{
		Kty: "RSA", Kid: "g1",
		N: base64.RawURLEncoding.EncodeToString(gateKey.N.Bytes()),
		E: base64.RawURLEncoding.EncodeToString([]byte{0x01, 0x00, 0x01}),
	}
}

func gateMint(t *testing.T, claims map[string]any) string {
	t.Helper()
	enc := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return base64.RawURLEncoding.EncodeToString(b)
	}
	input := enc(map[string]string{"typ": "JWT", "alg": "RS512", "kid": "g1"}) + "." + enc(claims)
	sum := sha512.Sum512([]byte(input))
	sig, err := rsa.SignPKCS1v15(rand.Reader, gateKey, crypto.SHA512, sum[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return input + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func gateClaims() map[string]any {
	return map[string]any{
		"iss": "https://auth.test", "sub": "u@test",
		"aud":       []string{"node.test"},
		"exp":       float64(time.Now().Add(10 * time.Minute).Unix()),
		"client_id": "c1",
		"scope":     "node",
		"x-nmos-node": map[string]any{
			"read": []string{"*"},
		},
	}
}

func authedServer(t *testing.T) *httptest.Server {
	t.Helper()
	s := NewServer(nil)
	s.Auth = &AuthGate{Keys: StaticKeys{gateJWK()}, Hosts: []string{"127.0.0.1", "node.test"}}
	ok := func(context.Context, *stdhttp.Request) (int, any, error) { return 200, []string{"ok"}, nil }
	s.Handle(stdhttp.MethodGet, "/x-nmos/", ok)
	s.Handle(stdhttp.MethodGet, "/x-nmos/node/v1.3/self", ok)
	s.Handle(stdhttp.MethodPost, "/x-nmos/node/v1.3/self", ok)
	ts := httptest.NewServer(s.MuxHandler())
	t.Cleanup(ts.Close)
	return ts
}

func gateDo(t *testing.T, ts *httptest.Server, method, path, token string) (int, stdhttp.Header) {
	t.Helper()
	req, err := stdhttp.NewRequest(method, ts.URL+path, nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := stdhttp.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return resp.StatusCode, resp.Header
}

func TestAuthGate(t *testing.T) {
	ts := authedServer(t)
	good := gateMint(t, gateClaims())

	// /x-nmos readable with no token at all.
	if st, _ := gateDo(t, ts, "GET", "/x-nmos/", ""); st != 200 {
		t.Errorf("/x-nmos without token = %d, want 200", st)
	}
	// Deeper without a token: 401 + WWW-Authenticate: Bearer.
	st, hdr := gateDo(t, ts, "GET", "/x-nmos/node/v1.3/self", "")
	if st != 401 || hdr.Get("WWW-Authenticate") == "" {
		t.Errorf("no token = %d %q, want 401 + WWW-Authenticate", st, hdr.Get("WWW-Authenticate"))
	}
	// Valid token with read grant: 200.
	if st, _ := gateDo(t, ts, "GET", "/x-nmos/node/v1.3/self", good); st != 200 {
		t.Errorf("good token = %d, want 200", st)
	}
	// Valid token, write verb, no write grant: 403.
	st, hdr = gateDo(t, ts, "POST", "/x-nmos/node/v1.3/self", good)
	if st != 403 || hdr.Get("WWW-Authenticate") == "" {
		t.Errorf("no write grant = %d, want 403 + WWW-Authenticate", st)
	}
	// Expired token: 401 invalid_token.
	c := gateClaims()
	c["exp"] = float64(time.Now().Add(-5 * time.Minute).Unix())
	st, hdr = gateDo(t, ts, "GET", "/x-nmos/node/v1.3/self", gateMint(t, c))
	if st != 401 || hdr.Get("WWW-Authenticate") != `Bearer error=invalid_token` {
		t.Errorf("expired = %d %q", st, hdr.Get("WWW-Authenticate"))
	}
	// Wrong audience: a GENUINE token addressed to someone else is a
	// forbidden use of this server — 403, per the AMWA suite.
	c = gateClaims()
	c["aud"] = []string{"other.test"}
	if st, _ := gateDo(t, ts, "GET", "/x-nmos/node/v1.3/self", gateMint(t, c)); st != 403 {
		t.Errorf("wrong aud = %d, want 403", st)
	}
	// The no-token WWW-Authenticate begins "Bearer " (trailing space —
	// the suite tokenises after that exact prefix).
	st, hdr = gateDo(t, ts, "GET", "/x-nmos/node/v1.3/self", "")
	if st != 401 || len(hdr.Get("WWW-Authenticate")) < 7 || hdr.Get("WWW-Authenticate")[:7] != "Bearer " {
		t.Errorf("no-token WWW-Authenticate = %q, must begin 'Bearer '", hdr.Get("WWW-Authenticate"))
	}
	// Garbage token: 401.
	if st, _ := gateDo(t, ts, "GET", "/x-nmos/node/v1.3/self", "not.a.jws"); st != 401 {
		t.Errorf("garbage token = %d, want 401", st)
	}
	// access_token query parameter substitutes for the header.
	req, _ := stdhttp.NewRequest("GET", ts.URL+"/x-nmos/node/v1.3/self?access_token="+good, nil)
	resp, err := stdhttp.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("query token: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("query-param token = %d, want 200", resp.StatusCode)
	}
	// OPTIONS preflight passes with no credentials.
	req, _ = stdhttp.NewRequest("OPTIONS", ts.URL+"/x-nmos/node/v1.3/self", nil)
	resp, err = stdhttp.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("options: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("OPTIONS preflight = %d, want 200", resp.StatusCode)
	}
}
