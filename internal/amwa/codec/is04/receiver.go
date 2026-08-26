package is04

import (
	"fmt"

	"dhs/internal/amwa/codec/spec"
)

// Receiver is the IS-04 v1.3 Receiver resource.
//
// JSON Schemas:
//
//	https://specs.amwa.tv/is-04/releases/v1.3.3/APIs/schemas/with-refs/receiver_core.html
//	+ receiver_audio.json / receiver_video.json / receiver_data.json / receiver_mux.json
//
// Format-specific media_type lists land in `caps.media_types[]`.
type Receiver struct {
	ResourceCore

	DeviceID          string               `json:"device_id"`
	Transport         string               `json:"transport"`
	InterfaceBindings []string             `json:"interface_bindings"`
	Format            string               `json:"format"`
	Caps              ReceiverCaps         `json:"caps"`
	Subscription      ReceiverSubscription `json:"subscription"`
}

// ReceiverCaps mirrors the receiver_*.json `caps` object. v1.3 supports
// `media_types` (list of accepted RTP payload types) and per-BCP-004
// `constraint_sets`. The latter lands in N9; we hold the slot here.
type ReceiverCaps struct {
	MediaTypes     []string         `json:"media_types,omitempty"`
	EventTypes     []string         `json:"event_types,omitempty"`
	ConstraintSets []map[string]any `json:"constraint_sets,omitempty"`
	// Version stamps when the constraint_sets / media_types last changed
	// (BCP-004-01 §1.0). Mandatory only when constraint_sets is present;
	// AMWA test_27_2 enforces it.
	Version string `json:"version,omitempty"`
}

// Validate enforces receiver_core + per-format rules.
func (r *Receiver) Validate() error {
	errs := validateCore(&r.ResourceCore, "receiver")

	if r.DeviceID == "" || !IsValidUUID(r.DeviceID) {
		errs = append(errs, fmt.Sprintf("receiver.device_id %q: must match UUID v1-5", r.DeviceID))
	}
	if r.Transport == "" || !IsValidTransportURN(r.Transport) {
		errs = append(errs, fmt.Sprintf("receiver.transport %q: must be a known NMOS transport URN or non-NMOS URI", r.Transport))
	}
	// interface_bindings landed in IS-04 v1.2 — accept its absence on
	// v1.0/v1.1 wire shapes.
	if r.Format == "" || !IsValidFormatURN(r.Format) {
		errs = append(errs, fmt.Sprintf("receiver.format %q: required, must be NMOS format URN or non-NMOS URI", r.Format))
	}
	if r.Subscription.SenderID != nil && *r.Subscription.SenderID != "" && !IsValidUUID(*r.Subscription.SenderID) {
		errs = append(errs, fmt.Sprintf("receiver.subscription.sender_id %q: must match UUID or be null", *r.Subscription.SenderID))
	}

	return joinErrs("is04 receiver validation failed", errs)
}

// DecodeReceiver parses + validates a Receiver payload.
func DecodeReceiver(raw []byte) (*Receiver, error) {
	return DecodeReceiverReporting(raw, APIVersion, nil)
}

// DecodeReceiverReporting parses a Receiver payload and validates it against the
// canonical rules, which track the latest IS-04 minor.
//
// A per-minor codec wants [ParseReceiver] instead: a v1.0 payload judged by
// v1.3 rules is failed for missing fields that minor never had.
func DecodeReceiverReporting(raw []byte, apiVer string, rep spec.Reporter) (*Receiver, error) {
	r, err := ParseReceiver(raw, apiVer, rep)
	if err != nil {
		return nil, err
	}
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return r, nil
}

// ParseReceiver decodes a Receiver served on an apiVer tree WITHOUT applying any
// minor's validation rules. Two classes of deviation are absorbed and
// reported rather than raised: a field IS-04 defines nowhere (see
// absorb.go) and a field it did not define until after apiVer (see
// [Since]). The caller then validates against the minor it asked for.
func ParseReceiver(raw []byte, apiVer string, rep spec.Reporter) (*Receiver, error) {
	var r Receiver
	if err := decodeAbsorbing(raw, &r, "receiver", apiVer, rep); err != nil {
		return nil, err
	}
	AbsorbLaterThan(raw, "receiver", apiVer, rep)
	return &r, nil
}
