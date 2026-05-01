package is04

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Sender is the IS-04 v1.3 Sender resource.
//
// JSON Schema: https://specs.amwa.tv/is-04/releases/v1.3.3/APIs/schemas/with-refs/sender.html
type Sender struct {
	ResourceCore

	FlowID            *string            `json:"flow_id"` // UUID OR null
	Transport         string             `json:"transport"`
	DeviceID          string             `json:"device_id"`
	ManifestHref      *string            `json:"manifest_href"` // URI OR null
	InterfaceBindings []string           `json:"interface_bindings"`
	Caps              map[string]any     `json:"caps,omitempty"`
	Subscription      SenderSubscription `json:"subscription"`
}

// SenderSubscription mirrors sender.json `subscription`. Spec
// (sender.json §required) lists both `receiver_id` and `active` as
// required; `receiver_id` type is `["string","null"]` so the field
// MUST be on the wire even when null — hence no `omitempty` on the
// pointer (Go drops a nil-pointer field tagged `omitempty` entirely).
//
// AMWA-approved registries (Cerebrum, etc.) silently compensate for
// missing receiver_id, but strict IS-04 conformance requires it.
type SenderSubscription struct {
	ReceiverID *string `json:"receiver_id"`
	Active     bool    `json:"active"`
}

// ReceiverSubscription mirrors receiver_*.json `subscription`. Spec
// requires both `sender_id` and `active`; `sender_id` type is
// `["string","null"]` so the field MUST be on the wire when null.
type ReceiverSubscription struct {
	SenderID *string `json:"sender_id"`
	Active   bool    `json:"active"`
}

// Validate enforces sender.json rules.
func (s *Sender) Validate() error {
	errs := validateCore(&s.ResourceCore, "sender")

	if s.FlowID != nil && *s.FlowID != "" && !IsValidUUID(*s.FlowID) {
		errs = append(errs, fmt.Sprintf("sender.flow_id %q: must match UUID v1-5 or be null", *s.FlowID))
	}
	if s.Transport == "" || !IsValidTransportURN(s.Transport) {
		errs = append(errs, fmt.Sprintf("sender.transport %q: must be a known NMOS transport URN or non-NMOS URI", s.Transport))
	}
	if s.DeviceID == "" || !IsValidUUID(s.DeviceID) {
		errs = append(errs, fmt.Sprintf("sender.device_id %q: must match UUID v1-5", s.DeviceID))
	}
	if s.InterfaceBindings == nil {
		errs = append(errs, "sender.interface_bindings: required (may be empty array)")
	}
	// manifest_href is required (string OR null) — the field must be
	// present in the JSON. Nil pointer = absent; empty string = invalid.
	if s.ManifestHref != nil && *s.ManifestHref == "" {
		errs = append(errs, "sender.manifest_href: empty string disallowed (use null when manifest absent)")
	}

	if s.Subscription.ReceiverID != nil && *s.Subscription.ReceiverID != "" && !IsValidUUID(*s.Subscription.ReceiverID) {
		errs = append(errs, fmt.Sprintf("sender.subscription.receiver_id %q: must match UUID or be null", *s.Subscription.ReceiverID))
	}

	return joinErrs("is04 sender validation failed", errs)
}

// DecodeSender parses + validates a Sender payload.
func DecodeSender(raw []byte) (*Sender, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	var s Sender
	if err := d.Decode(&s); err != nil {
		return nil, fmt.Errorf("is04: decode sender: %w", err)
	}
	if d.More() {
		return nil, fmt.Errorf("is04: decode sender: trailing JSON content")
	}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return &s, nil
}
