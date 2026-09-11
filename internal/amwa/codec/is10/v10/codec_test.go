package v10

import (
	"testing"

	"dhs/internal/amwa/codec/is10"
)

func TestIdentity(t *testing.T) {
	c := New()
	if c.SpecID() != is10.SpecID || c.APIVer() != "v1.0" || c.SpecPatch() != SpecPatch {
		t.Fatalf("identity = %s/%s/%s", c.SpecID(), c.APIVer(), c.SpecPatch())
	}
	got, ok := is10.Get("v1.0")
	if !ok || got != any(c) {
		t.Fatal("v1.0 codec not registered under its own identity")
	}
}

// v1.0 has no minor-specific shape: the codec delegates to the
// canonical decoder, so it must accept exactly what is10 accepts and
// reject exactly what is10 rejects.
func TestDecodeMetadataDelegates(t *testing.T) {
	const meta = `{"issuer":"https://a","authorization_endpoint":"https://a/authorize",` +
		`"token_endpoint":"https://a/token","jwks_uri":"https://a/jwks",` +
		`"registration_endpoint":"https://a/register","response_types_supported":["code"],` +
		`"code_challenge_methods_supported":["S256"]}`
	m, err := New().DecodeMetadata([]byte(meta))
	if err != nil {
		t.Fatalf("DecodeMetadata: %v", err)
	}
	if m.Issuer != "https://a" || m.TokenEndpoint != "https://a/token" {
		t.Fatalf("metadata lost: %+v", m)
	}
	if _, err := New().DecodeMetadata([]byte(`{"issuer":"https://a"}`)); err == nil {
		t.Fatal("incomplete metadata must be rejected")
	}
}
