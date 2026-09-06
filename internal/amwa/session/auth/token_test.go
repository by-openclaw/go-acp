package auth

// The NMOS reading of an IS-10 access token. Expected behaviour comes
// from the AMWA v1.0.0 schemas + the Behaviour documents (Access
// Tokens, Resource Servers), not from working code: RS512-only,
// required claims, audience wildcards, and the path-authorization
// table.
//
// These cases moved here from codec/is10 unchanged in substance when
// the JWT concern was separated — the codec keeps the wire documents,
// this package keeps the policy.

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

	jwt "dhs/internal/auth"
)

var tokenKey, _ = rsa.GenerateKey(rand.Reader, 2048)

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
	sig, err := rsa.SignPKCS1v15(rand.Reader, tokenKey, crypto.SHA512, sum[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return input + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func tokenJWK(kid string) jwt.JWK {
	pub := &tokenKey.PublicKey
	return jwt.JWK{
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
	c, err := Verify(tok, []jwt.JWK{tokenJWK("k1")})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if c.Iss != "https://auth.example.com" || c.ClientID != "client-1" {
		t.Errorf("claims lost: %+v", c)
	}
	if p, ok := c.APIs["query"]; !ok || len(p.Write) != 1 || p.Write[0] != "subscriptions/*" {
		t.Errorf("x-nmos-query claim lost: %+v", c.APIs)
	}
	if err := ValidateClaims(c, []string{"node-1.example.com"}, time.Now(), 30*time.Second); err != nil {
		t.Errorf("valid claims rejected: %v", err)
	}
	if c.EffectiveClientID() != "client-1" {
		t.Errorf("client id = %q", c.EffectiveClientID())
	}
}

func TestAlgorithmPinning(t *testing.T) {
	// RS256, or the classic alg=none downgrade, must be rejected at
	// PARSE time regardless of the signature bytes.
	for _, alg := range []string{"RS256", "HS512", "none"} {
		tok := mint(t, baseClaims(), alg, "")
		if _, err := Verify(tok, []jwt.JWK{tokenJWK("")}); err == nil {
			t.Errorf("alg %s must be rejected", alg)
		}
		if _, err := Parse(tok); err == nil {
			t.Errorf("alg %s must be rejected by Parse too", alg)
		}
	}
	// Tampered payload fails the signature.
	tok := mint(t, baseClaims(), "RS512", "")
	parts := strings.Split(tok, ".")
	forged := base64.RawURLEncoding.EncodeToString(
		[]byte(`{"iss":"x","sub":"y","aud":"z","exp":9e9,"client_id":"c"}`))
	if _, err := Verify(parts[0]+"."+forged+"."+parts[2], []jwt.JWK{tokenJWK("")}); err == nil {
		t.Error("tampered payload must fail verification")
	}
}

// Parse reads the claims of an UNVERIFIED token — the issuer probe the
// resource server uses to go and fetch a signing key it does not hold.
// It must therefore succeed on a token no key verifies.
func TestParseSkipsSignature(t *testing.T) {
	tok := mint(t, baseClaims(), "RS512", "k1")
	if _, err := Verify(tok, nil); err == nil {
		t.Fatal("verification with no keys must fail")
	}
	c, err := Parse(tok)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if c.Iss != "https://auth.example.com" {
		t.Errorf("iss lost: %+v", c)
	}
	if _, err := Parse("not.a.jws"); err == nil {
		t.Error("malformed token must be rejected")
	}
}

// A malformed x-nmos-* claim is an error, not an absent grant. Were it
// silently skipped, the request would 403 with a message about a
// missing permission that the token actually carries.
func TestMalformedNmosClaimIsAnError(t *testing.T) {
	m := baseClaims()
	m["x-nmos-query"] = "not-an-object"
	tok := mint(t, m, "RS512", "k1")
	if _, err := Verify(tok, []jwt.JWK{tokenJWK("k1")}); err == nil {
		t.Fatal("malformed x-nmos claim must be rejected")
	} else if !strings.Contains(err.Error(), "x-nmos-query") {
		t.Errorf("error must name the claim: %v", err)
	}
	if _, err := Parse(tok); err == nil {
		t.Error("Parse must reject it too")
	}
}

// Private claims that are not x-nmos-* are none of this package's
// business and must not become APIs entries.
func TestNonNmosPrivateClaimsIgnored(t *testing.T) {
	m := baseClaims()
	m["jti"] = "abc-123"
	m["https://vendor.example/custom"] = map[string]any{"read": []string{"*"}}
	tok := mint(t, m, "RS512", "k1")
	c, err := Verify(tok, []jwt.JWK{tokenJWK("k1")})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if len(c.APIs) != 2 {
		t.Errorf("APIs = %v, want only the two x-nmos claims", c.APIs)
	}
}

func TestClaimValidation(t *testing.T) {
	now := time.Now()
	leeway := 30 * time.Second
	verify := func(mutate func(map[string]any)) error {
		m := baseClaims()
		mutate(m)
		tok := mint(t, m, "RS512", "")
		c, err := Verify(tok, []jwt.JWK{tokenJWK("")})
		if err != nil {
			t.Fatalf("verify: %v", err)
		}
		return ValidateClaims(c, []string{"10.0.0.5", "node-1.example.com"}, now, leeway)
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
		// A port on either side is stripped rather than failing every
		// request from an issuer that scoped the token sloppily.
		{"https://node-1.example.com:8080/", "node-1.example.com", true},
		{"node-1.example.com", "node-1.example.com:443", true},
	}
	for _, tc := range cases {
		if got := AudienceMatches(jwt.Audience{tc.aud}, tc.host); got != tc.want {
			t.Errorf("aud %q vs %q = %v, want %v", tc.aud, tc.host, got, tc.want)
		}
	}
	// Any host in the set is enough; none is a rejection.
	aud := jwt.Audience{"node-2.example.com"}
	if !AudienceMatchesAny(aud, []string{"10.0.0.5", "node-2.example.com"}) {
		t.Error("a later host must still match")
	}
	if AudienceMatchesAny(aud, []string{"10.0.0.5"}) {
		t.Error("no matching host must be a rejection")
	}
}

func TestPathAuthorization(t *testing.T) {
	tok := mint(t, baseClaims(), "RS512", "")
	c, err := Verify(tok, []jwt.JWK{tokenJWK("")})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	cases := []struct {
		method, path string
		allow        bool
	}{
		// Always-readable base paths.
		{"GET", "/", true},
		{"HEAD", "/x-nmos", true},
		{"OPTIONS", "/x-nmos", true},
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
		// A write at the API root needs a specifier that matches the
		// empty remainder; "subscriptions/*" does not.
		{"POST", "/x-nmos/query/v1.3", false},
		// Outside the NMOS tree no IS-10 grant can apply.
		{"GET", "/metrics", false},
		// Not an NMOS API verb at all.
		{"TRACE", "/x-nmos/query/v1.3/senders", false},
	}
	for _, tc := range cases {
		err := Authorize(c, tc.method, tc.path)
		if (err == nil) != tc.allow {
			t.Errorf("%s %s: allow=%v, want %v (%v)", tc.method, tc.path, err == nil, tc.allow, err)
		}
	}
}

// A "*" write grant is the only thing that covers the API root, and it
// must actually work — the negative case above proves the narrow
// specifier fails, this proves the broad one does not.
func TestWriteAtApiRootWithWildcardGrant(t *testing.T) {
	m := baseClaims()
	m["x-nmos-query"] = map[string]any{"read": []string{"*"}, "write": []string{"*"}}
	tok := mint(t, m, "RS512", "")
	c, err := Verify(tok, []jwt.JWK{tokenJWK("")})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if err := Authorize(c, "POST", "/x-nmos/query/v1.3"); err != nil {
		t.Errorf("a wildcard write grant must cover the API root: %v", err)
	}
}

func TestHasScope(t *testing.T) {
	c := Token{Claims: jwt.Claims{Scope: "registration  query connection"}}
	for _, api := range []string{"registration", "query", "connection"} {
		if !c.HasScope(api) {
			t.Errorf("scope %q not found in %q", api, c.Scope)
		}
	}
	if c.HasScope("events") {
		t.Error("absent scope must not match")
	}
	if (Token{}).HasScope("query") {
		t.Error("empty scope must not match")
	}
}

// A read grant is a WHITELIST, not a formality: a claim that names one
// sub-tree must not open the rest of the API. The base claims grant
// read "*" everywhere, so this needs its own narrower token.
func TestNarrowReadSpecifierDenies(t *testing.T) {
	m := baseClaims()
	m["x-nmos-query"] = map[string]any{"read": []string{"senders/*"}}
	tok := mint(t, m, "RS512", "")
	c, err := Verify(tok, []jwt.JWK{tokenJWK("")})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if err := Authorize(c, "GET", "/x-nmos/query/v1.3/senders/abc"); err != nil {
		t.Errorf("granted sub-tree must be readable: %v", err)
	}
	if err := Authorize(c, "GET", "/x-nmos/query/v1.3/flows"); err == nil {
		t.Error("a read specifier naming senders must not grant flows")
	}
}
