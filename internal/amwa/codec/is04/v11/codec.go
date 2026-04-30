// Package v11 is the AMWA NMOS IS-04 v1.1.3 wire codec — one of three
// minors required by `internal/amwa/CLAUDE.md` "Versioning". It is a
// thin Strategy implementation of [is04.Codec] that wraps the
// canonical struct types in is04/ and gates fields per the v1.1.3
// spec text — fields introduced in later minors are stripped on
// encode and rejected on decode.
//
// Schema diffs vs v1.2.2 (computed from
// internal/amwa/codec/is04/testdata/schemas/):
//
//   - Node:   v1.2 ADDS required `interfaces` array. v1.1 has no such
//             property — strip on encode, reject on decode.
//   - Sender: v1.2 ADDS required `caps`, `interface_bindings`,
//             `subscription`. v1.1 has none of these — strip on
//             encode, reject on decode.
//   - Device / Source / Flow / Receiver: v1.2 = v1.1 at the top
//             level (polymorphic oneOf branches may differ — those
//             refinements layer in here as the spec audit
//             progresses).
//
// Implementation: every Encode method round-trips the canonical
// struct through encoding/json into a `map[string]json.RawMessage`,
// strips the v1.2+ keys, and re-marshals. Every Decode method
// pre-checks the raw JSON has none of the v1.2+ keys, then
// delegates to the canonical decoder. This keeps the canonical
// struct immutable and the gating localised to this package.
package v11

import (
	"acp/internal/amwa/codec/is04"
	"bytes"
	"encoding/json"
	"fmt"
)

// SpecPatch is the spec-text revision this codec strictly complies
// with — the latest patch within the v1.1 minor.
const SpecPatch = "v1.1.3"

// Codec implements [is04.Codec] for IS-04 wire minor v1.1.
//
// Stateless and safe for concurrent use.
type Codec struct{}

// New returns a Codec — equivalent to a zero-value Codec{}.
func New() Codec { return Codec{} }

// SpecID returns the AMWA NMOS catalogue slug for IS-04.
func (Codec) SpecID() string { return is04.SpecID }

// APIVer returns the wire URL minor — "v1.1".
func (Codec) APIVer() string { return "v1.1" }

// SpecPatch returns the latest patch release — "v1.1.3".
func (Codec) SpecPatch() string { return SpecPatch }

// nodeV12PlusFields names every Node-level property introduced in
// v1.2 or later. v1.1 wire MUST NOT carry these.
var nodeV12PlusFields = []string{"interfaces"}

// senderV12PlusFields names every Sender-level property introduced
// in v1.2 or later. v1.1 wire MUST NOT carry these.
var senderV12PlusFields = []string{"caps", "interface_bindings", "subscription"}

// EncodeNode marshals a Node for v1.1.3 — strips v1.2+ properties.
func (Codec) EncodeNode(n is04.Node) ([]byte, error) {
	if err := n.Validate(); err != nil {
		return nil, err
	}
	return stripFields(n, nodeV12PlusFields)
}

// DecodeNode parses a v1.1.3 Node payload. Rejects v1.2+ properties.
func (Codec) DecodeNode(raw []byte) (is04.Node, error) {
	if err := rejectFields(raw, nodeV12PlusFields, "node"); err != nil {
		return is04.Node{}, err
	}
	n, err := is04.DecodeNode(raw)
	if err != nil {
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

// EncodeDevice marshals a Device for v1.1.3 — top-level shape matches
// later minors so we delegate.
func (Codec) EncodeDevice(d is04.Device) ([]byte, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	return json.MarshalIndent(d, "", "  ")
}

// DecodeDevice parses a v1.1.3 Device payload.
func (Codec) DecodeDevice(raw []byte) (is04.Device, error) {
	d, err := is04.DecodeDevice(raw)
	if err != nil {
		return is04.Device{}, err
	}
	return *d, nil
}

// ValidateDevice applies the v1.1.3 required-field set.
func (Codec) ValidateDevice(d is04.Device) error {
	return d.Validate()
}

// EncodeSource marshals a Source for v1.1.3 — top-level shape
// matches v1.3.
func (Codec) EncodeSource(s is04.Source) ([]byte, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return json.MarshalIndent(s, "", "  ")
}

// DecodeSource parses a v1.1.3 Source payload.
func (Codec) DecodeSource(raw []byte) (is04.Source, error) {
	s, err := is04.DecodeSource(raw)
	if err != nil {
		return is04.Source{}, err
	}
	return *s, nil
}

// ValidateSource applies the v1.1.3 required-field set.
func (Codec) ValidateSource(s is04.Source) error {
	return s.Validate()
}

// EncodeFlow marshals a Flow for v1.1.3.
func (Codec) EncodeFlow(f is04.Flow) ([]byte, error) {
	if err := f.Validate(); err != nil {
		return nil, err
	}
	return json.MarshalIndent(f, "", "  ")
}

// DecodeFlow parses a v1.1.3 Flow payload.
func (Codec) DecodeFlow(raw []byte) (is04.Flow, error) {
	f, err := is04.DecodeFlow(raw)
	if err != nil {
		return is04.Flow{}, err
	}
	return *f, nil
}

// ValidateFlow applies the v1.1.3 required-field set.
func (Codec) ValidateFlow(f is04.Flow) error {
	return f.Validate()
}

// EncodeSender marshals a Sender for v1.1.3 — strips v1.2+
// properties (caps, interface_bindings, subscription).
func (Codec) EncodeSender(s is04.Sender) ([]byte, error) {
	if err := s.Validate(); err != nil {
		// v1.3 validator may require subscription / interface_bindings;
		// for v1.1 those are not in scope. Re-validate against the
		// v1.1 surface only.
		if err2 := validateSenderV11(s); err2 != nil {
			return nil, err2
		}
	}
	return stripFields(s, senderV12PlusFields)
}

// DecodeSender parses a v1.1.3 Sender payload. Rejects v1.2+ keys.
func (Codec) DecodeSender(raw []byte) (is04.Sender, error) {
	if err := rejectFields(raw, senderV12PlusFields, "sender"); err != nil {
		return is04.Sender{}, err
	}
	s, err := is04.DecodeSender(raw)
	if err != nil {
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
	if s.Label == "" {
		return fmt.Errorf("is04 v1.1: sender.label: required")
	}
	if s.Description == "" {
		return fmt.Errorf("is04 v1.1: sender.description: required")
	}
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

// EncodeReceiver marshals a Receiver for v1.1.3.
func (Codec) EncodeReceiver(r is04.Receiver) ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return json.MarshalIndent(r, "", "  ")
}

// DecodeReceiver parses a v1.1.3 Receiver payload.
func (Codec) DecodeReceiver(raw []byte) (is04.Receiver, error) {
	rr, err := is04.DecodeReceiver(raw)
	if err != nil {
		return is04.Receiver{}, err
	}
	return *rr, nil
}

// ValidateReceiver applies the v1.1.3 required-field set.
func (Codec) ValidateReceiver(r is04.Receiver) error {
	return r.Validate()
}

// stripFields marshals v into JSON, drops the named top-level keys,
// and re-marshals with 2-space indent. Returns the v1.1-clean payload.
func stripFields(v any, drop []string) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	for _, k := range drop {
		delete(m, k)
	}
	return json.MarshalIndent(m, "", "  ")
}

// rejectFields parses raw as a JSON object and returns an error if it
// carries any of the named top-level keys. Used to reject v1.2+
// payloads on the v1.1 decode path.
func rejectFields(raw []byte, forbidden []string, kind string) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	var m map[string]json.RawMessage
	if err := d.Decode(&m); err != nil {
		return fmt.Errorf("is04 v1.1: decode %s: %w", kind, err)
	}
	for _, k := range forbidden {
		if _, present := m[k]; present {
			return fmt.Errorf("is04 v1.1: %s.%s: forbidden in v1.1 (introduced in v1.2)", kind, k)
		}
	}
	return nil
}

func init() {
	is04.Register(Codec{})
}
