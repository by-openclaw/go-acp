// Package v13 is the AMWA NMOS IS-04 v1.3.3 wire codec — one of three
// minors required by `internal/amwa/CLAUDE.md` "Versioning". It is a
// thin Strategy implementation of [is04.Codec] that delegates to the
// canonical struct methods + decoders in the parent is04 package; the
// canonical struct field set already covers v1.3.3, so the v1.3
// strategy adds no field gating.
//
// Sibling packages v11/ + v12/ filter the canonical struct down to
// the older minors' field sets. They never import this package — all
// three minors are siblings, sharing only the canonical types in
// is04/.
//
// Lift-to-own-repo rule (per ADR-0006, codec stdlib-only): stdlib +
// is04/ + spec/ only.
package v13

import (
	"dhs/internal/amwa/codec/is04"
	"encoding/json"
)

// SpecPatch is the spec-text revision this codec strictly complies
// with. Wire path is /x-nmos/<api>/v1.3/, but the implementation is
// audited against the v1.3.3 patch — exposed via SpecPatch() so
// conformance reports stay attributable.
const SpecPatch = "v1.3.3"

// Codec implements [is04.Codec] for IS-04 wire minor v1.3.
//
// Stateless and safe for concurrent use. Hold the zero value or
// instantiate with [New]; both are equivalent.
type Codec struct{}

// New returns a Codec — equivalent to a zero-value Codec{}; provided
// for symmetry with other constructors in the codebase.
func New() Codec { return Codec{} }

// SpecID returns the AMWA NMOS catalogue slug for IS-04.
func (Codec) SpecID() string { return is04.SpecID }

// APIVer returns the wire URL minor — "v1.3".
func (Codec) APIVer() string { return "v1.3" }

// SpecPatch returns the latest patch release the codec is audited
// against — "v1.3.3".
func (Codec) SpecPatch() string { return SpecPatch }

// EncodeNode marshals a Node for IS-04 v1.3.3. Validates first; never
// emits a non-compliant payload.
func (Codec) EncodeNode(n is04.Node) ([]byte, error) {
	return n.Encode()
}

// DecodeNode parses a v1.3.3 Node payload. Unknown fields are rejected
// (DisallowUnknownFields) — peers sending fields that aren't in the
// v1.3 schema will be flagged as compliance deviations by the caller.
func (Codec) DecodeNode(raw []byte) (is04.Node, error) {
	n, err := is04.DecodeNode(raw)
	if err != nil {
		return is04.Node{}, err
	}
	return *n, nil
}

// ValidateNode applies the v1.3.3 required-field set + UUID / version /
// MAC patterns to a canonical Node struct. The canonical struct
// already covers every v1.3.3 field, so this is a direct delegate.
func (Codec) ValidateNode(n is04.Node) error {
	return n.Validate()
}

// EncodeDevice marshals a Device for v1.3.3. Validates first.
func (Codec) EncodeDevice(d is04.Device) ([]byte, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	return json.MarshalIndent(d, "", "  ")
}

// DecodeDevice parses a v1.3.3 Device payload.
func (Codec) DecodeDevice(raw []byte) (is04.Device, error) {
	d, err := is04.DecodeDevice(raw)
	if err != nil {
		return is04.Device{}, err
	}
	return *d, nil
}

// ValidateDevice applies the v1.3.3 required-field set.
func (Codec) ValidateDevice(d is04.Device) error {
	return d.Validate()
}

// EncodeSource marshals a Source for v1.3.3.
func (Codec) EncodeSource(s is04.Source) ([]byte, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return json.MarshalIndent(s, "", "  ")
}

// DecodeSource parses a v1.3.3 Source payload.
func (Codec) DecodeSource(raw []byte) (is04.Source, error) {
	s, err := is04.DecodeSource(raw)
	if err != nil {
		return is04.Source{}, err
	}
	return *s, nil
}

// ValidateSource applies the v1.3.3 required-field set.
func (Codec) ValidateSource(s is04.Source) error {
	return s.Validate()
}

// EncodeFlow marshals a Flow for v1.3.3.
func (Codec) EncodeFlow(f is04.Flow) ([]byte, error) {
	if err := f.Validate(); err != nil {
		return nil, err
	}
	return json.MarshalIndent(f, "", "  ")
}

// DecodeFlow parses a v1.3.3 Flow payload.
func (Codec) DecodeFlow(raw []byte) (is04.Flow, error) {
	f, err := is04.DecodeFlow(raw)
	if err != nil {
		return is04.Flow{}, err
	}
	return *f, nil
}

// ValidateFlow applies the v1.3.3 required-field set + format-specific
// rules.
func (Codec) ValidateFlow(f is04.Flow) error {
	return f.Validate()
}

// EncodeSender marshals a Sender for v1.3.3.
func (Codec) EncodeSender(s is04.Sender) ([]byte, error) {
	if err := is04.GateTransport("sender", s.Transport, "v1.3"); err != nil {
		return nil, err
	}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return json.MarshalIndent(s, "", "  ")
}

// DecodeSender parses a v1.3.3 Sender payload.
func (Codec) DecodeSender(raw []byte) (is04.Sender, error) {
	s, err := is04.DecodeSender(raw)
	if err != nil {
		return is04.Sender{}, err
	}
	return *s, nil
}

// ValidateSender applies the v1.3.3 required-field set + transport
// URN enum.
func (Codec) ValidateSender(s is04.Sender) error {
	return s.Validate()
}

// EncodeReceiver marshals a Receiver for v1.3.3.
func (Codec) EncodeReceiver(r is04.Receiver) ([]byte, error) {
	if err := is04.GateTransport("receiver", r.Transport, "v1.3"); err != nil {
		return nil, err
	}
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return json.MarshalIndent(r, "", "  ")
}

// DecodeReceiver parses a v1.3.3 Receiver payload.
func (Codec) DecodeReceiver(raw []byte) (is04.Receiver, error) {
	r, err := is04.DecodeReceiver(raw)
	if err != nil {
		return is04.Receiver{}, err
	}
	return *r, nil
}

// ValidateReceiver applies the v1.3.3 required-field set.
func (Codec) ValidateReceiver(r is04.Receiver) error {
	return r.Validate()
}

func init() {
	is04.Register(Codec{})
}
