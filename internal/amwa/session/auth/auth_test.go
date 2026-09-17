package auth

// TokenClient + KeyCache against a mock Authorization Server. The
// mock's shapes follow the IS-10 schemas (auth_metadata.json,
// token_response.json, jwks_schema.json).

import (
	"context"
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"dhs/internal/amwa/codec/is10"
	jwt "dhs/internal/auth"
)

// mockAS serves metadata + jwks + token endpoints.
func mockAS(t *testing.T, tokenHits *atomic.Int32, expiresIn int) *httptest.Server {
	t.Helper()
	mux := stdhttp.NewServeMux()
	var ts *httptest.Server
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
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
	mux.HandleFunc("/jwks", func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		_ = json.NewEncoder(w).Encode(jwt.JWKS{Keys: []jwt.JWK{{Kty: "RSA", Kid: "m1", N: "AQAB", E: "AQAB"}}})
	})
	mux.HandleFunc("/token", func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		tokenHits.Add(1)
		user, pass, ok := r.BasicAuth()
		if !ok || user != "cid" || pass != "secret" {
			w.WriteHeader(401)
			_ = json.NewEncoder(w).Encode(is10.TokenError{Error: "invalid_client"})
			return
		}
		if err := r.ParseForm(); err != nil ||
			r.PostForm.Get("grant_type") != "client_credentials" ||
			r.PostForm.Get("scope") == "" {
			w.WriteHeader(400)
			_ = json.NewEncoder(w).Encode(is10.TokenError{Error: "invalid_request"})
			return
		}
		_ = json.NewEncoder(w).Encode(is10.TokenResponse{
			AccessToken: "tok-1", ExpiresIn: expiresIn, TokenType: "Bearer",
			Scope: r.PostForm.Get("scope"),
		})
	})
	ts = httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

func TestTokenClientFetchAndCache(t *testing.T) {
	var hits atomic.Int32
	as := mockAS(t, &hits, 3600)
	c := NewTokenClient(TokenClientOptions{
		MetadataURL: MetadataURL(as.URL, ""),
		ClientID:    "cid", ClientSecret: "secret", Scope: "registration query",
	})
	tok, err := c.Token(context.Background())
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	if tok != "tok-1" {
		t.Errorf("token = %q", tok)
	}
	// Second call is served from cache — no extra endpoint hit.
	if _, err := c.Token(context.Background()); err != nil {
		t.Fatalf("cached token: %v", err)
	}
	if hits.Load() != 1 {
		t.Errorf("token endpoint hits = %d, want 1 (cache)", hits.Load())
	}
}

func TestTokenClientRefreshesNearExpiry(t *testing.T) {
	var hits atomic.Int32
	// expires_in=10 with a 15s refresh margin → every call refreshes.
	as := mockAS(t, &hits, 10)
	c := NewTokenClient(TokenClientOptions{
		MetadataURL: MetadataURL(as.URL, ""),
		ClientID:    "cid", ClientSecret: "secret", Scope: "query",
	})
	for i := 0; i < 2; i++ {
		if _, err := c.Token(context.Background()); err != nil {
			t.Fatalf("token %d: %v", i, err)
		}
	}
	if hits.Load() != 2 {
		t.Errorf("token endpoint hits = %d, want 2 (short-lived tokens refresh)", hits.Load())
	}
}

func TestTokenClientBadCredentials(t *testing.T) {
	var hits atomic.Int32
	as := mockAS(t, &hits, 3600)
	c := NewTokenClient(TokenClientOptions{
		MetadataURL: MetadataURL(as.URL, ""),
		ClientID:    "cid", ClientSecret: "WRONG", Scope: "query",
	})
	if _, err := c.Token(context.Background()); err == nil {
		t.Error("bad credentials must fail")
	}
}

func TestKeyCache(t *testing.T) {
	var hits atomic.Int32
	as := mockAS(t, &hits, 3600)
	k := NewKeyCache(MetadataURL(as.URL, ""), nil)
	if got := k.Keys(); len(got) != 0 {
		t.Errorf("pre-fetch keys = %d, want 0", len(got))
	}
	if err := k.Fetch(context.Background()); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	got := k.Keys()
	if len(got) != 1 || got[0].Kid != "m1" {
		t.Errorf("keys = %+v", got)
	}
	// A failing refresh keeps the stale set.
	as.Close()
	if err := k.Fetch(context.Background()); err == nil {
		t.Error("fetch against a dead AS must error")
	}
	if got := k.Keys(); len(got) != 1 {
		t.Errorf("stale keys must survive a failed refresh, got %d", len(got))
	}
}

func TestMetadataURL(t *testing.T) {
	if u := MetadataURL("https://as.example.com", ""); u != "https://as.example.com/.well-known/oauth-authorization-server" {
		t.Errorf("plain = %s", u)
	}
	if u := MetadataURL("https://as.example.com/", "x-nmos/auth/v1.0"); u != "https://as.example.com/.well-known/oauth-authorization-server/x-nmos/auth/v1.0" {
		t.Errorf("selector = %s", u)
	}
}
