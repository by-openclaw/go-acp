// Package v10 is the AMWA NMOS IS-08 v1.0.1 wire codec — the single
// track defined by the spec today. v1.0 covers:
//
//   - HTTP REST endpoints under `/x-nmos/channelmapping/v1.0/...`.
//   - Stage / activate flow on POST /map/activations.
//   - GET /map/active, /map/staged, /map/io.
//
// IS-08 has not received a v1.1 release; this codec is the only
// minor we register. The spec.Registry gracefully extends the day
// AMWA publishes one — drop a `v11/` package and an init Register
// call.
package v10

import (
	"dhs/internal/amwa/codec/is08"
)

// SpecPatch — the patch release the codec is audited against.
const SpecPatch = "v1.0.1"

// Codec implements [is08.Codec] for IS-08 wire minor v1.0.
type Codec struct{}

// New returns a Codec.
func New() Codec { return Codec{} }

func (Codec) SpecID() string    { return is08.SpecID }
func (Codec) APIVer() string    { return "v1.0" }
func (Codec) SpecPatch() string { return SpecPatch }

func (Codec) EncodeMapActive(m is08.MapActive) ([]byte, error) {
	return is08.EncodeMapActive(m)
}
func (Codec) DecodeMapActive(raw []byte) (is08.MapActive, error) {
	return is08.DecodeMapActive(raw)
}
func (Codec) ValidateMapActive(m is08.MapActive) error {
	return is08.ValidateMapActive(m)
}

func (Codec) EncodeMapActivationRequest(r is08.MapActivationRequest) ([]byte, error) {
	return is08.EncodeMapActivationRequest(r)
}
func (Codec) DecodeMapActivationRequest(raw []byte) (is08.MapActivationRequest, error) {
	return is08.DecodeMapActivationRequest(raw)
}
func (Codec) ValidateMapActivationRequest(r is08.MapActivationRequest) error {
	return is08.ValidateMapActivationRequest(r)
}

func (Codec) EncodeIO(io is08.IO) ([]byte, error) {
	return is08.EncodeIO(io)
}
func (Codec) DecodeIO(raw []byte) (is08.IO, error) {
	return is08.DecodeIO(raw)
}
func (Codec) ValidateIO(io is08.IO) error {
	return is08.ValidateIO(io)
}

func init() {
	is08.Register(Codec{})
}
