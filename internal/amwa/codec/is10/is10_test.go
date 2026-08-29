package is10_test

// Tests for the IS-10 token machinery. Expected behaviour comes from
// the AMWA v1.0.0 schemas + the Behaviour documents (Access Tokens,
// Resource Servers), not from working code: RS512-only, required
// claims, audience wildcards, and the path-authorization table.

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"dhs/internal/amwa/codec/is10"
	v10 "dhs/internal/amwa/codec/is10/v10"
)

var testKey, _ = rsa.GenerateKey(rand.Reader, 2048)

// mint builds a compact JWS over claims with the given alg label.
// Signing lives in the test only — dhs never issues tokens.
func mint(t *testing.T, claims map[string]any, alg, kid string) string {
	t.Helper()
	hdr := map[string]string{"typ": "JWT", "alg": alg}
	if kid != "" {
		hdr["kid"] = kid
	}
	enc := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return base64.RawURLEncoding.EncodeToString(b)
	}
	input := enc(hdr) + "." + enc(claims)
	sum := sha512.Sum512([]byte(input))
	sig, err := rsa.SignPKCS1v15(rand.Reader, testKey, crypto.SHA512, sum[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return input + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func testJWK(kid string) is10.JWK {
	pub := &testKey.PublicKey
	return is10.JWK{
		Kty: "RSA", Use: "sig", Alg: "RS512", Kid: kid,
		N: base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
		E: base64.RawURLEncoding.EncodeToString([]byte{0x01, 0x00, 0x01}),
	}
}

func baseClaims() map[string]any {
	return map[string]any{
		"iss":       "https://auth.example.com",
		"sub":       "user@example.com",
		"aud":       []string{"https://node-*.example.com"},
		"exp":       float64(time.Now().Add(30 * time.Minute).Unix()),
		"iat":       float64(time.Now().Unix()),
		"client_id": "client-1",
		"scope":     "registration query",
		"x-nmos-query": map[string]any{
			"read":  []string{"*"},
			"write": []string{"subscriptions/*"},
		},
		"x-nmos-connection": map[string]any{
			"read":  []string{"*"},
			"write": []string{"single/*"},
		},
	}
}

func TestVerifyRoundTrip(t *testing.T) {
	tok := mint(t, baseClaims(), "RS512", "k1")
	c, err := is10.VerifyWithKeys(tok, []is10.JWK{testJWK("k1")})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if c.Iss != "https://auth.example.com" || c.ClientID != "client-1" {
		t.Errorf("claims lost: %+v", c)
	}
	if p, ok := c.APIs["query"]; !ok || len(p.Write) != 1 || p.Write[0] != "subscriptions/*" {
		t.Errorf("x-nmos-query claim lost: %+v", c.APIs)
	}
	if err := is10.ValidateClaims(c, "node-1.example.com", time.Now(), 30*time.Second); err != nil {
		t.Errorf("valid claims rejected: %v", err)
	}
}

func TestAlgorithmPinning(t *testing.T) {
	// RS256, or the classic alg=none downgrade, must be rejected at
	// PARSE time regardless of the signature bytes.
	for _, alg := range []string{"RS256", "HS512", "none"} {
		tok := mint(t, baseClaims(), alg, "")
		if _, err := is10.VerifyWithKeys(tok, []is10.JWK{testJWK("")}); err == nil {
			t.Errorf("alg %s must be rejected", alg)
		}
	}
	// Tampered payload fails the signature.
	tok := mint(t, baseClaims(), "RS512", "")
	parts := strings.Split(tok, ".")
	forged := base64.RawURLEncoding.EncodeToString([]byte(`{"iss":"x","sub":"y","aud":"z","exp":9e9,"client_id":"c"}`))
	if _, err := is10.VerifyWithKeys(parts[0]+"."+forged+"."+parts[2], []is10.JWK{testJWK("")}); err == nil {
		t.Error("tampered payload must fail verification")
	}
}

func TestClaimValidation(t *testing.T) {
	now := time.Now()
	leeway := 30 * time.Second
	verify := func(mutate func(map[string]any)) error {
		m := baseClaims()
		mutate(m)
		tok := mint(t, m, "RS512", "")
		c, err := is10.VerifyWithKeys(tok, []is10.JWK{testJWK("")})
		if err != nil {
			t.Fatalf("verify: %v", err)
		}
		return is10.ValidateClaims(c, "node-1.example.com", now, leeway)
	}
	if err := verify(func(m map[string]any) {}); err != nil {
		t.Fatalf("base claims rejected: %v", err)
	}
	cases := map[string]func(map[string]any){
		"missing iss":         func(m map[string]any) { delete(m, "iss") },
		"missing sub":         func(m map[string]any) { delete(m, "sub") },
		"missing aud":         func(m map[string]any) { delete(m, "aud") },
		"missing exp":         func(m map[string]any) { delete(m, "exp") },
		"expired":             func(m map[string]any) { m["exp"] = float64(now.Add(-2 * time.Minute).Unix()) },
		"iat in future":       func(m map[string]any) { m["iat"] = float64(now.Add(5 * time.Minute).Unix()) },
		"nbf in future":       func(m map[string]any) { m["nbf"] = float64(now.Add(5 * time.Minute).Unix()) },
		"no client_id or azp": func(m map[string]any) { delete(m, "client_id") },
		"aud mismatch":        func(m map[string]any) { m["aud"] = []string{"other.example.net"} },
	}
	for name, mutate := range cases {
		if err := verify(mutate); err == nil {
			t.Errorf("%s: must be rejected", name)
		}
	}
	// Within-leeway expiry passes; azp substitutes for client_id;
	// string-form aud decodes.
	if err := verify(func(m map[string]any) { m["exp"] = float64(now.Add(-10 * time.Second).Unix()) }); err != nil {
		t.Errorf("expiry within leeway must pass: %v", err)
	}
	if err := verify(func(m map[string]any) { delete(m, "client_id"); m["azp"] = "client-1" }); err != nil {
		t.Errorf("azp must substitute for client_id: %v", err)
	}
	if err := verify(func(m map[string]any) { m["aud"] = "node-1.example.com" }); err != nil {
		t.Errorf("string-form aud must decode and match: %v", err)
	}
}

func TestAudienceMatching(t *testing.T) {
	cases := []struct {
		aud  string
		host string
		want bool
	}{
		{"node-1.example.com", "node-1.example.com", true},
		{"https://node-1.example.com", "node-1.example.com", true},
		{"node-*.example.com", "node-42.example.com", true},
		{"*.example.com", "a.b.example.com", true},
		{"NODE-1.EXAMPLE.COM", "node-1.example.com", true},
		{"node-1.example.com", "node-2.example.com", false},
		{"*.example.com", "example.org", false},
	}
	for _, tc := range cases {
		if got := is10.AudienceMatches(is10.Audience{tc.aud}, tc.host); got != tc.want {
			t.Errorf("aud %q vs %q = %v, want %v", tc.aud, tc.host, got, tc.want)
		}
	}
}

func TestPathAuthorization(t *testing.T) {
	tok := mint(t, baseClaims(), "RS512", "")
	c, err := is10.VerifyWithKeys(tok, []is10.JWK{testJWK("")})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	cases := []struct {
		method, path string
		allow        bool
	}{
		// Always-readable base paths.
		{"GET", "/", true},
		{"GET", "/x-nmos", true},
		{"GET", "/x-nmos/", true},
		// API base: scope OR claim grants read. `registration` is
		// scope-only in the base claims; `query` has a claim too.
		{"GET", "/x-nmos/registration", true},
		{"GET", "/x-nmos/registration/v1.3", true},
		{"GET", "/x-nmos/query/v1.3/", true},
		// events: neither scope nor claim.
		{"GET", "/x-nmos/events", false},
		// Deep paths need the x-nmos claim — scope alone (registration)
		// is NOT enough (path table row 5).
		{"GET", "/x-nmos/registration/v1.3/resource", false},
		{"GET", "/x-nmos/query/v1.3/senders", true},
		{"POST", "/x-nmos/query/v1.3/subscriptions", true},
		{"DELETE", "/x-nmos/query/v1.3/senders/abc", false},
		{"PATCH", "/x-nmos/connection/v1.1/single/senders/abc/staged", true},
		{"PATCH", "/x-nmos/connection/v1.1/bulk/senders", false},
		// URL normalization: ../ must not escape a narrow specifier.
		{"PATCH", "/x-nmos/connection/v1.1/single/../bulk/senders", false},
		// Write verbs never ride the implicit read paths.
		{"POST", "/x-nmos", false},
		{"POST", "/", false},
	}
	for _, tc := range cases {
		err := is10.Authorize(c, tc.method, tc.path)
		if (err == nil) != tc.allow {
			t.Errorf("%s %s: allow=%v, want %v (%v)", tc.method, tc.path, err == nil, tc.allow, err)
		}
	}
}

func TestMetadataAndJWKSAndTokenResponse(t *testing.T) {
	meta := `{"issuer":"https://auth.example.com","authorization_endpoint":"https://a/authorize","token_endpoint":"https://a/token","jwks_uri":"https://a/jwks","registration_endpoint":"https://a/register","response_types_supported":["code"],"code_challenge_methods_supported":["S256","plain"],"extra":"tolerated"}`
	m, err := is10.DecodeMetadata([]byte(meta))
	if err != nil {
		t.Fatalf("metadata: %v", err)
	}
	if m.JwksURI != "https://a/jwks" {
		t.Errorf("metadata lost: %+v", m)
	}
	if _, err := is10.DecodeMetadata([]byte(`{"issuer":"x"}`)); err == nil {
		t.Error("incomplete metadata must be rejected")
	}

	raw, _ := json.Marshal(is10.JWKS{Keys: []is10.JWK{testJWK("k1")}})
	s, err := is10.DecodeJWKS(raw)
	if err != nil || len(s.Keys) != 1 {
		t.Fatalf("jwks: %v %d", err, len(s.Keys))
	}
	if _, err := is10.DecodeJWKS([]byte(`{}`)); err == nil {
		t.Error("jwks without keys must be rejected")
	}

	tr := is10.TokenResponse{AccessToken: "t", ExpiresIn: 60, TokenType: "bearer"}
	if err := tr.Validate(); err != nil {
		t.Errorf("Bearer must be case-insensitive: %v", err)
	}
	tr.TokenType = "mac"
	if err := tr.Validate(); err == nil {
		t.Error("non-Bearer token_type must be rejected")
	}
}

func TestRegistryWiring(t *testing.T) {
	c, ok := is10.Get("v1.0")
	if !ok {
		t.Fatal("v1.0 codec not registered")
	}
	if c.SpecID() != is10.SpecID || c.SpecPatch() != v10.SpecPatch {
		t.Errorf("identity: %s %s %s", c.SpecID(), c.APIVer(), c.SpecPatch())
	}
	if got := is10.SupportedVersions(); len(got) != 1 || got[0] != "v1.0" {
		t.Errorf("supported = %v", got)
	}
}
