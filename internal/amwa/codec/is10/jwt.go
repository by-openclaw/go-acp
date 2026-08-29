package is10

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"time"
)

// Header is the JOSE header of an IS-10 access token. The spec pins
// exactly one algorithm: RS512 (RSASSA-PKCS1-v1_5 with SHA-512).
type Header struct {
	Typ string `json:"typ,omitempty"`
	Alg string `json:"alg"`
	Kid string `json:"kid,omitempty"`
}

// AlgRS512 is the only JWS algorithm IS-10 permits.
const AlgRS512 = "RS512"

// ParseToken splits a compact JWS into its verified-later parts.
// Only structure and the algorithm pin are checked here — signature
// and claims live in VerifyWithKeys / ValidateClaims.
func ParseToken(raw string) (h Header, c Claims, signingInput string, sig []byte, err error) {
	parts := strings.Split(strings.TrimSpace(raw), ".")
	if len(parts) != 3 {
		return Header{}, Claims{}, "", nil, fmt.Errorf("is10: token is not a compact JWS (want 3 segments, got %d)", len(parts))
	}
	dec := func(seg, what string) ([]byte, error) {
		b, err := base64.RawURLEncoding.DecodeString(seg)
		if err != nil {
			return nil, fmt.Errorf("is10: token %s: %w", what, err)
		}
		return b, nil
	}
	hRaw, err := dec(parts[0], "header")
	if err != nil {
		return Header{}, Claims{}, "", nil, err
	}
	if err := json.Unmarshal(hRaw, &h); err != nil {
		return Header{}, Claims{}, "", nil, fmt.Errorf("is10: token header: %w", err)
	}
	// The algorithm is pinned, and pinned BEFORE any verification —
	// accepting the header's word for other algorithms is the classic
	// JWS downgrade attack.
	if h.Alg != AlgRS512 {
		return Header{}, Claims{}, "", nil, fmt.Errorf("is10: token alg %q: IS-10 permits RS512 only", h.Alg)
	}
	cRaw, err := dec(parts[1], "payload")
	if err != nil {
		return Header{}, Claims{}, "", nil, err
	}
	if err := json.Unmarshal(cRaw, &c); err != nil {
		return Header{}, Claims{}, "", nil, fmt.Errorf("is10: token payload: %w", err)
	}
	sig, err = dec(parts[2], "signature")
	if err != nil {
		return Header{}, Claims{}, "", nil, err
	}
	return h, c, parts[0] + "." + parts[1], sig, nil
}

// PublicKey converts an RSA JWK to the stdlib form.
func (j JWK) PublicKey() (*rsa.PublicKey, error) {
	if !strings.EqualFold(j.Kty, "RSA") {
		return nil, fmt.Errorf("is10: jwk kty %q: not an RSA key", j.Kty)
	}
	if j.N == "" || j.E == "" {
		return nil, fmt.Errorf("is10: jwk %q: missing n/e members", j.Kid)
	}
	nb, err := base64.RawURLEncoding.DecodeString(j.N)
	if err != nil {
		return nil, fmt.Errorf("is10: jwk %q n: %w", j.Kid, err)
	}
	eb, err := base64.RawURLEncoding.DecodeString(j.E)
	if err != nil {
		return nil, fmt.Errorf("is10: jwk %q e: %w", j.Kid, err)
	}
	e := new(big.Int).SetBytes(eb)
	if !e.IsInt64() || e.Int64() <= 0 {
		return nil, fmt.Errorf("is10: jwk %q: invalid exponent", j.Kid)
	}
	return &rsa.PublicKey{N: new(big.Int).SetBytes(nb), E: int(e.Int64())}, nil
}

// verifyRS512 checks one signature against one key.
func verifyRS512(signingInput string, sig []byte, pub *rsa.PublicKey) error {
	sum := sha512.Sum512([]byte(signingInput))
	return rsa.VerifyPKCS1v15(pub, crypto.SHA512, sum[:], sig)
}

// VerifyWithKeys parses raw and verifies its signature against the
// key set: the kid-matching key first, then (per Behaviour - Resource
// Servers.md "all valid JWKs SHOULD be tried") every other RSA key
// until one verifies or the set is exhausted. Claims are NOT
// validated here — call ValidateClaims on the result.
func VerifyWithKeys(raw string, keys []JWK) (Claims, error) {
	h, c, input, sig, err := ParseToken(raw)
	if err != nil {
		return Claims{}, err
	}
	ordered := make([]JWK, 0, len(keys))
	if h.Kid != "" {
		for _, k := range keys {
			if k.Kid == h.Kid {
				ordered = append(ordered, k)
			}
		}
	}
	for _, k := range keys {
		if h.Kid != "" && k.Kid == h.Kid {
			continue
		}
		ordered = append(ordered, k)
	}
	var lastErr error
	for _, k := range ordered {
		pub, err := k.PublicKey()
		if err != nil {
			lastErr = err
			continue
		}
		if err := verifyRS512(input, sig, pub); err == nil {
			return c, nil
		} else {
			lastErr = err
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no keys available")
	}
	return Claims{}, fmt.Errorf("is10: token signature verification failed: %w", lastErr)
}

// ValidateClaims enforces the claim rules of Behaviour - Access
// Tokens.md + Resource Servers.md against this resource server:
//
//   - iss, sub, aud, exp REQUIRED;
//   - exp in the past / iat or nbf in the future reject (leeway
//     absorbs clock skew both ways);
//   - client_id required unless azp present;
//   - aud must match ONE of this server's identities (wildcards
//     allowed, scheme-prefixed URI or bare domain, ports never).
//
// hosts carries every name this server answers to — advertise host,
// OS hostname, hostname.local — because issuers commonly scope tokens
// by DNS wildcards ("https://*.local") that an IP literal can never
// match.
func ValidateClaims(c Claims, hosts []string, now time.Time, leeway time.Duration) error {
	if c.Iss == "" {
		return fmt.Errorf("is10: token: iss claim is required")
	}
	if c.Sub == "" {
		return fmt.Errorf("is10: token: sub claim is required")
	}
	if len(c.Aud) == 0 {
		return fmt.Errorf("is10: token: aud claim is required")
	}
	if c.Exp == nil {
		return fmt.Errorf("is10: token: exp claim is required")
	}
	unix := float64(now.Unix())
	if *c.Exp < unix-leeway.Seconds() {
		return fmt.Errorf("is10: token expired at %v", time.Unix(int64(*c.Exp), 0).UTC())
	}
	if c.Iat != nil && *c.Iat > unix+leeway.Seconds() {
		return fmt.Errorf("is10: token iat is in the future")
	}
	if c.Nbf != nil && *c.Nbf > unix+leeway.Seconds() {
		return fmt.Errorf("is10: token not valid before %v", time.Unix(int64(*c.Nbf), 0).UTC())
	}
	if c.ClientID == "" && c.Azp == "" {
		return fmt.Errorf("is10: token: client_id claim is required when azp is absent")
	}
	for _, h := range hosts {
		if AudienceMatches(c.Aud, h) {
			return nil
		}
	}
	return fmt.Errorf("is10: token aud %v does not name this server (%v)", []string(c.Aud), hosts)
}

// AudienceMatches reports whether any aud entry names host. Entries
// may be bare domain names or scheme-prefixed URIs (never carrying
// ports, paths or queries per the spec — but a sloppy issuer's port
// is stripped rather than silently failing every request), and may
// carry '*' wildcards (RFC 4592 style, e.g. "node-*.example.com").
func AudienceMatches(aud Audience, host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if h, _, ok := splitHostPort(host); ok {
		host = h
	}
	for _, a := range aud {
		a = strings.ToLower(strings.TrimSpace(a))
		if i := strings.Index(a, "://"); i >= 0 {
			a = a[i+3:]
		}
		if i := strings.IndexByte(a, '/'); i >= 0 {
			a = a[:i]
		}
		if h, _, ok := splitHostPort(a); ok {
			a = h
		}
		if wildcardMatch(a, host) {
			return true
		}
	}
	return false
}

// splitHostPort is a forgiving host[:port] splitter (no brackets —
// NMOS aud values are domain names).
func splitHostPort(s string) (host, port string, ok bool) {
	i := strings.LastIndexByte(s, ':')
	if i < 0 {
		return s, "", false
	}
	p := s[i+1:]
	if p == "" {
		return s, "", false
	}
	for _, r := range p {
		if r < '0' || r > '9' {
			return s, "", false
		}
	}
	return s[:i], p, true
}

// wildcardMatch matches s against pattern where '*' spans zero or
// more characters (the spec's `.*` regex equivalence, shared by the
// aud and x-nmos path grammars).
func wildcardMatch(pattern, s string) bool {
	// Iterative glob: split on '*', require ordered substring hits
	// with anchored first/last segments.
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return pattern == s
	}
	if !strings.HasPrefix(s, parts[0]) {
		return false
	}
	s = s[len(parts[0]):]
	for i := 1; i < len(parts)-1; i++ {
		idx := strings.Index(s, parts[i])
		if idx < 0 {
			return false
		}
		s = s[idx+len(parts[i]):]
	}
	return strings.HasSuffix(s, parts[len(parts)-1])
}
