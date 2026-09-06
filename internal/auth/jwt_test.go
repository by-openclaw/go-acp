package auth

// The auth battery. One suite for the JWT concern, reused by every protocol
// that authenticates instead of each one testing its own copy.
//
// The downgrade cases are the reason this file is worth reading: alg=none and
// alg-substitution are how JWT verifiers get broken in practice, and a test
// that only proves "a good token verifies" would pass on a verifier that
// accepts everything.

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"strings"
	"testing"
	"time"
)

// signer builds tokens the way an authorization server would.
type signer struct {
	key *rsa.PrivateKey
	kid string
}

func newSigner(t *testing.T, kid string) *signer {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return &signer{key: k, kid: kid}
}

func (s *signer) jwk() JWK {
	return JWK{
		Kty: "RSA",
		Kid: s.kid,
		N:   base64.RawURLEncoding.EncodeToString(s.key.N.Bytes()),
		E:   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(s.key.E)).Bytes()),
	}
}

// sign produces a compact JWS. alg selects the digest; claims is any JSON.
func (s *signer) sign(t *testing.T, alg string, claims map[string]any) string {
	t.Helper()
	b64 := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return base64.RawURLEncoding.EncodeToString(b)
	}
	hdr := map[string]any{"typ": "JWT", "alg": alg}
	if s.kid != "" {
		hdr["kid"] = s.kid
	}
	input := b64(hdr) + "." + b64(claims)

	hash, sum, err := hashFor(alg)
	if err != nil {
		t.Fatalf("hashFor(%s): %v", alg, err)
	}
	sig, err := rsa.SignPKCS1v15(rand.Reader, s.key, hash, sum([]byte(input)))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return input + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func liveClaims() map[string]any {
	return map[string]any{
		"iss":       "https://auth.example",
		"sub":       "dhs",
		"aud":       "https://node.example",
		"exp":       float64(time.Now().Add(time.Hour).Unix()),
		"client_id": "dhs-cli",
	}
}

// --- the happy path, per algorithm ---------------------------------------

func TestVerifyRoundTripEveryAlgorithm(t *testing.T) {
	for _, alg := range []string{AlgRS256, AlgRS384, AlgRS512} {
		t.Run(alg, func(t *testing.T) {
			s := newSigner(t, "k1")
			tok := s.sign(t, alg, liveClaims())

			c, err := VerifyWithKeys(tok, []JWK{s.jwk()}, alg)
			if err != nil {
				t.Fatalf("VerifyWithKeys: %v", err)
			}
			if c.Iss != "https://auth.example" || c.Sub != "dhs" {
				t.Errorf("claims not decoded: %+v", c)
			}
			if got := c.EffectiveClientID(); got != "dhs-cli" {
				t.Errorf("EffectiveClientID = %q", got)
			}
		})
	}
}

// --- the downgrades, which are the whole point ---------------------------

// alg=none is the canonical JWT break: a token with no signature that a naive
// verifier accepts because it believed the header.
func TestRejectsAlgNone(t *testing.T) {
	b64 := func(v any) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	tok := b64(map[string]any{"alg": "none", "typ": "JWT"}) + "." + b64(liveClaims()) + "."

	if _, err := VerifyWithKeys(tok, nil, AlgRS512); err == nil {
		t.Fatal("alg=none was accepted")
	}
}

// A token signed with one permitted algorithm must not pass when a DIFFERENT
// algorithm is the only one permitted — the pin has to bind the actual token,
// not merely appear in a list somewhere.
func TestRejectsAlgSubstitution(t *testing.T) {
	s := newSigner(t, "k1")
	tok := s.sign(t, AlgRS256, liveClaims())

	if _, err := VerifyWithKeys(tok, []JWK{s.jwk()}, AlgRS512); err == nil {
		t.Fatal("an RS256 token verified while only RS512 was permitted")
	}
}

// Refusing to parse with no permitted list is a guard against the caller who
// forgets the argument and silently accepts anything.
func TestRefusesEmptyPermittedList(t *testing.T) {
	s := newSigner(t, "k1")
	if _, _, _, _, err := ParseToken(s.sign(t, AlgRS512, liveClaims())); err == nil {
		t.Fatal("ParseToken accepted a token with no permitted algorithms")
	}
}

func TestRejectsForgedSignature(t *testing.T) {
	good, attacker := newSigner(t, "k1"), newSigner(t, "k1")
	tok := attacker.sign(t, AlgRS512, liveClaims())

	if _, err := VerifyWithKeys(tok, []JWK{good.jwk()}, AlgRS512); err == nil {
		t.Fatal("a token signed by an unknown key verified")
	}
}

// --- key selection --------------------------------------------------------

// kid is a hint, not a guarantee: a server that rotated keys before its
// published kid caught up must still be verifiable.
func TestFallsBackToOtherKeysWhenKidDoesNotMatch(t *testing.T) {
	other, real := newSigner(t, "old"), newSigner(t, "new")
	tok := real.sign(t, AlgRS512, liveClaims())

	// Announce a kid nobody has, so only the exhaustive pass can succeed.
	realJWK := real.jwk()
	realJWK.Kid = "unpublished"

	if _, err := VerifyWithKeys(tok, []JWK{other.jwk(), realJWK}, AlgRS512); err != nil {
		t.Fatalf("VerifyWithKeys did not fall back to the other keys: %v", err)
	}
}

func TestVerifyWithNoKeys(t *testing.T) {
	s := newSigner(t, "k1")
	_, err := VerifyWithKeys(s.sign(t, AlgRS512, liveClaims()), nil, AlgRS512)
	if err == nil || !strings.Contains(err.Error(), "no keys available") {
		t.Fatalf("err = %v, want a no-keys error", err)
	}
}

// A key that cannot be converted is skipped, not fatal. It shares the token's
// kid deliberately: kid-matching keys are tried FIRST, so this is the only
// arrangement that actually forces the skip path rather than letting the good
// key answer before the bad one is ever reached.
func TestSkipsUnusableKeys(t *testing.T) {
	s := newSigner(t, "k1")
	bad := JWK{Kty: "oct", Kid: "k1"} // same kid, unusable type

	if _, err := VerifyWithKeys(s.sign(t, AlgRS512, liveClaims()), []JWK{bad, s.jwk()}, AlgRS512); err != nil {
		t.Fatalf("a non-RSA key in the set broke verification: %v", err)
	}
}

// A caller may permit an algorithm this package cannot verify — HS256 is
// symmetric and has no place here. ParseToken's pin lets it through because
// the caller asked for it; hashFor is the backstop that refuses rather than
// falling back to some default digest.
func TestRejectsPermittedButUnverifiableAlg(t *testing.T) {
	b64 := func(v any) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	tok := b64(map[string]any{"alg": "HS256"}) + "." + b64(liveClaims()) + ".sig"

	_, err := VerifyWithKeys(tok, nil, "HS256")
	if err == nil || !strings.Contains(err.Error(), "no verifier for alg") {
		t.Fatalf("err = %v, want a no-verifier error", err)
	}
}

// --- malformed input ------------------------------------------------------

func TestParseTokenRejectsMalformed(t *testing.T) {
	tests := []struct{ name, tok string }{
		{"empty", ""},
		{"two segments", "aaa.bbb"},
		{"four segments", "a.b.c.d"},
		{"header not base64", "!!!.bbb.ccc"},
		{"header not json", base64.RawURLEncoding.EncodeToString([]byte("nope")) + ".bbb.ccc"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, _, _, err := ParseToken(tc.tok, AlgRS512); err == nil {
				t.Error("accepted a malformed token")
			}
		})
	}
}

func TestParseTokenRejectsBadPayloadAndSignature(t *testing.T) {
	b64 := func(v any) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	hdr := b64(map[string]any{"alg": AlgRS512})

	if _, _, _, _, err := ParseToken(hdr+".!!!.ccc", AlgRS512); err == nil {
		t.Error("accepted a non-base64 payload")
	}
	notJSON := base64.RawURLEncoding.EncodeToString([]byte("nope"))
	if _, _, _, _, err := ParseToken(hdr+"."+notJSON+".ccc", AlgRS512); err == nil {
		t.Error("accepted a non-JSON payload")
	}
	if _, _, _, _, err := ParseToken(hdr+"."+b64(liveClaims())+".!!!", AlgRS512); err == nil {
		t.Error("accepted a non-base64 signature")
	}
}

// --- claims ---------------------------------------------------------------

// aud is a string OR an array on the wire; both must decode to the same shape.
func TestAudienceAcceptsBothWireForms(t *testing.T) {
	var one, many Claims
	if err := json.Unmarshal([]byte(`{"aud":"a"}`), &one); err != nil {
		t.Fatalf("string aud: %v", err)
	}
	if err := json.Unmarshal([]byte(`{"aud":["a","b"]}`), &many); err != nil {
		t.Fatalf("array aud: %v", err)
	}
	if len(one.Aud) != 1 || one.Aud[0] != "a" {
		t.Errorf("string form = %v", one.Aud)
	}
	if len(many.Aud) != 2 {
		t.Errorf("array form = %v", many.Aud)
	}
	if err := json.Unmarshal([]byte(`{"aud":42}`), &one); err == nil {
		t.Error("a numeric aud was accepted")
	}
	// Round-trips as the array form.
	b, err := json.Marshal(Audience{"a"})
	if err != nil || string(b) != `["a"]` {
		t.Errorf("MarshalJSON = %s, %v", b, err)
	}
}

// The seam that keeps this package protocol-free: everything unrecognised is
// handed back raw for the protocol to interpret.
func TestPrivateClaimsArePreserved(t *testing.T) {
	var c Claims
	raw := `{"iss":"i","sub":"s","exp":1,"x-nmos-registration":{"read":["*"]},"jti":"abc"}`
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := c.Private["x-nmos-registration"]; !ok {
		t.Error("private claim was dropped — protocols cannot apply their own rules")
	}
	if _, ok := c.Private["jti"]; !ok {
		t.Error("unknown registered-ish claim was dropped")
	}
	if _, ok := c.Private["iss"]; ok {
		t.Error("a decoded claim was left in Private as well")
	}
}

func TestClaimsRejectsMalformed(t *testing.T) {
	var c Claims
	if err := json.Unmarshal([]byte(`not json`), &c); err == nil {
		t.Error("accepted non-JSON claims")
	}
	// Syntactically valid JSON that is not an object: this is what actually
	// reaches UnmarshalJSON's own decode, since invalid syntax is rejected by
	// encoding/json before the method is ever called.
	if err := json.Unmarshal([]byte(`[1,2]`), &c); err == nil {
		t.Error("accepted a JSON array as a claim set")
	}
	if err := json.Unmarshal([]byte(`{"iss":42}`), &c); err == nil {
		t.Error("accepted a numeric iss")
	}
}

func TestEffectiveClientIDFallsBackToAzp(t *testing.T) {
	if got := (&Claims{Azp: "via-azp"}).EffectiveClientID(); got != "via-azp" {
		t.Errorf("= %q, want via-azp", got)
	}
	if got := (&Claims{ClientID: "cid", Azp: "azp"}).EffectiveClientID(); got != "cid" {
		t.Errorf("= %q, want client_id to win", got)
	}
}

// --- the clock ------------------------------------------------------------

func TestValidateTime(t *testing.T) {
	now := time.Now()
	f := func(t time.Time) *float64 { v := float64(t.Unix()); return &v }

	tests := []struct {
		name    string
		c       Claims
		wantErr string
	}{
		{"no exp", Claims{}, "exp claim is required"},
		{"expired", Claims{Exp: f(now.Add(-time.Hour))}, "expired"},
		{"iat in future", Claims{Exp: f(now.Add(time.Hour)), Iat: f(now.Add(time.Hour))}, "iat is in the future"},
		{"nbf in future", Claims{Exp: f(now.Add(time.Hour)), Nbf: f(now.Add(time.Hour))}, "not valid before"},
		{"valid", Claims{Exp: f(now.Add(time.Hour)), Iat: f(now.Add(-time.Minute))}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateTime(tc.c, now, 0)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("err = %v, want it to mention %q", err, tc.wantErr)
			}
		})
	}
}

// Leeway absorbs skew in BOTH directions. A device a second off NTP that
// rejects every token looks like an auth bug and is not one.
func TestValidateTimeLeewayWorksBothWays(t *testing.T) {
	now := time.Now()
	f := func(t time.Time) *float64 { v := float64(t.Unix()); return &v }

	justExpired := Claims{Exp: f(now.Add(-30 * time.Second))}
	if err := ValidateTime(justExpired, now, time.Minute); err != nil {
		t.Errorf("leeway did not absorb a 30s-expired token: %v", err)
	}
	justFuture := Claims{Exp: f(now.Add(time.Hour)), Nbf: f(now.Add(30 * time.Second))}
	if err := ValidateTime(justFuture, now, time.Minute); err != nil {
		t.Errorf("leeway did not absorb a 30s-early nbf: %v", err)
	}
}

// --- JWKS -----------------------------------------------------------------

func TestDecodeJWKS(t *testing.T) {
	s := newSigner(t, "k1")
	b, err := json.Marshal(JWKS{Keys: []JWK{s.jwk()}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	set, err := DecodeJWKS(b)
	if err != nil || len(set.Keys) != 1 {
		t.Fatalf("DecodeJWKS = %+v, %v", set, err)
	}

	// An empty set is legal; a body WITHOUT `keys` is a different document
	// and must not be read as "zero keys".
	if _, err := DecodeJWKS([]byte(`{"keys":[]}`)); err != nil {
		t.Errorf("empty key set rejected: %v", err)
	}
	if _, err := DecodeJWKS([]byte(`{}`)); err == nil {
		t.Error("a body with no keys member was accepted")
	}
	if _, err := DecodeJWKS([]byte(`<html>`)); err == nil {
		t.Error("an HTML error page was accepted as a key set")
	}
}

func TestPublicKeyRejectsBadJWKs(t *testing.T) {
	tests := []struct {
		name string
		j    JWK
	}{
		{"not rsa", JWK{Kty: "EC", Kid: "e"}},
		{"missing n/e", JWK{Kty: "RSA", Kid: "m"}},
		{"n not base64", JWK{Kty: "RSA", Kid: "n", N: "!!!", E: "AQAB"}},
		{"e not base64", JWK{Kty: "RSA", Kid: "e2", N: "AQAB", E: "!!!"}},
		{"zero exponent", JWK{Kty: "RSA", Kid: "z", N: "AQAB", E: base64.RawURLEncoding.EncodeToString([]byte{0})}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.j.PublicKey(); err == nil {
				t.Error("accepted an unusable JWK")
			}
		})
	}
	// kty is case-insensitive per RFC 7517.
	s := newSigner(t, "k1")
	lower := s.jwk()
	lower.Kty = "rsa"
	if _, err := lower.PublicKey(); err != nil {
		t.Errorf("lowercase kty rejected: %v", err)
	}
}

func TestHashForRejectsUnknownAlg(t *testing.T) {
	if _, _, err := hashFor("HS256"); err == nil {
		t.Error("hashFor accepted a symmetric algorithm")
	}
	// Sanity: the three we support map to distinct digests.
	for alg, want := range map[string]int{AlgRS256: sha256.Size, AlgRS384: sha512.Size384, AlgRS512: sha512.Size} {
		_, sum, err := hashFor(alg)
		if err != nil {
			t.Fatalf("hashFor(%s): %v", alg, err)
		}
		if got := len(sum([]byte("x"))); got != want {
			t.Errorf("%s digest = %d bytes, want %d", alg, got, want)
		}
	}
}

// --- bearer ---------------------------------------------------------------

func TestBearerToken(t *testing.T) {
	tests := []struct{ name, hdr, want string }{
		{"standard", "Bearer abc", "abc"},
		{"lowercase scheme", "bearer abc", "abc"},
		{"mixed case", "BeArEr abc", "abc"},
		{"extra spaces", "Bearer   abc  ", "abc"},
		{"no header", "", ""},
		{"wrong scheme", "Basic abc", ""},
		{"scheme only", "Bearer", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := http.NewRequest(http.MethodGet, "http://x/", nil)
			if tc.hdr != "" {
				r.Header.Set("Authorization", tc.hdr)
			}
			if got := BearerToken(r); got != tc.want {
				t.Errorf("= %q, want %q", got, tc.want)
			}
		})
	}
	if got := BearerToken(nil); got != "" {
		t.Errorf("nil request = %q", got)
	}
}

// --- matching -------------------------------------------------------------

func TestWildcardMatch(t *testing.T) {
	tests := []struct {
		pattern, s string
		want       bool
	}{
		{"exact", "exact", true},
		{"exact", "other", false},
		{"*", "anything", true},
		{"node-*.example.com", "node-1.example.com", true},
		{"node-*.example.com", "other.example.com", false},
		{"*.example.com", "a.b.example.com", true},
		{"a*b*c", "azzbzzc", true},
		{"a*b*c", "azzc", false},
		{"prefix*", "prefix", true},
		{"*suffix", "suffix", true},
	}
	for _, tc := range tests {
		if got := WildcardMatch(tc.pattern, tc.s); got != tc.want {
			t.Errorf("WildcardMatch(%q, %q) = %v, want %v", tc.pattern, tc.s, got, tc.want)
		}
	}
}

func TestSplitHostPort(t *testing.T) {
	tests := []struct {
		in, host, port string
		ok             bool
	}{
		{"host:80", "host", "80", true},
		{"host", "host", "", false},
		{"host:", "host:", "", false},
		{"https://host", "https://host", "", false}, // scheme colon is not a port
		{"host:abc", "host:abc", "", false},
	}
	for _, tc := range tests {
		h, p, ok := SplitHostPort(tc.in)
		if h != tc.host || p != tc.port || ok != tc.ok {
			t.Errorf("SplitHostPort(%q) = (%q,%q,%v), want (%q,%q,%v)",
				tc.in, h, p, ok, tc.host, tc.port, tc.ok)
		}
	}
}
