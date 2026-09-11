package auth

import (
	"context"
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	jwt "dhs/internal/auth"
)

// brokenAS is an Authorization Server whose every endpoint misbehaves in
// a way the test selects: metadata / jwks / token can each be valid,
// malformed JSON, or an HTTP failure.
type brokenAS struct {
	metadata, jwks, token string // "ok" | "junk" | "500" | "cut"
	ts                    *httptest.Server
}

func newBrokenAS(t *testing.T, metadata, jwks, token string) *brokenAS {
	t.Helper()
	b := &brokenAS{metadata: metadata, jwks: jwks, token: token}
	mux := stdhttp.NewServeMux()
	answer := func(mode string, w stdhttp.ResponseWriter, ok func()) {
		switch mode {
		case "junk":
			_, _ = w.Write([]byte("{not json"))
		case "500":
			w.WriteHeader(500)
			_, _ = w.Write([]byte("boom"))
		case "cut":
			w.Header().Set("Content-Length", "100")
			_, _ = w.Write([]byte("{"))
			if h, isH := w.(stdhttp.Hijacker); isH {
				if c, _, err := h.Hijack(); err == nil {
					_ = c.Close()
				}
			}
		default:
			ok()
		}
	}
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		answer(b.metadata, w, func() {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer": b.ts.URL, "authorization_endpoint": b.ts.URL + "/authorize",
				"token_endpoint": b.ts.URL + "/token", "jwks_uri": b.ts.URL + "/jwks",
				"registration_endpoint":    b.ts.URL + "/register",
				"response_types_supported": []string{"code"}, "code_challenge_methods_supported": []string{"S256"},
				"grant_types_supported": []string{"client_credentials"},
			})
		})
	})
	mux.HandleFunc("/jwks", func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		answer(b.jwks, w, func() {
			_ = json.NewEncoder(w).Encode(jwt.JWKS{Keys: []jwt.JWK{{Kty: "RSA", Kid: "b1", N: "AQAB", E: "AQAB"}}})
		})
	})
	mux.HandleFunc("/token", func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		answer(b.token, w, func() {
			_, _ = w.Write([]byte(`{"token_type":"Bearer"}`)) // no access_token: fails Validate
		})
	})
	b.ts = httptest.NewServer(mux)
	t.Cleanup(b.ts.Close)
	return b
}

// fetchJSON reports every failure with the URL: an unbuildable request, an
// unreachable server, a body cut short, a non-200.
func TestFetchJSONFailures(t *testing.T) {
	hc := &stdhttp.Client{Timeout: time.Second}
	if _, err := fetchJSON(context.Background(), hc, "http://bad url\x7f"); err == nil || !strings.Contains(err.Error(), "build request") {
		t.Errorf("unbuildable request: %v", err)
	}
	if _, err := fetchJSON(context.Background(), hc, "http://127.0.0.1:1/x"); err == nil || !strings.Contains(err.Error(), "GET") {
		t.Errorf("unreachable: %v", err)
	}
	b := newBrokenAS(t, "cut", "500", "ok")
	if _, err := fetchJSON(context.Background(), hc, MetadataURL(b.ts.URL, "")); err == nil || !strings.Contains(err.Error(), "read") {
		t.Errorf("cut body: %v", err)
	}
	if _, err := fetchJSON(context.Background(), hc, b.ts.URL+"/jwks"); err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("non-200: %v", err)
	}
}

// Token surfaces each stage's failure: metadata unreachable or malformed,
// the token POST refused without a JSON error body, a malformed token
// body, and a token body that fails validation.
func TestTokenClientFailureStages(t *testing.T) {
	cases := map[string]struct {
		as   *brokenAS
		want string
	}{
		"metadata junk":     {newBrokenAS(t, "junk", "ok", "ok"), "metadata"},
		"token 500 no json": {newBrokenAS(t, "ok", "ok", "500"), "HTTP 500"},
		"token junk":        {newBrokenAS(t, "ok", "ok", "junk"), "decode token response"},
		"token invalid":     {newBrokenAS(t, "ok", "ok", "ok"), "access_token"},
		"token cut":         {newBrokenAS(t, "ok", "ok", "cut"), "read token response"},
	}
	for name, tc := range cases {
		c := NewTokenClient(TokenClientOptions{MetadataURL: MetadataURL(tc.as.ts.URL, ""), ClientID: "cid", ClientSecret: "s", Scope: "node"})
		_, err := c.Token(context.Background())
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.want)) {
			t.Errorf("%s: err = %v, want mention of %q", name, err, tc.want)
		}
	}
	unreachable := NewTokenClient(TokenClientOptions{MetadataURL: "http://127.0.0.1:1/.well-known/oauth-authorization-server"})
	if _, err := unreachable.Token(context.Background()); err == nil {
		t.Error("unreachable metadata must fail")
	}
	// A token endpoint that vanishes after metadata was cached.
	as := newBrokenAS(t, "ok", "ok", "ok")
	c := NewTokenClient(TokenClientOptions{MetadataURL: MetadataURL(as.ts.URL, ""), ClientID: "cid", ClientSecret: "s", Scope: "node"})
	_, _ = c.Token(context.Background())
	c.meta.TokenEndpoint = "http://127.0.0.1:1/token"
	if _, err := c.Token(context.Background()); err == nil || !strings.Contains(err.Error(), "POST") {
		t.Errorf("vanished token endpoint: %v", err)
	}
	c.meta.TokenEndpoint = "http://bad url\x7f"
	if _, err := c.Token(context.Background()); err == nil || !strings.Contains(err.Error(), "build token request") {
		t.Errorf("unbuildable token request: %v", err)
	}
}

// Fetch and FetchIssuer surface each stage's failure, and FetchIssuer
// merges an issuer's keys without duplicating ones already held.
func TestKeyCacheFetchAndIssuerMerge(t *testing.T) {
	for name, as := range map[string]*brokenAS{
		"metadata junk": newBrokenAS(t, "junk", "ok", "ok"),
		"jwks 500":      newBrokenAS(t, "ok", "500", "ok"),
		"jwks junk":     newBrokenAS(t, "ok", "junk", "ok"),
	} {
		k := NewKeyCache(MetadataURL(as.ts.URL, ""), nil)
		if err := k.Fetch(context.Background()); err == nil {
			t.Errorf("Fetch %s must fail", name)
		}
		if err := k.FetchIssuer(context.Background(), as.ts.URL); err == nil {
			t.Errorf("FetchIssuer %s must fail", name)
		}
	}
	k := NewKeyCache("http://127.0.0.1:1/x", nil)
	if err := k.FetchIssuer(context.Background(), "http://127.0.0.1:1"); err == nil {
		t.Error("unreachable issuer must fail")
	}

	good := newBrokenAS(t, "ok", "ok", "ok")
	k = NewKeyCache(MetadataURL(good.ts.URL, ""), nil)
	if err := k.FetchIssuer(context.Background(), good.ts.URL); err != nil {
		t.Fatal(err)
	}
	if err := k.FetchIssuer(context.Background(), good.ts.URL); err != nil {
		t.Fatal(err)
	}
	if keys := k.Keys(); len(keys) != 1 || keys[0].Kid != "b1" {
		t.Errorf("issuer keys merged twice = %+v, want one key b1", keys)
	}
}

// Run refreshes on its cadence, keeps the cached keys when a refresh
// fails, and stops when the context ends.
func TestKeyCacheRunRefreshesAndStops(t *testing.T) {
	orig := refreshInterval
	refreshInterval = 5 * time.Millisecond
	t.Cleanup(func() { refreshInterval = orig })

	var hits atomic.Int32
	mux := stdhttp.NewServeMux()
	mux.HandleFunc("/", func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		hits.Add(1)
		w.WriteHeader(500)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	k := NewKeyCache(MetadataURL(ts.URL, ""), nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { k.Run(ctx); close(done) }()
	deadline := time.Now().Add(3 * time.Second)
	for hits.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not stop on cancel")
	}
	if hits.Load() < 2 {
		t.Errorf("Run refreshed %d times, want at least 2 on a 5ms cadence", hits.Load())
	}
}
