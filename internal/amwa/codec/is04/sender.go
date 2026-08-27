package is04

import (
	"fmt"

	"dhs/internal/amwa/codec/spec"
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

// SenderSubscription mirrors sender.json `subscription`. Per IS-04
// v1.3 sender.json the field is REQUIRED with type ["string","null"]
// — it MUST appear on the wire even when there's no consumer (then
// `null`). No omitempty.
type SenderSubscription struct {
	ReceiverID *string `json:"receiver_id"`
	Active     bool    `json:"active"`
}

// ReceiverSubscription mirrors receiver_core.json `subscription`.
// Same required-but-nullable rule as SenderSubscription.
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
	// interface_bindings landed in IS-04 v1.2 — when present, it must
	// be an array of MAC strings; when absent (a v1.0/v1.1 wire shape)
	// we accept the field's omission silently.
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
	return DecodeSenderReporting(raw, APIVersion, nil)
}

// DecodeSenderReporting parses a Sender payload and validates it against the
// canonical rules, which track the latest IS-04 minor.
//
// A per-minor codec wants [ParseSender] instead: a v1.0 payload judged by
// v1.3 rules is failed for missing fields that minor never had.
func DecodeSenderReporting(raw []byte, apiVer string, rep spec.Reporter) (*Sender, error) {
	s, err := ParseSender(raw, apiVer, rep)
	if err != nil {
		return nil, err
	}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return s, nil
}

// ParseSender decodes a Sender served on an apiVer tree WITHOUT applying any
// minor's validation rules. Two classes of deviation are absorbed and
// reported rather than raised: a field IS-04 defines nowhere (see
// absorb.go) and a field it did not define until after apiVer (see
// [Since]). The caller then validates against the minor it asked for.
func ParseSender(raw []byte, apiVer string, rep spec.Reporter) (*Sender, error) {
	var s Sender
	if err := decodeAbsorbing(raw, &s, "sender", apiVer, rep); err != nil {
		return nil, err
	}
	return &s, nil
}
