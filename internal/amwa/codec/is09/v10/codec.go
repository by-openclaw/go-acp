// Package v10 is the AMWA NMOS IS-09 v1.0.0 wire codec. IS-09 has only
// one stable minor today, so this is the sole codec registered with
// is09 — adding a future minor lands as a sibling vXX/ package.
//
// The IS-09 v1.0.0 schema predates IS-10 (Authorization), so it
// deliberately omits the `api_auth` TXT key from DNS-SD records and
// the `auth` field from the Global resource. The codec ships exactly
// what the spec text mandates — see `internal/amwa/codec/is09/global.go`
// + `validate.go` in the parent package.
package v10

import (
	"acp/internal/amwa/codec/is09"
)

// SpecPatch is the spec-text revision this codec strictly complies
// with — IS-09 has only one release.
const SpecPatch = "v1.0.0"

// Codec implements [is09.Codec] for IS-09 wire minor v1.0.
//
// Stateless and safe for concurrent use.
type Codec struct{}

// New returns a Codec — equivalent to a zero-value Codec{}.
func New() Codec { return Codec{} }

// SpecID returns the AMWA NMOS catalogue slug for IS-09.
func (Codec) SpecID() string { return is09.SpecID }

// APIVer returns the wire URL minor — "v1.0".
func (Codec) APIVer() string { return "v1.0" }

// SpecPatch returns the spec patch — "v1.0.0".
func (Codec) SpecPatch() string { return SpecPatch }

// EncodeGlobal marshals a Global config for v1.0.0. Validates first.
func (Codec) EncodeGlobal(g is09.Global) ([]byte, error) {
	return g.Encode()
}

// DecodeGlobal parses a v1.0.0 Global config payload. Unknown fields
// are rejected by the underlying decoder.
func (Codec) DecodeGlobal(raw []byte) (is09.Global, error) {
	g, err := is09.Decode(raw)
	if err != nil {
		return is09.Global{}, err
	}
	return *g, nil
}

// ValidateGlobal applies the v1.0.0 required-field set + range bounds
// (heartbeat 1-1000, announce 2-10, ptp domain 0-127, syslog port
// 1-65535) + syslog hostname/IPv4/IPv6 format checks.
func (Codec) ValidateGlobal(g is09.Global) error {
	return g.Validate()
}

func init() {
	is09.Register(Codec{})
}
