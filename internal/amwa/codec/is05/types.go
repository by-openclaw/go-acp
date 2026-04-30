package is05

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"time"
)

// ActivationMode is the fixed-set discriminator on the activation
// object. Spec: §4.2 PATCH /staged → activation.
//
//   ""                              — no activation requested
//   activate_immediate              — apply on receipt
//   activate_scheduled_relative     — apply N seconds after receipt
//   activate_scheduled_absolute     — apply at TAI timestamp
type ActivationMode string

// Recognised activation modes per the spec. Empty is a valid value
// (no activation in this PATCH).
const (
	ActivationModeNone               ActivationMode = ""
	ActivationModeImmediate          ActivationMode = "activate_immediate"
	ActivationModeScheduledRelative  ActivationMode = "activate_scheduled_relative"
	ActivationModeScheduledAbsolute  ActivationMode = "activate_scheduled_absolute"
)

// IsValidActivationMode is true when m is one of the four spec values.
func IsValidActivationMode(m ActivationMode) bool {
	switch m {
	case ActivationModeNone,
		ActivationModeImmediate,
		ActivationModeScheduledRelative,
		ActivationModeScheduledAbsolute:
		return true
	}
	return false
}

// Activation is the activation sub-object on staged Sender / Receiver.
// All fields nullable per spec — empty Mode means "no activation now".
//
// `requested_time` is TAI seconds:nanoseconds for the
// activate_scheduled_* modes; null for activate_immediate.
// `activation_time` is set BY THE SERVER on the response; clients
// always send null.
type Activation struct {
	Mode           ActivationMode `json:"mode"`
	RequestedTime  *string        `json:"requested_time"`
	ActivationTime *string        `json:"activation_time"`
}

// TransportParams is the per-leg array of transport-specific
// parameters. RTP carries source_ip / destination_ip / source_port /
// destination_port etc.; WebSocket carries connection_uri etc.
// We hold this as the raw map to keep the codec polymorphic — every
// transport URN gets its own validation in [validateTransportParams].
type TransportParams = map[string]any

// MasterEnable is shared by Sender + Receiver staged. Boolean.
// Without it set true, nothing happens even with a valid target.
type MasterEnableField struct {
	MasterEnable bool `json:"master_enable"`
}

// StagedSender is the body of GET / PATCH /single/senders/{id}/staged.
//
// Spec: https://specs.amwa.tv/is-05/branches/v1.1/APIs/schemas/with-refs/sender-stage-schema.html
type StagedSender struct {
	MasterEnableField

	// Receiver ID currently consuming this sender's flow, or null.
	ReceiverID *string `json:"receiver_id"`

	// Activation hint — what the server will do when this PATCH
	// commits. May be omitted (no-activation PATCH).
	Activation Activation `json:"activation"`

	// Per-leg transport parameters. RTP-style senders carry one
	// element per leg (single-leg unicast or two-leg ST 2022-7).
	TransportParams []TransportParams `json:"transport_params"`

	// Transport file payload (SDP) — only meaningful for RTP-based
	// senders. nil pointer = field absent on wire.
	TransportFile *TransportFile `json:"transport_file,omitempty"`
}

// TransportFile is the inline transport file representation used in
// PATCH bodies (vs the separate /transportfile GET endpoint).
type TransportFile struct {
	Type string `json:"type"` // "application/sdp" for RTP
	Data string `json:"data"` // SDP body, or empty when null
}

// StagedReceiver is the body of GET / PATCH
// /single/receivers/{id}/staged.
type StagedReceiver struct {
	MasterEnableField

	// Sender ID currently feeding this receiver, or null.
	SenderID *string `json:"sender_id"`

	Activation Activation `json:"activation"`

	TransportParams []TransportParams `json:"transport_params"`

	// SDP-style transport file the controller feeds into the
	// receiver — same shape as StagedSender.TransportFile.
	TransportFile *TransportFile `json:"transport_file,omitempty"`
}

// ActiveSender is the read-only mirror of currently-running sender
// state — same shape as StagedSender, but the activation_time field
// on Activation is now populated by the server.
type ActiveSender = StagedSender

// ActiveReceiver mirrors StagedReceiver post-activation.
type ActiveReceiver = StagedReceiver

// taiPattern matches `<sec>:<nsec>` per IS-04 / IS-05.
var taiPattern = regexp.MustCompile(`^[0-9]+:[0-9]+$`)

// ValidateActivation enforces the per-mode rules:
//
//   activate_scheduled_relative — requested_time MUST be set, in TAI.
//   activate_scheduled_absolute — same.
//   activate_immediate          — requested_time MUST be null.
//   ""                          — every field MUST be null.
func ValidateActivation(a Activation) error {
	if !IsValidActivationMode(a.Mode) {
		return fmt.Errorf("is05: activation.mode %q: invalid", a.Mode)
	}
	switch a.Mode {
	case ActivationModeNone:
		if a.RequestedTime != nil {
			return fmt.Errorf("is05: activation.requested_time: must be null when mode is empty")
		}
	case ActivationModeImmediate:
		if a.RequestedTime != nil {
			return fmt.Errorf("is05: activation.requested_time: must be null for activate_immediate")
		}
	case ActivationModeScheduledRelative, ActivationModeScheduledAbsolute:
		if a.RequestedTime == nil || *a.RequestedTime == "" {
			return fmt.Errorf("is05: activation.requested_time: required for %s", a.Mode)
		}
		if !taiPattern.MatchString(*a.RequestedTime) {
			return fmt.Errorf("is05: activation.requested_time %q: must match `<sec>:<nsec>` TAI form",
				*a.RequestedTime)
		}
	}
	return nil
}

// ValidateStagedSender enforces required fields + activation rules.
// transport_params SHAPE depends on transport URN — validated by the
// controller against IS-04 sender.transport.
func ValidateStagedSender(s StagedSender) error {
	if err := ValidateActivation(s.Activation); err != nil {
		return err
	}
	if s.TransportParams == nil {
		return fmt.Errorf("is05: staged.sender.transport_params: required (may be empty array)")
	}
	if s.TransportFile != nil && s.TransportFile.Type == "" {
		return fmt.Errorf("is05: staged.sender.transport_file.type: required when transport_file present")
	}
	return nil
}

// ValidateStagedReceiver mirrors ValidateStagedSender.
func ValidateStagedReceiver(r StagedReceiver) error {
	if err := ValidateActivation(r.Activation); err != nil {
		return err
	}
	if r.TransportParams == nil {
		return fmt.Errorf("is05: staged.receiver.transport_params: required (may be empty array)")
	}
	return nil
}

// EncodeStagedSender marshals with validation.
func EncodeStagedSender(s StagedSender) ([]byte, error) {
	if err := ValidateStagedSender(s); err != nil {
		return nil, err
	}
	return json.MarshalIndent(s, "", "  ")
}

// DecodeStagedSender parses + validates a Staged Sender body.
func DecodeStagedSender(raw []byte) (*StagedSender, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	var s StagedSender
	if err := d.Decode(&s); err != nil {
		return nil, fmt.Errorf("is05: decode staged sender: %w", err)
	}
	if d.More() {
		return nil, fmt.Errorf("is05: decode staged sender: trailing JSON")
	}
	if err := ValidateStagedSender(s); err != nil {
		return nil, err
	}
	return &s, nil
}

// EncodeStagedReceiver marshals with validation.
func EncodeStagedReceiver(r StagedReceiver) ([]byte, error) {
	if err := ValidateStagedReceiver(r); err != nil {
		return nil, err
	}
	return json.MarshalIndent(r, "", "  ")
}

// DecodeStagedReceiver parses + validates a Staged Receiver body.
func DecodeStagedReceiver(raw []byte) (*StagedReceiver, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	var r StagedReceiver
	if err := d.Decode(&r); err != nil {
		return nil, fmt.Errorf("is05: decode staged receiver: %w", err)
	}
	if d.More() {
		return nil, fmt.Errorf("is05: decode staged receiver: trailing JSON")
	}
	if err := ValidateStagedReceiver(r); err != nil {
		return nil, err
	}
	return &r, nil
}

// FormatTAINow renders a `<sec>:<nsec>` TAI string from a wall-clock
// time.Time. Note: this is approximate (uses Unix epoch + Go
// monotonic) — production code that needs strict TAI must subtract
// the leap-second offset (~37s in 2026); see IETF NTP sources.
func FormatTAINow(t time.Time) string {
	return fmt.Sprintf("%d:%d", t.Unix(), t.Nanosecond())
}
