// Package v10 is the AMWA NMOS IS-05 v1.0.2 wire codec — the
// RTP-only baseline. v1.0 omits:
//
//   - Bulk endpoints (added in v1.1)
//   - WebSocket / MQTT / MUX / SMPTE 2022-1 transports (added in v1.1)
//   - Some transport_params keys introduced in v1.1
//
// The wire-shape of staged Sender / Receiver is identical between
// v1.0 and v1.1 at the top level; transport-params validation is
// done by the controller against the IS-04 sender.transport URN —
// v1.0 only accepts RTP variants.
package v10

import (
	"acp/internal/amwa/codec/is05"
)

// SpecPatch — the patch release the codec is audited against.
const SpecPatch = "v1.0.2"

// Codec implements [is05.Codec] for IS-05 wire minor v1.0.
type Codec struct{}

// New returns a Codec.
func New() Codec { return Codec{} }

func (Codec) SpecID() string    { return is05.SpecID }
func (Codec) APIVer() string    { return "v1.0" }
func (Codec) SpecPatch() string { return SpecPatch }

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
