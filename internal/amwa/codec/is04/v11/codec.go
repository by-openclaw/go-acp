// Package v11 is the AMWA NMOS IS-04 v1.1.3 wire codec — one of the four
// minors required by `internal/amwa/CLAUDE.md` "Versioning". It is a
// Strategy implementation of [is04.Codec] over the canonical structs
// in is04/, and it holds ONLY what is specific to v1.1: this package's
// identity, and the v1.1.3 required-field validators.
//
// It holds no field-gating tables of its own. Which property arrived
// at which minor is stated once, as data, in [is04.Since]; both
// directions read it:
//
//	Encode  validate at v1.1  ->  is04.StripLaterThan  ->  wire
//	Decode  is04.ParseX (absorbs + reports)  ->  validate at v1.1
//
// The asymmetry is deliberate. What we EMIT is strict — a v1.1 tree
// carrying a later minor's property is our bug, and AMWA IS-04-01
// fails the Node for it. What we READ is tolerant — a peer that sends
// one is still fully readable, and the deviation is reported as a
// compliance event rather than costing the operator the resource.
// A real EVS Neuron serves `controls` on its v1.0 Device tree.
//
// Adding a minor means adding rows to [is04.Since] and one package
// like this one. It does not mean editing this file.
package v11

import (
	"dhs/internal/amwa/codec/is04"
	"dhs/internal/amwa/codec/spec"
	"encoding/json"
	"fmt"
)

// SpecPatch is the spec-text revision this codec strictly complies
// with — the latest patch within the v1.1 minor.
const SpecPatch = "v1.1.3"

// Codec implements [is04.Codec] for IS-04 wire minor v1.1.
//
// Stateless and safe for concurrent use.
type Codec struct {
	// Reporter receives deviations found while DECODING a peer's
	// payload. Optional: the zero Codec absorbs silently, which is
	// what every existing caller gets.
	Reporter spec.Reporter
}

// New returns a Codec — equivalent to a zero-value Codec{}.
func New() Codec { return Codec{} }

// SpecID returns the AMWA NMOS catalogue slug for IS-04.
func (Codec) SpecID() string { return is04.SpecID }

// APIVer returns the wire URL minor — "v1.1".
func (Codec) APIVer() string { return "v1.1" }

// SpecPatch returns the latest patch release — "v1.1.3".
func (Codec) SpecPatch() string { return SpecPatch }

// EncodeNode renders a Node onto the v1.1 wire.
func (Codec) EncodeNode(n is04.Node) ([]byte, error) {
	if err := n.Validate(); err != nil {
		return nil, err
	}
	raw, err := json.MarshalIndent(n, "", "  ")
	if err != nil {
		return nil, err
	}
	return is04.StripLaterThan(raw, "node", "v1.1")
}

// DecodeNode parses a Node served on a v1.1 tree.
func (c Codec) DecodeNode(raw []byte) (is04.Node, error) {
	n, err := is04.ParseNode(raw, "v1.1", c.Reporter)
	if err != nil {
		return is04.Node{}, err
	}
	if err := c.ValidateNode(*n); err != nil {
		return is04.Node{}, err
	}
	return *n, nil
}

// ValidateNode applies the v1.1.3 required-field set. The canonical
// validator covers v1.3, which marks `interfaces` required; for
// v1.1 we accept Nodes with no Interfaces slice (it never appears
// on the v1.1 wire).
func (Codec) ValidateNode(n is04.Node) error {
	// Force interfaces empty on the canonical struct's perspective —
	// caller can leave the field unset; we only check the v1.1 set.
	saved := n.Interfaces
	n.Interfaces = nil
	defer func() { n.Interfaces = saved }()
	if err := n.Validate(); err != nil {
		// The v1.3 validator includes `interfaces` in required. For
		// v1.1 we let it pass — a non-nil but empty Interfaces slice
		// is the canonical equivalent of "field not present on wire".
		// The Validate path treats nil Interfaces as missing, so here
		// we ensure the slice is present-but-empty.
		n.Interfaces = []is04.NodeIface{}
		if err2 := n.Validate(); err2 != nil {
			return err2
		}
	}
	return nil
}

// EncodeDevice renders a Device onto the v1.1 wire.
func (Codec) EncodeDevice(d is04.Device) ([]byte, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	raw, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return nil, err
	}
	return is04.StripLaterThan(raw, "device", "v1.1")
}

// DecodeDevice parses a Device served on a v1.1 tree.
func (c Codec) DecodeDevice(raw []byte) (is04.Device, error) {
	d, err := is04.ParseDevice(raw, "v1.1", c.Reporter)
	if err != nil {
		return is04.Device{}, err
	}
	if err := c.ValidateDevice(*d); err != nil {
		return is04.Device{}, err
	}
	return *d, nil
}

// ValidateDevice applies the v1.1.3 required-field set.
func (Codec) ValidateDevice(d is04.Device) error {
	return d.Validate()
}

// EncodeSource renders a Source onto the v1.1 wire.
func (Codec) EncodeSource(s is04.Source) ([]byte, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, err
	}
	return is04.StripLaterThan(raw, "source", "v1.1")
}

// DecodeSource parses a Source served on a v1.1 tree.
func (c Codec) DecodeSource(raw []byte) (is04.Source, error) {
	s, err := is04.ParseSource(raw, "v1.1", c.Reporter)
	if err != nil {
		return is04.Source{}, err
	}
	if err := c.ValidateSource(*s); err != nil {
		return is04.Source{}, err
	}
	return *s, nil
}

// ValidateSource applies the v1.1.3 required-field set.
func (Codec) ValidateSource(s is04.Source) error {
	return s.Validate()
}

// EncodeFlow renders a Flow onto the v1.1 wire.
func (Codec) EncodeFlow(f is04.Flow) ([]byte, error) {
	if err := f.Validate(); err != nil {
		return nil, err
	}
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return nil, err
	}
	return is04.StripLaterThan(raw, "flow", "v1.1")
}

// DecodeFlow parses a Flow served on a v1.1 tree.
func (c Codec) DecodeFlow(raw []byte) (is04.Flow, error) {
	f, err := is04.ParseFlow(raw, "v1.1", c.Reporter)
	if err != nil {
		return is04.Flow{}, err
	}
	if err := c.ValidateFlow(*f); err != nil {
		return is04.Flow{}, err
	}
	return *f, nil
}

// ValidateFlow applies the v1.1.3 required-field set.
func (Codec) ValidateFlow(f is04.Flow) error {
	return f.Validate()
}

// EncodeSender renders a Sender onto the v1.1 wire.
func (Codec) EncodeSender(s is04.Sender) ([]byte, error) {
	if err := is04.GateTransport("sender", s.Transport, "v1.1"); err != nil {
		return nil, err
	}
	if err := s.Validate(); err != nil {
		// The canonical validator targets v1.3.3, which requires
		// subscription / interface_bindings. Neither exists on the
		// v1.1 surface, so fall back to the v1.1 required set.
		if err2 := validateSenderV11(s); err2 != nil {
			return nil, err2
		}
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, err
	}
	return is04.StripLaterThan(raw, "sender", "v1.1")
}

// DecodeSender parses a Sender served on a v1.1 tree.
func (c Codec) DecodeSender(raw []byte) (is04.Sender, error) {
	s, err := is04.ParseSender(raw, "v1.1", c.Reporter)
	if err != nil {
		return is04.Sender{}, err
	}
	if err := c.ValidateSender(*s); err != nil {
		return is04.Sender{}, err
	}
	return *s, nil
}

// ValidateSender applies the v1.1.3 required-field set — drops the
// v1.2-introduced caps / interface_bindings / subscription
// requirements.
func (Codec) ValidateSender(s is04.Sender) error {
	return validateSenderV11(s)
}

// validateSenderV11 applies the v1.1.3 required-field set directly:
// resource_core + flow_id (UUID or null) + transport + device_id +
// manifest_href (string OR null). The v1.2 additions
// (caps / interface_bindings / subscription) are NOT required on v1.1
// — we apply the rules ourselves rather than delegating to the v1.3
// validator which would reject a missing interface_bindings.
func validateSenderV11(s is04.Sender) error {
	if s.ID == "" || !is04.IsValidUUID(s.ID) {
		return fmt.Errorf("is04 v1.1: sender.id %q: must be UUID v1-5", s.ID)
	}
	if s.Version == "" || !is04.IsValidVersion(s.Version) {
		return fmt.Errorf("is04 v1.1: sender.version %q: must match `<sec>:<nsec>`", s.Version)
	}
	// label and description are REQUIRED TO BE PRESENT, not required
	// to be non-empty — resource_core.json sets no minLength on
	// either, so "" is a legal value and a very common one.
	//
	// Testing the Go zero value cannot tell "" apart from absent, so
	// this check rejected every device that ships blank descriptions.
	// It failed all 176 Senders on a real EVS Neuron, which sends
	// `"description": ""` throughout. Presence belongs to the decoder,
	// which sees the raw JSON keys; the validator only judges values.
	if s.Tags == nil {
		return fmt.Errorf("is04 v1.1: sender.tags: required")
	}
	if s.FlowID != nil && *s.FlowID != "" && !is04.IsValidUUID(*s.FlowID) {
		return fmt.Errorf("is04 v1.1: sender.flow_id %q: must be UUID v1-5 or null", *s.FlowID)
	}
	if s.Transport == "" || !is04.IsValidTransportURN(s.Transport) {
		return fmt.Errorf("is04 v1.1: sender.transport %q: must be a valid transport URN", s.Transport)
	}
	if s.DeviceID == "" || !is04.IsValidUUID(s.DeviceID) {
		return fmt.Errorf("is04 v1.1: sender.device_id %q: must be UUID v1-5", s.DeviceID)
	}
	if s.ManifestHref != nil && *s.ManifestHref == "" {
		return fmt.Errorf("is04 v1.1: sender.manifest_href: empty string disallowed (use null)")
	}
	return nil
}

// EncodeReceiver renders a Receiver onto the v1.1 wire.
func (Codec) EncodeReceiver(r is04.Receiver) ([]byte, error) {
	if err := is04.GateTransport("receiver", r.Transport, "v1.1"); err != nil {
		return nil, err
	}
	if err := r.Validate(); err != nil {
		return nil, err
	}
	raw, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, err
	}
	return is04.StripLaterThan(raw, "receiver", "v1.1")
}

// DecodeReceiver parses a Receiver served on a v1.1 tree.
func (c Codec) DecodeReceiver(raw []byte) (is04.Receiver, error) {
	r, err := is04.ParseReceiver(raw, "v1.1", c.Reporter)
	if err != nil {
		return is04.Receiver{}, err
	}
	if err := c.ValidateReceiver(*r); err != nil {
		return is04.Receiver{}, err
	}
	return *r, nil
}

// ValidateReceiver applies the v1.1.3 required-field set.
func (Codec) ValidateReceiver(r is04.Receiver) error {
	return r.Validate()
}

func init() {
	is04.Register(Codec{})
}
