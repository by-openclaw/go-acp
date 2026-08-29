// Package v10 is the AMWA NMOS IS-10 v1.0.0 wire codec — the only
// published minor. The canonical machinery in is10/ carries v1.0's
// whole surface, so this package is identity plus the registry entry,
// per the locked pattern.
package v10

import (
	"dhs/internal/amwa/codec/is10"
)

// SpecPatch — the patch release the codec is audited against.
const SpecPatch = "v1.0.0"

// Codec implements [is10.Codec] for IS-10 wire minor v1.0.
type Codec struct{}

// New returns a Codec.
func New() Codec { return Codec{} }

func (Codec) SpecID() string    { return is10.SpecID }
func (Codec) APIVer() string    { return "v1.0" }
func (Codec) SpecPatch() string { return SpecPatch }

func (Codec) DecodeMetadata(raw []byte) (is10.Metadata, error) { return is10.DecodeMetadata(raw) }
func (Codec) DecodeJWKS(raw []byte) (is10.JWKS, error)         { return is10.DecodeJWKS(raw) }
func (Codec) VerifyWithKeys(tok string, keys []is10.JWK) (is10.Claims, error) {
	return is10.VerifyWithKeys(tok, keys)
}

func init() {
	is10.Register(Codec{})
}
