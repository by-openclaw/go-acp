// Package v12 is the AMWA NMOS IS-05 v1.2.0 wire codec.
//
// v1.2's substantive change is not a new field: it moves the transport
// catalogue OUT of the spec text. Per the v1.2.0 Upgrade Path,
// "from v1.2 onwards, additional transport types and associated
// schemas are defined in the Transports register of the NMOS Parameter
// Registers". A transport can therefore be added without any spec
// document being revised — which is precisely how ndi and usb reached
// production while our table still listed six transports and the
// validator rejected both.
//
// The wire SHAPE is unchanged from v1.1, so the canonical struct in
// is05/ still covers it and this codec delegates. What v1.2 adds is
// permission: NDI and USB senders/receivers may appear on a v1.2 tree
// and MUST NOT appear on v1.0 or v1.1 (Upgrade Path — "earlier API
// versions MUST NOT list any Senders or Receivers which make use of
// this new transport type"). That gate lives in
// is04.IsNMOSTransportAt, keyed by IS-05 minor.
package v12

import (
	"dhs/internal/amwa/codec/is05"
)

// SpecPatch — the patch release the codec is audited against.
const SpecPatch = "v1.2.0"

// Codec implements [is05.Codec] for IS-05 wire minor v1.2.
type Codec struct{}

// New returns a Codec.
func New() Codec { return Codec{} }

// SpecID / APIVer / SpecPatch — Versioned interface.
func (Codec) SpecID() string    { return is05.SpecID }
func (Codec) APIVer() string    { return "v1.2" }
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
