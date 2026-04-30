// Package v12 is the AMWA NMOS IS-04 v1.2.2 wire codec — one of three
// minors required by `internal/amwa/CLAUDE.md` "Versioning". It is a
// thin Strategy implementation of [is04.Codec] that wraps the
// canonical struct types in is04/ and post-processes JSON encode /
// decode to gate fields per the v1.2.2 spec text.
//
// Schema diffs vs v1.3.3 (computed from the AMWA schema bundle in
// internal/amwa/codec/is04/testdata/schemas/):
//
//   - Node:     identical to v1.3.3
//   - Device:   identical to v1.3.3
//   - Source:   identical top-level (polymorphic oneOf on format)
//   - Flow:     identical top-level (polymorphic oneOf on format)
//   - Sender:   identical to v1.3.3
//   - Receiver: identical top-level (polymorphic oneOf on format)
//
// The v1.3 → v1.2 wire is therefore an additive-fields-empty case for
// every top-level resource. The codec exists today as the locked
// Strategy slot so peers advertising `api_ver=v1.2` route through it
// rather than v1.3; future schema deltas (e.g. format-specific oneOf
// branches that DO differ between v1.2 and v1.3) get layered in here
// without touching the plugin.
package v12

import (
	"acp/internal/amwa/codec/is04"
)

// SpecPatch is the spec-text revision this codec strictly complies
// with — the latest patch within the v1.2 minor.
const SpecPatch = "v1.2.2"

// Codec implements [is04.Codec] for IS-04 wire minor v1.2.
//
// Stateless and safe for concurrent use.
type Codec struct{}

// New returns a Codec — equivalent to a zero-value Codec{}.
func New() Codec { return Codec{} }

// SpecID returns the AMWA NMOS catalogue slug for IS-04.
func (Codec) SpecID() string { return is04.SpecID }

// APIVer returns the wire URL minor — "v1.2".
func (Codec) APIVer() string { return "v1.2" }

// SpecPatch returns the latest patch release — "v1.2.2".
func (Codec) SpecPatch() string { return SpecPatch }

// EncodeNode marshals a Node for v1.2.2. Top-level shape matches
// v1.3.3, so we delegate to the canonical encoder.
func (Codec) EncodeNode(n is04.Node) ([]byte, error) {
	return n.Encode()
}

// DecodeNode parses a v1.2.2 Node payload.
func (Codec) DecodeNode(raw []byte) (is04.Node, error) {
	n, err := is04.DecodeNode(raw)
	if err != nil {
		return is04.Node{}, err
	}
	return *n, nil
}

// ValidateNode applies the v1.2.2 required-field set.
func (Codec) ValidateNode(n is04.Node) error {
	return n.Validate()
}

// EncodeDevice / DecodeDevice / ValidateDevice — identical to v1.3.3
// for top-level shape.
func (Codec) EncodeDevice(d is04.Device) ([]byte, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	return marshalIndent(d)
}

// DecodeDevice parses a v1.2.2 Device payload.
func (Codec) DecodeDevice(raw []byte) (is04.Device, error) {
	d, err := is04.DecodeDevice(raw)
	if err != nil {
		return is04.Device{}, err
	}
	return *d, nil
}

// ValidateDevice applies the v1.2.2 required-field set.
func (Codec) ValidateDevice(d is04.Device) error {
	return d.Validate()
}

// EncodeSource marshals a Source for v1.2.2.
func (Codec) EncodeSource(s is04.Source) ([]byte, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return marshalIndent(s)
}

// DecodeSource parses a v1.2.2 Source payload.
func (Codec) DecodeSource(raw []byte) (is04.Source, error) {
	s, err := is04.DecodeSource(raw)
	if err != nil {
		return is04.Source{}, err
	}
	return *s, nil
}

// ValidateSource applies the v1.2.2 required-field set.
func (Codec) ValidateSource(s is04.Source) error {
	return s.Validate()
}

// EncodeFlow marshals a Flow for v1.2.2.
func (Codec) EncodeFlow(f is04.Flow) ([]byte, error) {
	if err := f.Validate(); err != nil {
		return nil, err
	}
	return marshalIndent(f)
}

// DecodeFlow parses a v1.2.2 Flow payload.
func (Codec) DecodeFlow(raw []byte) (is04.Flow, error) {
	f, err := is04.DecodeFlow(raw)
	if err != nil {
		return is04.Flow{}, err
	}
	return *f, nil
}

// ValidateFlow applies the v1.2.2 required-field set.
func (Codec) ValidateFlow(f is04.Flow) error {
	return f.Validate()
}

// EncodeSender marshals a Sender for v1.2.2.
func (Codec) EncodeSender(s is04.Sender) ([]byte, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return marshalIndent(s)
}

// DecodeSender parses a v1.2.2 Sender payload.
func (Codec) DecodeSender(raw []byte) (is04.Sender, error) {
	s, err := is04.DecodeSender(raw)
	if err != nil {
		return is04.Sender{}, err
	}
	return *s, nil
}

// ValidateSender applies the v1.2.2 required-field set.
func (Codec) ValidateSender(s is04.Sender) error {
	return s.Validate()
}

// EncodeReceiver marshals a Receiver for v1.2.2.
func (Codec) EncodeReceiver(r is04.Receiver) ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return marshalIndent(r)
}

// DecodeReceiver parses a v1.2.2 Receiver payload.
func (Codec) DecodeReceiver(raw []byte) (is04.Receiver, error) {
	r, err := is04.DecodeReceiver(raw)
	if err != nil {
		return is04.Receiver{}, err
	}
	return *r, nil
}

// ValidateReceiver applies the v1.2.2 required-field set.
func (Codec) ValidateReceiver(r is04.Receiver) error {
	return r.Validate()
}

func init() {
	is04.Register(Codec{})
}
