// Package v11 is the AMWA NMOS IS-05 v1.1.2 wire codec. v1.1 adds
// bulk endpoints + extended transport_params for ST 2022-7 / mux /
// WebSocket / MQTT / SMPTE 2022-1 over the v1.0 RTP-only baseline;
// the canonical struct in is05/ already covers the union, so the
// v1.1 codec delegates to it directly. Per-transport oneOf branches
// are validated by the controller against the IS-04 sender.transport
// URN — this codec doesn't gate them at the wire-shape level.
package v11

import (
	"acp/internal/amwa/codec/is05"
)

// SpecPatch — the patch release the codec is audited against.
const SpecPatch = "v1.1.2"

// Codec implements [is05.Codec] for IS-05 wire minor v1.1.
type Codec struct{}

// New returns a Codec.
func New() Codec { return Codec{} }

// SpecID / APIVer / SpecPatch — Versioned interface.
func (Codec) SpecID() string    { return is05.SpecID }
func (Codec) APIVer() string    { return "v1.1" }
func (Codec) SpecPatch() string { return SpecPatch }

// EncodeStagedSender / DecodeStagedSender / ValidateStagedSender —
// delegate to the canonical encoders in is05/. Same for receiver.
func (Codec) EncodeStagedSender(s is05.StagedSender) ([]byte, error) {
	return is05.EncodeStagedSender(s)
}
func (Codec) DecodeStagedSender(raw []byte) (is05.StagedSender, error) {
	s, err := is05.DecodeStagedSender(raw)
	if err != nil {
		return is05.StagedSender{}, err
	}
	return *s, nil
}
func (Codec) ValidateStagedSender(s is05.StagedSender) error {
	return is05.ValidateStagedSender(s)
}

func (Codec) EncodeStagedReceiver(r is05.StagedReceiver) ([]byte, error) {
	return is05.EncodeStagedReceiver(r)
}
func (Codec) DecodeStagedReceiver(raw []byte) (is05.StagedReceiver, error) {
	r, err := is05.DecodeStagedReceiver(raw)
	if err != nil {
		return is05.StagedReceiver{}, err
	}
	return *r, nil
}
func (Codec) ValidateStagedReceiver(r is05.StagedReceiver) error {
	return is05.ValidateStagedReceiver(r)
}

func init() {
	is05.Register(Codec{})
}
