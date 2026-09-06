package auth

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
)

// JWK is one JSON Web Key (RFC 7517). Only the RSA members are decoded,
// because those are what verification needs; keys carry arbitrary extra
// parameters and decoding stays tolerant of them.
type JWK struct {
	Kty string   `json:"kty"`
	Use string   `json:"use,omitempty"`
	Alg string   `json:"alg,omitempty"`
	Kid string   `json:"kid,omitempty"`
	N   string   `json:"n,omitempty"`
	E   string   `json:"e,omitempty"`
	X5c []string `json:"x5c,omitempty"`
}

// JWKS is a JSON Web Key Set.
type JWKS struct {
	Keys []JWK `json:"keys"`
}

// DecodeJWKS parses a key set. `keys` must be present: a body without it is
// not an empty key set, it is a different document — often an HTML error page
// or an OAuth error object — and treating that as "zero keys" turns a
// misconfigured endpoint into a silent authorization failure.
func DecodeJWKS(raw []byte) (JWKS, error) {
	var s struct {
		Keys *[]JWK `json:"keys"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return JWKS{}, fmt.Errorf("auth: decode jwks: %w", err)
	}
	if s.Keys == nil {
		return JWKS{}, fmt.Errorf("auth: jwks: keys is required")
	}
	return JWKS{Keys: *s.Keys}, nil
}

// PublicKey converts an RSA JWK to the stdlib form.
func (j JWK) PublicKey() (*rsa.PublicKey, error) {
	if !strings.EqualFold(j.Kty, "RSA") {
		return nil, fmt.Errorf("auth: jwk kty %q: not an RSA key", j.Kty)
	}
	if j.N == "" || j.E == "" {
		return nil, fmt.Errorf("auth: jwk %q: missing n/e members", j.Kid)
	}
	nb, err := base64.RawURLEncoding.DecodeString(j.N)
	if err != nil {
		return nil, fmt.Errorf("auth: jwk %q n: %w", j.Kid, err)
	}
	eb, err := base64.RawURLEncoding.DecodeString(j.E)
	if err != nil {
		return nil, fmt.Errorf("auth: jwk %q e: %w", j.Kid, err)
	}
	e := new(big.Int).SetBytes(eb)
	if !e.IsInt64() || e.Int64() <= 0 {
		return nil, fmt.Errorf("auth: jwk %q: invalid exponent", j.Kid)
	}
	return &rsa.PublicKey{N: new(big.Int).SetBytes(nb), E: int(e.Int64())}, nil
}

// hashFor maps a permitted alg to its digest. Returning an error rather than
// defaulting matters: a default would silently verify RS512 tokens with a
// SHA-256 digest and reject every one of them for the wrong reason.
func hashFor(alg string) (crypto.Hash, func([]byte) []byte, error) {
	switch alg {
	case AlgRS256:
		return crypto.SHA256, func(b []byte) []byte { s := sha256.Sum256(b); return s[:] }, nil
	case AlgRS384:
		return crypto.SHA384, func(b []byte) []byte { s := sha512.Sum384(b); return s[:] }, nil
	case AlgRS512:
		return crypto.SHA512, func(b []byte) []byte { s := sha512.Sum512(b); return s[:] }, nil
	default:
		return 0, nil, fmt.Errorf("auth: no verifier for alg %q", alg)
	}
}

// VerifyWithKeys parses raw and verifies its signature against the key set.
//
// The kid-matching key is tried first, then every other RSA key until one
// verifies or the set is exhausted. Trying the rest is not sloppiness: an
// authorization server that rotates keys may sign with a new one before its
// published kid catches up, and RFC 7515 makes kid a hint rather than a
// guarantee. The algorithm is still pinned, so the fallback widens which KEY
// may sign, never how.
//
// Claims are NOT validated here — call ValidateTime, and let the protocol
// apply its own issuer and audience rules.
func VerifyWithKeys(raw string, keys []JWK, permitted ...string) (Claims, error) {
	h, c, input, sig, err := ParseToken(raw, permitted...)
	if err != nil {
		return Claims{}, err
	}
	hash, sum, err := hashFor(h.Alg)
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
		pub, perr := k.PublicKey()
		if perr != nil {
			lastErr = perr
			continue
		}
		if verr := rsa.VerifyPKCS1v15(pub, hash, sum([]byte(input)), sig); verr == nil {
			return c, nil
		} else {
			lastErr = verr
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no keys available")
	}
	return Claims{}, fmt.Errorf("auth: token signature verification failed: %w", lastErr)
}
