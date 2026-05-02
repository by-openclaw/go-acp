// Package v10 is the AMWA NMOS IS-04 v1.0.3 wire codec — one of four
// minors required by `internal/amwa/CLAUDE.md` "Versioning". It is a
// thin Strategy implementation of [is04.Codec] that wraps the
// canonical struct types in is04/ and gates fields per the v1.0.3
// spec text — fields introduced in v1.1+ are stripped on encode and
// rejected on decode.
//
// Schema diffs vs v1.1.3 (computed from
// internal/amwa/codec/is04/testdata/schemas/v1.0.3/ and
// /v1.1.3/):
//
//   - Node:     v1.0 lacks `description`, `tags` (added in v1.1
//               via resource_core), `api`, `clocks`, `interfaces`
//               (added in v1.1 / v1.2). v1.0 Node has top-level
//               `href` REQUIRED (not deprecated yet).
//   - Device:   v1.0 lacks `description`, `tags`, `controls[]`
//               (controls added in v1.1).
//   - Source:   v1.0 has `description`, `tags`, `parents[]`. Lacks
//               `clock_name` (v1.1), `grain_rate` (v1.1), `channels`
//               (v1.2 audio variant).
//   - Flow:     v1.0 has `description`, `tags`, `parents[]`. Lacks
//               `device_id` (v1.1), `grain_rate` (v1.1),
//               `media_type` (v1.1), and every per-format field
//               (frame_width, sample_rate, etc. — v1.1+).
//   - Sender:   v1.0 has `description`, `tags`. Lacks v1.2 additions:
//               `caps`, `interface_bindings`, `subscription`.
//   - Receiver: v1.0 has the same top-level shape as v1.1; lacks
//               `interface_bindings` (v1.2).
//
// Implementation: every Encode method round-trips the canonical
// struct through encoding/json into a `map[string]json.RawMessage`,
// strips the v1.1+ keys, and re-marshals. Every Decode method
// pre-checks the raw JSON has none of the v1.1+ keys, then
// delegates to the canonical decoder. v1.0-specific validators
// implement the v1.0.3 required-field set (which differs from the
// canonical Validate that targets v1.3.3).
package v10

import (
	"acp/internal/amwa/codec/is04"
	"bytes"
	"encoding/json"
	"fmt"
)

// SpecPatch is the spec-text revision this codec strictly complies
// with — the latest patch within the v1.0 minor.
const SpecPatch = "v1.0.3"

// Codec implements [is04.Codec] for IS-04 wire minor v1.0.
//
// Stateless and safe for concurrent use.
type Codec struct{}

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

// Fields ACTUALLY introduced in v1.1+ that v1.0 wire MUST NOT carry.
// `description` + `tags` are NOT in this list because v1.0.3 allows
// them as additionalProperties — the AMWA IS-04-02 test_23_1 /
// test_24_1 (basic-query / RQL filter) explicitly populates
// `description` in the v1.0 body and expects it to round-trip
// through the WS grain. We instead drop them only when empty
// (zero-value Go struct field) via stripEmpty in EncodeNode/Device,
// which matches the behavior of nmos-cpp's v1.0 codec.
var nodeV11PlusFields = []string{
	"api",        // added v1.1
	"clocks",     // added v1.1
	"interfaces", // added v1.2
}

var deviceV11PlusFields = []string{
	"controls", // added v1.1
}

var sourceV11PlusFields = []string{
	"clock_name",  // added v1.1
	"grain_rate",  // added v1.1
	"channels",    // added v1.2 (audio variant)
	// description + tags are core fields in v1.0+ for sources;
	// IS04Utils.downgrade_resource keeps them on Source. No strip.
}

var flowV11PlusFields = []string{
	"device_id",               // added v1.1
	"grain_rate",              // added v1.1
	"media_type",              // added v1.1 (top-level)
	"components",              // added v1.1 (video raw — array)
	"frame_width",             // added v1.1 (video)
	"frame_height",            // added v1.1 (video)
	"interlace_mode",          // added v1.1 (video)
	"colorspace",              // added v1.1 (video)
	"transfer_characteristic", // added v1.1 (video)
	"sample_rate",             // added v1.1 (audio)
	"bit_depth",               // added v1.1 (audio)
	"DID_SDID",                // added v1.1 (sdianc_data)
	"event_type",              // added v1.1 (json_data)
}

var senderV11PlusFields = []string{
	"caps",               // added v1.2
	"interface_bindings", // added v1.2
	"subscription",       // added v1.2
}

var receiverV11PlusFields = []string{
	"interface_bindings", // added v1.2
}

// EncodeNode marshals a Node for v1.0.3 — strips v1.1+ properties
// and the v1.3-only `authorization` flag from services[*]. (api block
// is fully stripped, so endpoints[*].authorization is moot.) Empty
// `description`/`tags` are also stripped: v1.0 doesn't require them
// and the AMWA Testing tool's per-version fixture lacks them.
func (Codec) EncodeNode(n is04.Node) ([]byte, error) {
	if err := validateNodeV10(n); err != nil {
		return nil, err
	}
	raw, err := stripFields(n, nodeV11PlusFields)
	if err != nil {
		return nil, err
	}
	raw, err = stripEmptyOptional(raw, "description", "tags")
	if err != nil {
		return nil, err
	}
	return stripAuthFromServices(raw)
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

// stripAuthFromServices removes the `authorization` key from every
// element of `services[]` in a JSON object body. Used by codecs whose
// wire schema predates IS-10 (`authorization` was added to the
// services entry in IS-04 v1.3 alongside BCP-003-02 auth).
func stripAuthFromServices(raw []byte) ([]byte, error) {
	return stripFromArray(raw, []string{"services"}, "authorization")
}

// stripFromArray walks raw as JSON, descends into the nested object
// path, then drops `key` from every element of the array at the leaf.
// Re-marshals with 2-space indent. Idempotent — silently returns the
// input unchanged when the path doesn't resolve to an array.
func stripFromArray(raw []byte, path []string, key string) ([]byte, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	cur := v
	for _, p := range path[:len(path)-1] {
		m, ok := cur.(map[string]any)
		if !ok {
			return raw, nil
		}
		cur = m[p]
	}
	leaf, ok := cur.(map[string]any)
	if !ok {
		return raw, nil
	}
	arr, ok := leaf[path[len(path)-1]].([]any)
	if !ok {
		return raw, nil
	}
	for _, el := range arr {
		if em, ok := el.(map[string]any); ok {
			delete(em, key)
		}
	}
	return json.MarshalIndent(v, "", "  ")
}

// DecodeNode parses a v1.0.3 Node payload. Rejects v1.1+ keys.
func (Codec) DecodeNode(raw []byte) (is04.Node, error) {
	if err := rejectFields(raw, nodeV11PlusFields, "node"); err != nil {
		return is04.Node{}, err
	}
	// Decode permissively into the canonical struct (the v1.0 fields
	// are a subset of canonical Node), then run the v1.0 validator.
	var n is04.Node
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&n); err != nil {
		return is04.Node{}, fmt.Errorf("is04 v1.0: decode node: %w", err)
	}
	if err := validateNodeV10(n); err != nil {
		return is04.Node{}, err
	}
	return n, nil
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

// EncodeDevice marshals a Device for v1.0.3 — strips `controls`
// (v1.1) plus empty `description`/`tags` to match the IS04Utils v1.0
// downgrade shape.
func (Codec) EncodeDevice(d is04.Device) ([]byte, error) {
	if err := validateDeviceV10(d); err != nil {
		return nil, err
	}
	raw, err := stripFields(d, deviceV11PlusFields)
	if err != nil {
		return nil, err
	}
	return stripEmptyOptional(raw, "description", "tags")
}

// DecodeDevice parses a v1.0.3 Device payload. Rejects v1.1+ keys.
func (Codec) DecodeDevice(raw []byte) (is04.Device, error) {
	if err := rejectFields(raw, deviceV11PlusFields, "device"); err != nil {
		return is04.Device{}, err
	}
	var d is04.Device
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&d); err != nil {
		return is04.Device{}, fmt.Errorf("is04 v1.0: decode device: %w", err)
	}
	if err := validateDeviceV10(d); err != nil {
		return is04.Device{}, err
	}
	return d, nil
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

// EncodeSource marshals a Source for v1.0.3 — strips clock_name,
// grain_rate, channels.
func (Codec) EncodeSource(s is04.Source) ([]byte, error) {
	if err := validateSourceV10(s); err != nil {
		return nil, err
	}
	return stripFields(s, sourceV11PlusFields)
}

// DecodeSource parses a v1.0.3 Source payload. Rejects v1.1+ keys.
func (Codec) DecodeSource(raw []byte) (is04.Source, error) {
	if err := rejectFields(raw, sourceV11PlusFields, "source"); err != nil {
		return is04.Source{}, err
	}
	var s is04.Source
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&s); err != nil {
		return is04.Source{}, fmt.Errorf("is04 v1.0: decode source: %w", err)
	}
	if err := validateSourceV10(s); err != nil {
		return is04.Source{}, err
	}
	return s, nil
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

// EncodeFlow marshals a Flow for v1.0.3 — strips device_id, grain_rate,
// media_type, and every per-format field added in v1.1+.
func (Codec) EncodeFlow(f is04.Flow) ([]byte, error) {
	if err := validateFlowV10(f); err != nil {
		return nil, err
	}
	return stripFields(f, flowV11PlusFields)
}

// DecodeFlow parses a v1.0.3 Flow payload. Rejects v1.1+ keys.
func (Codec) DecodeFlow(raw []byte) (is04.Flow, error) {
	if err := rejectFields(raw, flowV11PlusFields, "flow"); err != nil {
		return is04.Flow{}, err
	}
	var f is04.Flow
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&f); err != nil {
		return is04.Flow{}, fmt.Errorf("is04 v1.0: decode flow: %w", err)
	}
	if err := validateFlowV10(f); err != nil {
		return is04.Flow{}, err
	}
	return f, nil
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

// EncodeSender marshals a Sender for v1.0.3 — strips caps,
// interface_bindings, subscription.
func (Codec) EncodeSender(s is04.Sender) ([]byte, error) {
	if err := validateSenderV10(s); err != nil {
		return nil, err
	}
	return stripFields(s, senderV11PlusFields)
}

// DecodeSender parses a v1.0.3 Sender payload. Rejects v1.1+ keys.
func (Codec) DecodeSender(raw []byte) (is04.Sender, error) {
	if err := rejectFields(raw, senderV11PlusFields, "sender"); err != nil {
		return is04.Sender{}, err
	}
	var s is04.Sender
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&s); err != nil {
		return is04.Sender{}, fmt.Errorf("is04 v1.0: decode sender: %w", err)
	}
	if err := validateSenderV10(s); err != nil {
		return is04.Sender{}, err
	}
	return s, nil
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

// EncodeReceiver marshals a Receiver for v1.0.3 — strips
// `interface_bindings` (v1.2 addition) AND `subscription.active`
// (v1.1 addition). The IS04Utils.downgrade_resource v1.0 path
// removes both, so the SYNC body must too for AMWA test_31's
// byte-equality check.
func (Codec) EncodeReceiver(r is04.Receiver) ([]byte, error) {
	if err := validateReceiverV10(r); err != nil {
		return nil, err
	}
	raw, err := stripFields(r, receiverV11PlusFields)
	if err != nil {
		return nil, err
	}
	return stripNestedKey(raw, []string{"subscription"}, "active")
}

// stripNestedKey for v1.0 — same shape as the v11/v12 helper. Walks
// the nested object path, deletes `key` from the leaf map.
func stripNestedKey(raw []byte, path []string, key string) ([]byte, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	cur := v
	for _, p := range path[:len(path)-1] {
		m, ok := cur.(map[string]any)
		if !ok {
			return raw, nil
		}
		cur = m[p]
	}
	leaf, ok := cur.(map[string]any)
	if !ok {
		return raw, nil
	}
	if leaf2, ok := leaf[path[len(path)-1]].(map[string]any); ok {
		delete(leaf2, key)
	}
	return json.MarshalIndent(v, "", "  ")
}

// DecodeReceiver parses a v1.0.3 Receiver payload. Rejects v1.1+ keys.
func (Codec) DecodeReceiver(raw []byte) (is04.Receiver, error) {
	if err := rejectFields(raw, receiverV11PlusFields, "receiver"); err != nil {
		return is04.Receiver{}, err
	}
	var r is04.Receiver
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&r); err != nil {
		return is04.Receiver{}, fmt.Errorf("is04 v1.0: decode receiver: %w", err)
	}
	if err := validateReceiverV10(r); err != nil {
		return is04.Receiver{}, err
	}
	return r, nil
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

// stripFields marshals v into JSON, drops the named top-level keys,
// and re-marshals with 2-space indent.
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
// carries any of the named top-level keys.
func rejectFields(raw []byte, forbidden []string, kind string) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	var m map[string]json.RawMessage
	if err := d.Decode(&m); err != nil {
		return fmt.Errorf("is04 v1.0: decode %s: %w", kind, err)
	}
	for _, k := range forbidden {
		if _, present := m[k]; present {
			return fmt.Errorf("is04 v1.0: %s.%s: forbidden in v1.0 (introduced in v1.1+)", kind, k)
		}
	}
	return nil
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
