// Package v12 is the AMWA NMOS IS-04 v1.2.2 wire codec — one of four
// minors required by `internal/amwa/CLAUDE.md` "Versioning". It is a
// thin Strategy implementation of [is04.Codec] that wraps the
// canonical struct types in is04/ and post-processes JSON encode /
// decode to gate fields per the v1.2.2 spec text.
//
// Schema diffs vs v1.3.3 (computed from the AMWA schema bundle in
// internal/amwa/codec/is04/testdata/schemas/, verified live against
// the AMWA NMOS Testing tool 2026-05-02 — closes #191):
//
//   - Node:     v1.3 added `interfaces[].attached_network_device`.
//               v1.2 wire MUST NOT carry it. Strip on encode, reject
//               on decode.
//   - Device:   identical to v1.3.3 (top level).
//   - Source:   identical top-level (polymorphic oneOf on format).
//   - Flow:     identical top-level (polymorphic oneOf on format).
//   - Sender:   identical to v1.3.3 (caps + interface_bindings +
//               subscription added in v1.2 — present in both).
//   - Receiver: v1.3 added `caps.constraint_sets[]` and
//               `caps.version` (BCP-004-01 receiver capabilities).
//               v1.2 wire MUST NOT carry either. Strip on encode,
//               reject on decode.
//
// Implementation: Encode methods deep-copy the canonical struct, nil
// out v1.3-only fields, then marshal. Decode methods pre-check the
// raw JSON has none of the v1.3-only nested keys before delegating
// to the canonical decoder.
package v12

import (
	"acp/internal/amwa/codec/is04"
	"bytes"
	"encoding/json"
	"fmt"
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

// EncodeNode marshals a Node for v1.2.2 — strips
// `interfaces[].attached_network_device` (added v1.3) and the
// v1.3-only `authorization` flag from services + api.endpoints
// (IS-10 added it in v1.3).
func (Codec) EncodeNode(n is04.Node) ([]byte, error) {
	if err := n.Validate(); err != nil {
		return nil, err
	}
	cp := n
	if len(cp.Interfaces) > 0 {
		cp.Interfaces = make([]is04.NodeIface, len(n.Interfaces))
		for i, iface := range n.Interfaces {
			cp.Interfaces[i] = iface
			cp.Interfaces[i].AttachedNetworkDevice = nil
		}
	}
	raw, err := marshalIndent(cp)
	if err != nil {
		return nil, err
	}
	raw, err = stripNestedKey(raw, []string{"services"}, "authorization")
	if err != nil {
		return nil, err
	}
	return stripNestedKey(raw, []string{"api", "endpoints"}, "authorization")
}

// DecodeNode parses a v1.2.2 Node payload. Rejects v1.3-only nested
// fields under interfaces[].
func (Codec) DecodeNode(raw []byte) (is04.Node, error) {
	if err := rejectNodeV13Nested(raw); err != nil {
		return is04.Node{}, err
	}
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

// EncodeDevice marshals a Device for v1.2.2 — strips the v1.3-only
// `controls[].authorization` flag (IS-10 added it in v1.3).
func (Codec) EncodeDevice(d is04.Device) ([]byte, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	raw, err := marshalIndent(d)
	if err != nil {
		return nil, err
	}
	return stripNestedKey(raw, []string{"controls"}, "authorization")
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

// EncodeReceiver marshals a Receiver for v1.2.2 — strips BCP-004-01
// receiver-capabilities fields `caps.constraint_sets[]` and
// `caps.version` (added v1.3).
func (Codec) EncodeReceiver(r is04.Receiver) ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	cp := r
	cp.Caps.ConstraintSets = nil
	cp.Caps.Version = ""
	return marshalIndent(cp)
}

// DecodeReceiver parses a v1.2.2 Receiver payload. Rejects
// `caps.constraint_sets` and `caps.version` (v1.3-only).
func (Codec) DecodeReceiver(raw []byte) (is04.Receiver, error) {
	if err := rejectReceiverV13Caps(raw); err != nil {
		return is04.Receiver{}, err
	}
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

// rejectNodeV13Nested fails decode if any element of the raw Node JSON's
// `interfaces[]` array carries `attached_network_device` (added v1.3).
func rejectNodeV13Nested(raw []byte) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	var m map[string]json.RawMessage
	if err := d.Decode(&m); err != nil {
		return fmt.Errorf("is04 v1.2: decode node: %w", err)
	}
	ifacesRaw, present := m["interfaces"]
	if !present {
		return nil
	}
	var ifaces []map[string]json.RawMessage
	if err := json.Unmarshal(ifacesRaw, &ifaces); err != nil {
		return nil // let canonical decode handle the error
	}
	for i, iface := range ifaces {
		if _, has := iface["attached_network_device"]; has {
			return fmt.Errorf("is04 v1.2: node.interfaces[%d].attached_network_device: forbidden in v1.2 (introduced in v1.3)", i)
		}
	}
	return nil
}

// rejectReceiverV13Caps fails decode if the raw Receiver JSON's
// `caps` object carries `constraint_sets` or `version` (added v1.3).
func rejectReceiverV13Caps(raw []byte) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	var m map[string]json.RawMessage
	if err := d.Decode(&m); err != nil {
		return fmt.Errorf("is04 v1.2: decode receiver: %w", err)
	}
	capsRaw, present := m["caps"]
	if !present {
		return nil
	}
	var caps map[string]json.RawMessage
	if err := json.Unmarshal(capsRaw, &caps); err != nil {
		return nil // let canonical decode handle the error
	}
	for _, k := range []string{"constraint_sets", "version"} {
		if _, has := caps[k]; has {
			return fmt.Errorf("is04 v1.2: receiver.caps.%s: forbidden in v1.2 (introduced in v1.3)", k)
		}
	}
	return nil
}

// marshalIndent lives in helpers.go.

func init() {
	is04.Register(Codec{})
}
