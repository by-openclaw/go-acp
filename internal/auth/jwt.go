package auth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Header is the JOSE header of a compact JWS.
type Header struct {
	Typ string `json:"typ,omitempty"`
	Alg string `json:"alg"`
	Kid string `json:"kid,omitempty"`
}

// The JWS algorithms this package verifies. All three are RSASSA-PKCS1-v1_5
// over the matching SHA-2 hash, which crypto/rsa provides directly.
//
// Deliberately not a long list: an algorithm nobody's peer offers is untested
// code in a security path. Add one when a peer needs it, with a test.
const (
	AlgRS256 = "RS256"
	AlgRS384 = "RS384"
	AlgRS512 = "RS512"
)

// Audience is the `aud` claim. RFC 7519 allows a bare string or an array, so
// decoding accepts both and canonicalises to a slice.
type Audience []string

// UnmarshalJSON accepts both wire forms.
func (a *Audience) UnmarshalJSON(raw []byte) error {
	var one string
	if err := json.Unmarshal(raw, &one); err == nil {
		*a = Audience{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(raw, &many); err != nil {
		return fmt.Errorf("auth: aud must be a string or array of strings")
	}
	*a = Audience(many)
	return nil
}

// MarshalJSON always emits the array form.
func (a Audience) MarshalJSON() ([]byte, error) { return json.Marshal([]string(a)) }

// Claims is the registered RFC 7519 claim set plus the two OAuth2 claims that
// travel with practically every access token.
//
// Private carries every OTHER claim, undecoded. That is the seam that keeps
// this package protocol-free: NMOS sweeps "x-nmos-*" out of it, CCM will
// sweep whatever its issuer sends, and neither needs a field here.
type Claims struct {
	Iss      string
	Sub      string
	Aud      Audience
	Exp      *float64
	Iat      *float64
	Nbf      *float64
	ClientID string
	Azp      string
	Scope    string

	Private map[string]json.RawMessage
}

// UnmarshalJSON decodes the registered claims by name and keeps the rest.
// Unknown claims are tolerated, never an error — real tokens routinely carry
// jti and issuer-specific extras, and rejecting them would fail every token
// from a conformant authorization server.
func (c *Claims) UnmarshalJSON(raw []byte) error {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return fmt.Errorf("auth: claims: %w", err)
	}
	get := func(key string, dst any) error {
		v, ok := m[key]
		if !ok {
			return nil
		}
		if err := json.Unmarshal(v, dst); err != nil {
			return fmt.Errorf("auth: claim %s: %w", key, err)
		}
		delete(m, key)
		return nil
	}
	for key, dst := range map[string]any{
		"iss": &c.Iss, "sub": &c.Sub, "aud": &c.Aud,
		"exp": &c.Exp, "iat": &c.Iat, "nbf": &c.Nbf,
		"client_id": &c.ClientID, "azp": &c.Azp, "scope": &c.Scope,
	} {
		if err := get(key, dst); err != nil {
			return err
		}
	}
	if len(m) > 0 {
		c.Private = m
	}
	return nil
}

// EffectiveClientID is client_id, or azp when client_id is absent. Issuers
// differ on which they set and OAuth2 treats them as the same identity.
func (c *Claims) EffectiveClientID() string {
	if c.ClientID != "" {
		return c.ClientID
	}
	return c.Azp
}

// ParseToken splits a compact JWS and decodes its header and claims. It does
// NOT verify the signature — that is VerifyWithKeys — but it DOES pin the
// algorithm, and it pins it here, before anything else looks at the token.
//
// permitted lists the acceptable `alg` values and must not be empty. Trusting
// the header's own word for the algorithm is the classic JWS downgrade: a
// token claiming alg=none, or claiming HMAC so the "verifier" checks it with
// the public key as the secret. Refusing to parse an unlisted alg makes both
// unreachable rather than merely unlikely.
func ParseToken(raw string, permitted ...string) (h Header, c Claims, signingInput string, sig []byte, err error) {
	if len(permitted) == 0 {
		return Header{}, Claims{}, "", nil, fmt.Errorf("auth: no permitted algorithms given — refusing to parse")
	}
	parts := strings.Split(strings.TrimSpace(raw), ".")
	if len(parts) != 3 {
		return Header{}, Claims{}, "", nil,
			fmt.Errorf("auth: token is not a compact JWS (want 3 segments, got %d)", len(parts))
	}
	dec := func(seg, what string) ([]byte, error) {
		b, derr := base64.RawURLEncoding.DecodeString(seg)
		if derr != nil {
			return nil, fmt.Errorf("auth: token %s: %w", what, derr)
		}
		return b, nil
	}

	hRaw, err := dec(parts[0], "header")
	if err != nil {
		return Header{}, Claims{}, "", nil, err
	}
	if err = json.Unmarshal(hRaw, &h); err != nil {
		return Header{}, Claims{}, "", nil, fmt.Errorf("auth: token header: %w", err)
	}
	if !permits(permitted, h.Alg) {
		return Header{}, Claims{}, "", nil,
			fmt.Errorf("auth: token alg %q not permitted (want one of %v)", h.Alg, permitted)
	}

	cRaw, err := dec(parts[1], "payload")
	if err != nil {
		return Header{}, Claims{}, "", nil, err
	}
	if err = json.Unmarshal(cRaw, &c); err != nil {
		return Header{}, Claims{}, "", nil, fmt.Errorf("auth: token payload: %w", err)
	}
	if sig, err = dec(parts[2], "signature"); err != nil {
		return Header{}, Claims{}, "", nil, err
	}
	return h, c, parts[0] + "." + parts[1], sig, nil
}

func permits(permitted []string, alg string) bool {
	for _, p := range permitted {
		if p == alg {
			return true
		}
	}
	return false
}

// ValidateTime enforces the clock claims: exp is required and must not have
// passed; iat and nbf, when present, must not be in the future.
//
// leeway absorbs clock skew in BOTH directions. Without it a device whose NTP
// is a second off rejects every token it is given, which presents as an
// authorization bug and is not one.
//
// It deliberately does not check iss, sub or aud. Which issuers and audiences
// are acceptable is protocol policy, and a package that guessed would be
// wrong for the next protocol.
func ValidateTime(c Claims, now time.Time, leeway time.Duration) error {
	if c.Exp == nil {
		return fmt.Errorf("auth: token: exp claim is required")
	}
	unix := float64(now.Unix())
	if *c.Exp < unix-leeway.Seconds() {
		return fmt.Errorf("auth: token expired at %v", time.Unix(int64(*c.Exp), 0).UTC())
	}
	if c.Iat != nil && *c.Iat > unix+leeway.Seconds() {
		return fmt.Errorf("auth: token iat is in the future")
	}
	if c.Nbf != nil && *c.Nbf > unix+leeway.Seconds() {
		return fmt.Errorf("auth: token not valid before %v", time.Unix(int64(*c.Nbf), 0).UTC())
	}
	return nil
}
