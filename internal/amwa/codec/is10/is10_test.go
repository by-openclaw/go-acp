package is10_test

// Tests for the IS-10 wire documents. Expected behaviour comes from the
// AMWA v1.0.0 schemas + the Behaviour documents (Token Requests), not
// from working code.
//
// The token machinery these tests used to cover moved out: the RFC
// 7515/7517/7519 half to internal/auth, the NMOS claim policy to
// internal/amwa/session/auth. Their tests moved with them.

import (
	"encoding/json"
	"testing"

	"dhs/internal/amwa/codec/is10"
	v10 "dhs/internal/amwa/codec/is10/v10"
)

func TestDecodeMetadata(t *testing.T) {
	const meta = `{"issuer":"https://auth.example.com","authorization_endpoint":"https://a/authorize",` +
		`"token_endpoint":"https://a/token","jwks_uri":"https://a/jwks",` +
		`"registration_endpoint":"https://a/register","response_types_supported":["code"],` +
		`"code_challenge_methods_supported":["S256","plain"],"extra":"tolerated"}`
	m, err := is10.DecodeMetadata([]byte(meta))
	if err != nil {
		t.Fatalf("metadata: %v", err)
	}
	if m.JwksURI != "https://a/jwks" {
		t.Errorf("metadata lost: %+v", m)
	}
	if m.Issuer != "https://auth.example.com" {
		t.Errorf("issuer lost: %+v", m)
	}
}

// Every required member of auth_metadata.json gets its own case: a
// missing endpoint that decoded to "" would send the client to an empty
// URL, which fails far from the cause.
func TestMetadataRequiredMembers(t *testing.T) {
	full := is10.Metadata{
		Issuer:                        "https://a",
		AuthorizationEndpoint:         "https://a/authorize",
		TokenEndpoint:                 "https://a/token",
		JwksURI:                       "https://a/jwks",
		RegistrationEndpoint:          "https://a/register",
		ResponseTypesSupported:        []string{"code"},
		CodeChallengeMethodsSupported: []string{"S256"},
	}
	if err := full.Validate(); err != nil {
		t.Fatalf("complete metadata rejected: %v", err)
	}
	for _, tc := range []struct {
		name string
		drop func(*is10.Metadata)
	}{
		{"issuer", func(m *is10.Metadata) { m.Issuer = "" }},
		{"authorization_endpoint", func(m *is10.Metadata) { m.AuthorizationEndpoint = "" }},
		{"token_endpoint", func(m *is10.Metadata) { m.TokenEndpoint = "" }},
		{"jwks_uri", func(m *is10.Metadata) { m.JwksURI = "" }},
		{"registration_endpoint", func(m *is10.Metadata) { m.RegistrationEndpoint = "" }},
		{"response_types_supported", func(m *is10.Metadata) { m.ResponseTypesSupported = nil }},
		{"code_challenge_methods_supported", func(m *is10.Metadata) { m.CodeChallengeMethodsSupported = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := full
			tc.drop(&m)
			if err := m.Validate(); err == nil {
				t.Errorf("metadata without %s must be rejected", tc.name)
			}
		})
	}
	if _, err := is10.DecodeMetadata([]byte(`{"issuer":`)); err == nil {
		t.Error("malformed JSON must be rejected")
	}
}

func TestTokenResponse(t *testing.T) {
	tr := is10.TokenResponse{AccessToken: "t", ExpiresIn: 60, TokenType: "bearer"}
	if err := tr.Validate(); err != nil {
		t.Errorf("Bearer must be case-insensitive: %v", err)
	}
	for _, tc := range []struct {
		name string
		tr   is10.TokenResponse
	}{
		{"no access_token", is10.TokenResponse{ExpiresIn: 60, TokenType: "Bearer"}},
		{"no expires_in", is10.TokenResponse{AccessToken: "t", TokenType: "Bearer"}},
		{"negative expires_in", is10.TokenResponse{AccessToken: "t", ExpiresIn: -1, TokenType: "Bearer"}},
		{"non-Bearer type", is10.TokenResponse{AccessToken: "t", ExpiresIn: 60, TokenType: "mac"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.tr.Validate(); err == nil {
				t.Errorf("%s must be rejected", tc.name)
			}
		})
	}
}

func TestTokenErrorDecodes(t *testing.T) {
	var te is10.TokenError
	if err := json.Unmarshal([]byte(`{"error":"invalid_client","error_description":"bad secret"}`), &te); err != nil {
		t.Fatalf("token error: %v", err)
	}
	if te.Error != "invalid_client" || te.ErrorDescription != "bad secret" {
		t.Errorf("token error lost: %+v", te)
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
	if is10.Default().APIVer() != "v1.0" {
		t.Errorf("default = %s", is10.Default().APIVer())
	}
	// The codec is metadata-only now; verification is not a codec concern.
	if _, err := c.DecodeMetadata([]byte(`{}`)); err == nil {
		t.Error("codec must validate the metadata it decodes")
	}
}

// The registry helpers the plugin layer selects versions through.
func TestVersionSelection(t *testing.T) {
	if got := is10.AllCodecs(); len(got) != 1 {
		t.Fatalf("AllCodecs = %d codecs, want 1", len(got))
	}
	c, err := is10.SelectHighest([]string{"v0.9", "v1.0"})
	if err != nil {
		t.Fatalf("SelectHighest: %v", err)
	}
	if c.APIVer() != "v1.0" {
		t.Errorf("selected %s", c.APIVer())
	}
	// No intersection is a typed error, never a silent downgrade.
	if _, err := is10.SelectHighest([]string{"v2.0"}); err == nil {
		t.Error("a peer with no common version must be an error")
	}
}

// Registering a codec that claims another spec is an init-time bug, and
// a panic is the only place it can be caught before the wrong codec
// starts answering IS-10 traffic.
func TestRegisterRejectsForeignSpecID(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("Register must panic on a foreign SpecID")
		}
	}()
	is10.Register(foreignCodec{})
}

type foreignCodec struct{}

func (foreignCodec) SpecID() string                               { return "is-04" }
func (foreignCodec) APIVer() string                               { return "v1.0" }
func (foreignCodec) SpecPatch() string                            { return "v1.0.0" }
func (foreignCodec) DecodeMetadata([]byte) (is10.Metadata, error) { return is10.Metadata{}, nil }
