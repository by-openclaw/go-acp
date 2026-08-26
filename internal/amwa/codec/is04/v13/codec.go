// Package v13 is the AMWA NMOS IS-04 v1.3.3 wire codec — one of the four
// minors required by `internal/amwa/CLAUDE.md` "Versioning". It is a
// Strategy implementation of [is04.Codec] over the canonical structs
// in is04/, and it holds ONLY what is specific to v1.3: this package's
// identity, and the v1.3.3 required-field validators.
//
// It holds no field-gating tables of its own. Which property arrived
// at which minor is stated once, as data, in [is04.Since]; both
// directions read it:
//
//	Encode  validate at v1.3  ->  is04.StripLaterThan  ->  wire
//	Decode  is04.ParseX (absorbs + reports)  ->  validate at v1.3
//
// The asymmetry is deliberate. What we EMIT is strict — a v1.3 tree
// carrying a later minor's property is our bug, and AMWA IS-04-01
// fails the Node for it. What we READ is tolerant — a peer that sends
// one is still fully readable, and the deviation is reported as a
// compliance event rather than costing the operator the resource.
// A real EVS Neuron serves `controls` on its v1.0 Device tree.
//
// Adding a minor means adding rows to [is04.Since] and one package
// like this one. It does not mean editing this file.
package v13

import (
	"dhs/internal/amwa/codec/is04"
	"dhs/internal/amwa/codec/spec"
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
type Codec struct {
	// Reporter receives deviations found while DECODING a peer's
	// payload. Optional: the zero Codec absorbs silently.
	Reporter spec.Reporter
}

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

// EncodeNode renders a Node onto the v1.3 wire.
func (Codec) EncodeNode(n is04.Node) ([]byte, error) {
	if err := n.Validate(); err != nil {
		return nil, err
	}
	raw, err := json.MarshalIndent(n, "", "  ")
	if err != nil {
		return nil, err
	}
	return is04.StripLaterThan(raw, "node", "v1.3")
}

// DecodeNode parses a Node served on a v1.3 tree.
func (c Codec) DecodeNode(raw []byte) (is04.Node, error) {
	n, err := is04.ParseNode(raw, "v1.3", c.Reporter)
	if err != nil {
		return is04.Node{}, err
	}
	if err := c.ValidateNode(*n); err != nil {
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

// EncodeDevice renders a Device onto the v1.3 wire.
func (Codec) EncodeDevice(d is04.Device) ([]byte, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	raw, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return nil, err
	}
	return is04.StripLaterThan(raw, "device", "v1.3")
}

// DecodeDevice parses a Device served on a v1.3 tree.
func (c Codec) DecodeDevice(raw []byte) (is04.Device, error) {
	d, err := is04.ParseDevice(raw, "v1.3", c.Reporter)
	if err != nil {
		return is04.Device{}, err
	}
	if err := c.ValidateDevice(*d); err != nil {
		return is04.Device{}, err
	}
	return *d, nil
}

// ValidateDevice applies the v1.3.3 required-field set.
func (Codec) ValidateDevice(d is04.Device) error {
	return d.Validate()
}

// EncodeSource renders a Source onto the v1.3 wire.
func (Codec) EncodeSource(s is04.Source) ([]byte, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, err
	}
	return is04.StripLaterThan(raw, "source", "v1.3")
}

// DecodeSource parses a Source served on a v1.3 tree.
func (c Codec) DecodeSource(raw []byte) (is04.Source, error) {
	s, err := is04.ParseSource(raw, "v1.3", c.Reporter)
	if err != nil {
		return is04.Source{}, err
	}
	if err := c.ValidateSource(*s); err != nil {
		return is04.Source{}, err
	}
	return *s, nil
}

// ValidateSource applies the v1.3.3 required-field set.
func (Codec) ValidateSource(s is04.Source) error {
	return s.Validate()
}

// EncodeFlow renders a Flow onto the v1.3 wire.
func (Codec) EncodeFlow(f is04.Flow) ([]byte, error) {
	if err := f.Validate(); err != nil {
		return nil, err
	}
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return nil, err
	}
	return is04.StripLaterThan(raw, "flow", "v1.3")
}

// DecodeFlow parses a Flow served on a v1.3 tree.
func (c Codec) DecodeFlow(raw []byte) (is04.Flow, error) {
	f, err := is04.ParseFlow(raw, "v1.3", c.Reporter)
	if err != nil {
		return is04.Flow{}, err
	}
	if err := c.ValidateFlow(*f); err != nil {
		return is04.Flow{}, err
	}
	return *f, nil
}

// ValidateFlow applies the v1.3.3 required-field set + format-specific
// rules.
func (Codec) ValidateFlow(f is04.Flow) error {
	return f.Validate()
}

// EncodeSender renders a Sender onto the v1.3 wire.
func (Codec) EncodeSender(s is04.Sender) ([]byte, error) {
	if err := is04.GateTransport("sender", s.Transport, "v1.3"); err != nil {
		return nil, err
	}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, err
	}
	return is04.StripLaterThan(raw, "sender", "v1.3")
}

// DecodeSender parses a Sender served on a v1.3 tree.
func (c Codec) DecodeSender(raw []byte) (is04.Sender, error) {
	s, err := is04.ParseSender(raw, "v1.3", c.Reporter)
	if err != nil {
		return is04.Sender{}, err
	}
	if err := c.ValidateSender(*s); err != nil {
		return is04.Sender{}, err
	}
	return *s, nil
}

// ValidateSender applies the v1.3.3 required-field set + transport
// URN enum.
func (Codec) ValidateSender(s is04.Sender) error {
	return s.Validate()
}

// EncodeReceiver renders a Receiver onto the v1.3 wire.
func (Codec) EncodeReceiver(r is04.Receiver) ([]byte, error) {
	if err := is04.GateTransport("receiver", r.Transport, "v1.3"); err != nil {
		return nil, err
	}
	if err := r.Validate(); err != nil {
		return nil, err
	}
	raw, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, err
	}
	return is04.StripLaterThan(raw, "receiver", "v1.3")
}

// DecodeReceiver parses a Receiver served on a v1.3 tree.
func (c Codec) DecodeReceiver(raw []byte) (is04.Receiver, error) {
	r, err := is04.ParseReceiver(raw, "v1.3", c.Reporter)
	if err != nil {
		return is04.Receiver{}, err
	}
	if err := c.ValidateReceiver(*r); err != nil {
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
