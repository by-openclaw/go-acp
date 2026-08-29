package is10

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Audience is the JWT `aud` claim: the schema allows a single string
// or an array of strings, so decoding accepts both and canonicalises
// to a slice.
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
		return fmt.Errorf("is10: aud must be a string or array of strings")
	}
	*a = Audience(many)
	return nil
}

// MarshalJSON always emits the array form (the RECOMMENDED shape).
func (a Audience) MarshalJSON() ([]byte, error) {
	return json.Marshal([]string(a))
}

// Permissions is the value of one x-nmos-* claim: wildcarded URL path
// specifiers per verb class. An omitted key means the permission was
// not granted (the spec minimises token size this way).
type Permissions struct {
	Read  []string `json:"read,omitempty"`
	Write []string `json:"write,omitempty"`
}

// Claims is the IS-10 access-token claim set (token_schema.json).
// APIs collects the x-nmos-* private claims keyed by API name with
// the "x-nmos-" prefix stripped ("registration", "query", ...).
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
	APIs     map[string]Permissions
}

// xNmosPrefix marks the private-claim namespace.
const xNmosPrefix = "x-nmos-"

// UnmarshalJSON decodes registered claims by name and sweeps every
// x-nmos-* key into APIs. Unknown claims are tolerated — RFC 7519
// tokens routinely carry extras (jti, custom AS claims).
func (c *Claims) UnmarshalJSON(raw []byte) error {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return fmt.Errorf("is10: claims: %w", err)
	}
	get := func(key string, dst any) error {
		v, ok := m[key]
		if !ok {
			return nil
		}
		if err := json.Unmarshal(v, dst); err != nil {
			return fmt.Errorf("is10: claim %s: %w", key, err)
		}
		return nil
	}
	if err := get("iss", &c.Iss); err != nil {
		return err
	}
	if err := get("sub", &c.Sub); err != nil {
		return err
	}
	if err := get("aud", &c.Aud); err != nil {
		return err
	}
	if err := get("exp", &c.Exp); err != nil {
		return err
	}
	if err := get("iat", &c.Iat); err != nil {
		return err
	}
	if err := get("nbf", &c.Nbf); err != nil {
		return err
	}
	if err := get("client_id", &c.ClientID); err != nil {
		return err
	}
	if err := get("azp", &c.Azp); err != nil {
		return err
	}
	if err := get("scope", &c.Scope); err != nil {
		return err
	}
	for k, v := range m {
		if !strings.HasPrefix(k, xNmosPrefix) {
			continue
		}
		var p Permissions
		if err := json.Unmarshal(v, &p); err != nil {
			return fmt.Errorf("is10: claim %s: %w", k, err)
		}
		if c.APIs == nil {
			c.APIs = map[string]Permissions{}
		}
		c.APIs[strings.TrimPrefix(k, xNmosPrefix)] = p
	}
	return nil
}

// HasScope reports whether the space-separated scope claim names api.
func (c Claims) HasScope(api string) bool {
	for _, s := range strings.Fields(c.Scope) {
		if s == api {
			return true
		}
	}
	return false
}

// TokenResponse is the OAuth 2.0 token-endpoint success body
// (token_response.json).
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
	TokenType    string `json:"token_type"`
}

// Validate enforces the schema's required members plus the Bearer
// type (case-insensitive per Behaviour - Token Requests.md).
func (t TokenResponse) Validate() error {
	if t.AccessToken == "" {
		return fmt.Errorf("is10: token response: access_token is required")
	}
	if t.ExpiresIn <= 0 {
		return fmt.Errorf("is10: token response: expires_in is required")
	}
	if !strings.EqualFold(t.TokenType, "bearer") {
		return fmt.Errorf("is10: token response: token_type %q is not Bearer", t.TokenType)
	}
	return nil
}

// TokenError is the token-endpoint error body (token_error_response.json).
type TokenError struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description,omitempty"`
	ErrorURI         string `json:"error_uri,omitempty"`
}

// Metadata is the RFC 8414 Authorization Server metadata document
// (auth_metadata.json).
type Metadata struct {
	Issuer                        string   `json:"issuer"`
	AuthorizationEndpoint         string   `json:"authorization_endpoint"`
	TokenEndpoint                 string   `json:"token_endpoint"`
	JwksURI                       string   `json:"jwks_uri"`
	RegistrationEndpoint          string   `json:"registration_endpoint"`
	ScopesSupported               []string `json:"scopes_supported,omitempty"`
	ResponseTypesSupported        []string `json:"response_types_supported"`
	GrantTypesSupported           []string `json:"grant_types_supported,omitempty"`
	RevocationEndpoint            string   `json:"revocation_endpoint,omitempty"`
	CodeChallengeMethodsSupported []string `json:"code_challenge_methods_supported"`
}

// Validate enforces the schema's required members.
func (m Metadata) Validate() error {
	miss := func(what, v string) error {
		if v == "" {
			return fmt.Errorf("is10: server metadata: %s is required", what)
		}
		return nil
	}
	for _, pair := range [][2]string{
		{"issuer", m.Issuer},
		{"authorization_endpoint", m.AuthorizationEndpoint},
		{"token_endpoint", m.TokenEndpoint},
		{"jwks_uri", m.JwksURI},
		{"registration_endpoint", m.RegistrationEndpoint},
	} {
		if err := miss(pair[0], pair[1]); err != nil {
			return err
		}
	}
	if m.ResponseTypesSupported == nil {
		return fmt.Errorf("is10: server metadata: response_types_supported is required")
	}
	if m.CodeChallengeMethodsSupported == nil {
		return fmt.Errorf("is10: server metadata: code_challenge_methods_supported is required")
	}
	return nil
}

// DecodeMetadata parses + validates a metadata document. Decoding is
// tolerant of extra members — RFC 8414 servers publish many.
func DecodeMetadata(raw []byte) (Metadata, error) {
	var m Metadata
	if err := json.Unmarshal(raw, &m); err != nil {
		return Metadata{}, fmt.Errorf("is10: decode server metadata: %w", err)
	}
	return m, m.Validate()
}

// JWK is one JSON Web Key. The IS-10 schema fixes the envelope; the
// RSA members (n, e) come from RFC 7517 and are what verification
// needs. Decoding is tolerant — keys carry arbitrary extra params.
type JWK struct {
	Kty string   `json:"kty"`
	Use string   `json:"use,omitempty"`
	Alg string   `json:"alg,omitempty"`
	Kid string   `json:"kid,omitempty"`
	N   string   `json:"n,omitempty"`
	E   string   `json:"e,omitempty"`
	X5c []string `json:"x5c,omitempty"`
}

// JWKS is a JSON Web Key Set (jwks_schema.json).
type JWKS struct {
	Keys []JWK `json:"keys"`
}

// DecodeJWKS parses a key set; `keys` must exist.
func DecodeJWKS(raw []byte) (JWKS, error) {
	var s struct {
		Keys *[]JWK `json:"keys"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return JWKS{}, fmt.Errorf("is10: decode jwks: %w", err)
	}
	if s.Keys == nil {
		return JWKS{}, fmt.Errorf("is10: jwks: keys is required")
	}
	return JWKS{Keys: *s.Keys}, nil
}
