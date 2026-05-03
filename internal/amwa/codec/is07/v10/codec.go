// Package v10 is the AMWA NMOS IS-07 v1.0.1 wire codec — the
// single track defined by the spec today. v1.0 covers:
//
//   - WebSocket transport (state, health, reboot, shutdown).
//   - MQTT transport (adds connection_status).
//   - HTTP REST endpoints under `/x-nmos/events/v1.0/...`.
//
// IS-07 has not received a v1.1 release; this codec is the only
// minor we register. The spec.Registry gracefully extends the day
// AMWA publishes one — drop a `v11/` package and an init Register
// call.
package v10

import (
	"dhs/internal/amwa/codec/is07"
)

// SpecPatch — the patch release the codec is audited against.
const SpecPatch = "v1.0.1"

// Codec implements [is07.Codec] for IS-07 wire minor v1.0.
type Codec struct{}

// New returns a Codec.
func New() Codec { return Codec{} }

func (Codec) SpecID() string    { return is07.SpecID }
func (Codec) APIVer() string    { return "v1.0" }
func (Codec) SpecPatch() string { return SpecPatch }

func (Codec) EncodeMessage(m is07.Message) ([]byte, error) {
	return is07.EncodeMessage(m)
}
func (Codec) DecodeMessage(raw []byte) (is07.Message, error) {
	return is07.DecodeMessage(raw)
}
func (Codec) EncodeCommand(c is07.Command) ([]byte, error) {
	return is07.EncodeCommand(c)
}
func (Codec) DecodeCommand(raw []byte) (is07.Command, error) {
	return is07.DecodeCommand(raw)
}

func init() {
	is07.Register(Codec{})
}
