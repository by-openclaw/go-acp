package is10

import (
	"encoding/json"
	"fmt"
	"strings"
)

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
