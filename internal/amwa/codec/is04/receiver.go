package is04

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Receiver is the IS-04 v1.3 Receiver resource.
//
// JSON Schemas:
//   https://specs.amwa.tv/is-04/releases/v1.3.3/APIs/schemas/with-refs/receiver_core.html
//   + receiver_audio.json / receiver_video.json / receiver_data.json / receiver_mux.json
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
	if r.InterfaceBindings == nil {
		errs = append(errs, "receiver.interface_bindings: required (may be empty array)")
	}
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
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	var r Receiver
	if err := d.Decode(&r); err != nil {
		return nil, fmt.Errorf("is04: decode receiver: %w", err)
	}
	if d.More() {
		return nil, fmt.Errorf("is04: decode receiver: trailing JSON content")
	}
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return &r, nil
}
