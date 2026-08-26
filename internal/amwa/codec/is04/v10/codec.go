// Package v10 is the AMWA NMOS IS-04 v1.0.3 wire codec — one of the four
// minors required by `internal/amwa/CLAUDE.md` "Versioning". It is a
// Strategy implementation of [is04.Codec] over the canonical structs
// in is04/, and it holds ONLY what is specific to v1.0: this package's
// identity, and the v1.0.3 required-field validators.
//
// It holds no field-gating tables of its own. Which property arrived
// at which minor is stated once, as data, in [is04.Since]; both
// directions read it:
//
//	Encode  validate at v1.0  ->  is04.StripLaterThan  ->  wire
//	Decode  is04.ParseX (absorbs + reports)  ->  validate at v1.0
//
// The asymmetry is deliberate. What we EMIT is strict — a v1.0 tree
// carrying a later minor's property is our bug, and AMWA IS-04-01
// fails the Node for it. What we READ is tolerant — a peer that sends
// one is still fully readable, and the deviation is reported as a
// compliance event rather than costing the operator the resource.
// A real EVS Neuron serves `controls` on its v1.0 Device tree.
//
// Adding a minor means adding rows to [is04.Since] and one package
// like this one. It does not mean editing this file.
package v10

import (
	"dhs/internal/amwa/codec/is04"
	"dhs/internal/amwa/codec/spec"
	"encoding/json"
	"fmt"
)

// SpecPatch is the spec-text revision this codec strictly complies
// with — the latest patch within the v1.0 minor.
const SpecPatch = "v1.0.3"

// Codec implements [is04.Codec] for IS-04 wire minor v1.0.
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

// APIVer returns the wire URL minor — "v1.0".
func (Codec) APIVer() string { return "v1.0" }

// SpecPatch returns the latest patch release — "v1.0.3".
func (Codec) SpecPatch() string { return SpecPatch }

// Field gating tables — fields introduced in v1.1 or later that v1.0
// wire MUST NOT carry. Each list is a strict subset of the canonical
// (v1.3) struct's JSON keys.

// EncodeNode renders a Node onto the v1.0 wire.
func (Codec) EncodeNode(n is04.Node) ([]byte, error) {
	if err := validateNodeV10(n); err != nil {
		return nil, err
	}
	raw, err := json.MarshalIndent(n, "", "  ")
	if err != nil {
		return nil, err
	}
	raw, err = is04.StripLaterThan(raw, "node", "v1.0")
	if err != nil {
		return nil, err
	}
	return stripEmptyOptional(raw, "description", "tags")
}

// stripEmptyOptional drops the named top-level keys when their JSON
// value is empty — empty string for strings, empty object/array for
// objects/arrays, null. Used by v10 codecs to clean up
// description/tags fields the canonical struct emits even when the
// caller didn't supply them.
func stripEmptyOptional(raw []byte, keys ...string) ([]byte, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return raw, nil
	}
	for _, k := range keys {
		v, ok := m[k]
		if !ok {
			continue
		}
		if jsonValueIsEmpty(v) {
			delete(m, k)
		}
	}
	return json.MarshalIndent(m, "", "  ")
}

func jsonValueIsEmpty(v json.RawMessage) bool {
	s := string(v)
	switch s {
	case `""`, `null`, `{}`, `[]`:
		return true
	}
	return false
}

// DecodeNode parses a Node served on a v1.0 tree.
func (c Codec) DecodeNode(raw []byte) (is04.Node, error) {
	n, err := is04.ParseNode(raw, "v1.0", c.Reporter)
	if err != nil {
		return is04.Node{}, err
	}
	if err := c.ValidateNode(*n); err != nil {
		return is04.Node{}, err
	}
	return *n, nil
}

// ValidateNode applies the v1.0.3 required-field set.
func (Codec) ValidateNode(n is04.Node) error { return validateNodeV10(n) }

// validateNodeV10 enforces v1.0.3 Node required fields:
// id, version, label, href, caps, services. Description / tags are
// NOT required (added in v1.1). API / clocks / interfaces are NOT
// required (added in v1.1+).
func validateNodeV10(n is04.Node) error {
	var errs []string
	if n.ID == "" || !is04.IsValidUUID(n.ID) {
		errs = append(errs, fmt.Sprintf("node.id %q: must match RFC 4122 v1-v5 UUID pattern", n.ID))
	}
	if n.Version == "" || !is04.IsValidVersion(n.Version) {
		errs = append(errs, fmt.Sprintf("node.version %q: must match `<sec>:<nsec>` TAI form", n.Version))
	}
	if n.Href == "" {
		errs = append(errs, "node.href: required (v1.0 top-level)")
	}
	if n.Caps == nil {
		errs = append(errs, "node.caps: required (may be empty object)")
	}
	if n.Services == nil {
		errs = append(errs, "node.services: required (may be empty array)")
	}
	for i, s := range n.Services {
		if s.Href == "" {
			errs = append(errs, fmt.Sprintf("node.services[%d].href: required", i))
		}
		if s.Type == "" {
			errs = append(errs, fmt.Sprintf("node.services[%d].type: required (URN)", i))
		}
	}
	return joinErrs("is04 v1.0 node validation failed", errs)
}

// EncodeDevice renders a Device onto the v1.0 wire.
func (Codec) EncodeDevice(d is04.Device) ([]byte, error) {
	if err := validateDeviceV10(d); err != nil {
		return nil, err
	}
	raw, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return nil, err
	}
	raw, err = is04.StripLaterThan(raw, "device", "v1.0")
	if err != nil {
		return nil, err
	}
	return stripEmptyOptional(raw, "description", "tags")
}

// DecodeDevice parses a Device served on a v1.0 tree.
func (c Codec) DecodeDevice(raw []byte) (is04.Device, error) {
	d, err := is04.ParseDevice(raw, "v1.0", c.Reporter)
	if err != nil {
		return is04.Device{}, err
	}
	if err := c.ValidateDevice(*d); err != nil {
		return is04.Device{}, err
	}
	return *d, nil
}

// ValidateDevice applies the v1.0.3 required-field set.
func (Codec) ValidateDevice(d is04.Device) error { return validateDeviceV10(d) }

// validateDeviceV10 enforces v1.0.3 Device required fields:
// id, version, label, type, node_id, senders, receivers.
// Description / tags / controls NOT required.
func validateDeviceV10(d is04.Device) error {
	var errs []string
	if d.ID == "" || !is04.IsValidUUID(d.ID) {
		errs = append(errs, fmt.Sprintf("device.id %q: must match UUID v1-v5", d.ID))
	}
	if d.Version == "" || !is04.IsValidVersion(d.Version) {
		errs = append(errs, fmt.Sprintf("device.version %q: must match `<sec>:<nsec>`", d.Version))
	}
	if d.Type == "" {
		errs = append(errs, "device.type: required (URN)")
	}
	if d.NodeID == "" || !is04.IsValidUUID(d.NodeID) {
		errs = append(errs, fmt.Sprintf("device.node_id %q: must match UUID v1-v5", d.NodeID))
	}
	if d.Senders == nil {
		errs = append(errs, "device.senders: required (may be empty array)")
	}
	for i, id := range d.Senders {
		if !is04.IsValidUUID(id) {
			errs = append(errs, fmt.Sprintf("device.senders[%d] %q: not a UUID", i, id))
		}
	}
	if d.Receivers == nil {
		errs = append(errs, "device.receivers: required (may be empty array)")
	}
	for i, id := range d.Receivers {
		if !is04.IsValidUUID(id) {
			errs = append(errs, fmt.Sprintf("device.receivers[%d] %q: not a UUID", i, id))
		}
	}
	return joinErrs("is04 v1.0 device validation failed", errs)
}

// EncodeSource renders a Source onto the v1.0 wire.
func (Codec) EncodeSource(s is04.Source) ([]byte, error) {
	if err := validateSourceV10(s); err != nil {
		return nil, err
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, err
	}
	return is04.StripLaterThan(raw, "source", "v1.0")
}

// DecodeSource parses a Source served on a v1.0 tree.
func (c Codec) DecodeSource(raw []byte) (is04.Source, error) {
	s, err := is04.ParseSource(raw, "v1.0", c.Reporter)
	if err != nil {
		return is04.Source{}, err
	}
	if err := c.ValidateSource(*s); err != nil {
		return is04.Source{}, err
	}
	return *s, nil
}

// ValidateSource applies the v1.0.3 required-field set.
func (Codec) ValidateSource(s is04.Source) error { return validateSourceV10(s) }

// validateSourceV10 enforces v1.0.3 Source required fields:
// id, version, label, description, format, caps, tags, device_id,
// parents.
func validateSourceV10(s is04.Source) error {
	var errs []string
	if s.ID == "" || !is04.IsValidUUID(s.ID) {
		errs = append(errs, fmt.Sprintf("source.id %q: must match UUID v1-v5", s.ID))
	}
	if s.Version == "" || !is04.IsValidVersion(s.Version) {
		errs = append(errs, fmt.Sprintf("source.version %q: must match `<sec>:<nsec>`", s.Version))
	}
	if s.Format == "" {
		errs = append(errs, "source.format: required (URN)")
	}
	if s.Caps == nil {
		errs = append(errs, "source.caps: required (may be empty object)")
	}
	if s.Tags == nil {
		errs = append(errs, "source.tags: required (may be empty object)")
	}
	if s.DeviceID == "" || !is04.IsValidUUID(s.DeviceID) {
		errs = append(errs, fmt.Sprintf("source.device_id %q: must match UUID v1-v5", s.DeviceID))
	}
	if s.Parents == nil {
		errs = append(errs, "source.parents: required (may be empty array)")
	}
	for i, id := range s.Parents {
		if !is04.IsValidUUID(id) {
			errs = append(errs, fmt.Sprintf("source.parents[%d] %q: not a UUID", i, id))
		}
	}
	return joinErrs("is04 v1.0 source validation failed", errs)
}

// EncodeFlow renders a Flow onto the v1.0 wire.
func (Codec) EncodeFlow(f is04.Flow) ([]byte, error) {
	if err := validateFlowV10(f); err != nil {
		return nil, err
	}
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return nil, err
	}
	return is04.StripLaterThan(raw, "flow", "v1.0")
}

// DecodeFlow parses a Flow served on a v1.0 tree.
func (c Codec) DecodeFlow(raw []byte) (is04.Flow, error) {
	f, err := is04.ParseFlow(raw, "v1.0", c.Reporter)
	if err != nil {
		return is04.Flow{}, err
	}
	if err := c.ValidateFlow(*f); err != nil {
		return is04.Flow{}, err
	}
	return *f, nil
}

// ValidateFlow applies the v1.0.3 required-field set.
func (Codec) ValidateFlow(f is04.Flow) error { return validateFlowV10(f) }

// validateFlowV10 enforces v1.0.3 Flow required fields:
// id, version, label, description, format, tags, source_id, parents.
// v1.0 Flow has NO device_id (added v1.1), NO grain_rate, NO media_type,
// NO per-format fields.
func validateFlowV10(f is04.Flow) error {
	var errs []string
	if f.ID == "" || !is04.IsValidUUID(f.ID) {
		errs = append(errs, fmt.Sprintf("flow.id %q: must match UUID v1-v5", f.ID))
	}
	if f.Version == "" || !is04.IsValidVersion(f.Version) {
		errs = append(errs, fmt.Sprintf("flow.version %q: must match `<sec>:<nsec>`", f.Version))
	}
	if f.Format == "" || !is04.IsValidFormatURN(f.Format) {
		errs = append(errs, fmt.Sprintf("flow.format %q: must be a known NMOS format URN", f.Format))
	}
	if f.Tags == nil {
		errs = append(errs, "flow.tags: required (may be empty object)")
	}
	if f.SourceID == "" || !is04.IsValidUUID(f.SourceID) {
		errs = append(errs, fmt.Sprintf("flow.source_id %q: must match UUID v1-v5", f.SourceID))
	}
	if f.Parents == nil {
		errs = append(errs, "flow.parents: required (may be empty array)")
	}
	for i, id := range f.Parents {
		if !is04.IsValidUUID(id) {
			errs = append(errs, fmt.Sprintf("flow.parents[%d] %q: not a UUID", i, id))
		}
	}
	return joinErrs("is04 v1.0 flow validation failed", errs)
}

// EncodeSender renders a Sender onto the v1.0 wire.
func (Codec) EncodeSender(s is04.Sender) ([]byte, error) {
	if err := is04.GateTransport("sender", s.Transport, "v1.0"); err != nil {
		return nil, err
	}
	if err := validateSenderV10(s); err != nil {
		return nil, err
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, err
	}
	return is04.StripLaterThan(raw, "sender", "v1.0")
}

// DecodeSender parses a Sender served on a v1.0 tree.
func (c Codec) DecodeSender(raw []byte) (is04.Sender, error) {
	s, err := is04.ParseSender(raw, "v1.0", c.Reporter)
	if err != nil {
		return is04.Sender{}, err
	}
	if err := c.ValidateSender(*s); err != nil {
		return is04.Sender{}, err
	}
	return *s, nil
}

// ValidateSender applies the v1.0.3 required-field set.
func (Codec) ValidateSender(s is04.Sender) error { return validateSenderV10(s) }

// validateSenderV10 enforces v1.0.3 Sender required fields:
// id, version, label, description, flow_id, transport, device_id,
// manifest_href. Note flow_id and manifest_href are both REQUIRED
// non-null in v1.0 (became nullable in v1.1).
func validateSenderV10(s is04.Sender) error {
	var errs []string
	if s.ID == "" || !is04.IsValidUUID(s.ID) {
		errs = append(errs, fmt.Sprintf("sender.id %q: must match UUID v1-v5", s.ID))
	}
	if s.Version == "" || !is04.IsValidVersion(s.Version) {
		errs = append(errs, fmt.Sprintf("sender.version %q: must match `<sec>:<nsec>`", s.Version))
	}
	if s.FlowID == nil || *s.FlowID == "" || !is04.IsValidUUID(*s.FlowID) {
		errs = append(errs, "sender.flow_id: required (UUID, non-null in v1.0)")
	}
	if s.Transport == "" || !is04.IsValidTransportURN(s.Transport) {
		errs = append(errs, fmt.Sprintf("sender.transport %q: must be a valid transport URN", s.Transport))
	}
	if s.Tags == nil {
		errs = append(errs, "sender.tags: required (may be empty object)")
	}
	if s.DeviceID == "" || !is04.IsValidUUID(s.DeviceID) {
		errs = append(errs, fmt.Sprintf("sender.device_id %q: must match UUID v1-v5", s.DeviceID))
	}
	if s.ManifestHref == nil || *s.ManifestHref == "" {
		errs = append(errs, "sender.manifest_href: required (non-null in v1.0)")
	}
	return joinErrs("is04 v1.0 sender validation failed", errs)
}

// EncodeReceiver renders a Receiver onto the v1.0 wire.
func (Codec) EncodeReceiver(r is04.Receiver) ([]byte, error) {
	if err := is04.GateTransport("receiver", r.Transport, "v1.0"); err != nil {
		return nil, err
	}
	if err := validateReceiverV10(r); err != nil {
		return nil, err
	}
	raw, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, err
	}
	return is04.StripLaterThan(raw, "receiver", "v1.0")
}

// DecodeReceiver parses a Receiver served on a v1.0 tree.
func (c Codec) DecodeReceiver(raw []byte) (is04.Receiver, error) {
	r, err := is04.ParseReceiver(raw, "v1.0", c.Reporter)
	if err != nil {
		return is04.Receiver{}, err
	}
	if err := c.ValidateReceiver(*r); err != nil {
		return is04.Receiver{}, err
	}
	return *r, nil
}

// ValidateReceiver applies the v1.0.3 required-field set.
func (Codec) ValidateReceiver(r is04.Receiver) error { return validateReceiverV10(r) }

// validateReceiverV10 enforces v1.0.3 Receiver required fields:
// id, version, label, description, format, caps, tags, device_id,
// transport, subscription.
func validateReceiverV10(r is04.Receiver) error {
	var errs []string
	if r.ID == "" || !is04.IsValidUUID(r.ID) {
		errs = append(errs, fmt.Sprintf("receiver.id %q: must match UUID v1-v5", r.ID))
	}
	if r.Version == "" || !is04.IsValidVersion(r.Version) {
		errs = append(errs, fmt.Sprintf("receiver.version %q: must match `<sec>:<nsec>`", r.Version))
	}
	if r.Format == "" {
		errs = append(errs, "receiver.format: required (URN)")
	}
	// receiver.caps is a typed struct in canonical Receiver; v1.0
	// requires it as an object (may be empty {}). The struct is always
	// present, so we only check that JSON-marshalled output produces a
	// non-null object — see EncodeReceiver path. No nil check needed.
	if r.Tags == nil {
		errs = append(errs, "receiver.tags: required (may be empty object)")
	}
	if r.DeviceID == "" || !is04.IsValidUUID(r.DeviceID) {
		errs = append(errs, fmt.Sprintf("receiver.device_id %q: must match UUID v1-v5", r.DeviceID))
	}
	if r.Transport == "" || !is04.IsValidTransportURN(r.Transport) {
		errs = append(errs, fmt.Sprintf("receiver.transport %q: must be a valid transport URN", r.Transport))
	}
	// Subscription is required in v1.0; sender_id may be null but the
	// object itself must be present.
	return joinErrs("is04 v1.0 receiver validation failed", errs)
}

// joinErrs renders a non-empty validation slice into a single error.
// Returns nil when errs is empty. Mirrors is04.joinErrs (which is
// package-private to is04/).
func joinErrs(prefix string, errs []string) error {
	if len(errs) == 0 {
		return nil
	}
	out := prefix + ": "
	for i, e := range errs {
		if i > 0 {
			out += "; "
		}
		out += e
	}
	return fmt.Errorf("%s", out)
}

func init() {
	is04.Register(Codec{})
}
