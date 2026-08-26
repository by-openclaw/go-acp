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

// MarshalJSON writes an unset mode as JSON null, never as "".
//
// Every IS-05 activation schema types `mode` as an ENUM that is
// nullable: null, activate_immediate, activate_scheduled_relative,
// activate_scheduled_absolute. The empty string is not a member, so a
// Go zero value serialised literally makes every staged and active
// response fail schema validation -- a wide failure with a narrow
// cause, because the activation block sits inside almost every other
// response this API returns.
func (a Activation) MarshalJSON() ([]byte, error) {
	// A named alias, so the nested Marshal does not recurse back into
	// this method.
	type plain Activation
	if a.Mode == "" {
		return json.Marshal(struct {
			Mode any `json:"mode"`
			plain
		}{Mode: nil, plain: plain(a)})
	}
	return json.Marshal(plain(a))
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
	// Both members are typed ["string","null"] by the schema, so both
	// are pointers. An unset transport file is
	// {"type":null,"data":null} -- NOT an absent object, and not a
	// pair of empty strings; neither of those validates.
	Type *string `json:"type"` // "application/sdp" for RTP
	Data *string `json:"data"` // SDP body
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
	// No omitempty: receiver-stage-schema lists transport_file as
	// REQUIRED (nullable), while sender-stage-schema does not allow it
	// at all under additionalProperties:false. The asymmetry is the
	// spec's, and it is why the two views cannot share one tag.
	TransportFile *TransportFile `json:"transport_file"`
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
	if s.TransportFile != nil && s.TransportFile.Data != nil && *s.TransportFile.Data != "" &&
		(s.TransportFile.Type == nil || *s.TransportFile.Type == "") {
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

// TAILeapSeconds is TAI − UTC: the count of leap seconds inserted
// since the two scales were aligned in 1972.
//
// 37 since 2017-01-01, and unchanged since — the IERS has announced no
// leap second in the years around this table, and the 2022 CGPM
// resolution is to stop inserting them by 2035. A constant is
// therefore honest for the deployment window and a table would be
// pretending to a precision nothing here uses.
//
// This offset is NOT cosmetic. IS-05 §4.2 types every timestamp as
// TAI, so a controller scheduling an absolute activation sends a TAI
// instant. Reading it as Unix seconds puts the switch 37 seconds into
// the future, which looks like a device that simply never activates —
// AMWA IS-05-01 test_29/test_30 give up after three retries.
const TAILeapSeconds = 37

// FormatTAINow renders a wall-clock time as a `<sec>:<nsec>` TAI
// string.
func FormatTAINow(t time.Time) string {
	return fmt.Sprintf("%d:%d", t.Unix()+TAILeapSeconds, t.Nanosecond())
}

// TAIToTime converts a TAI `<sec>:<nsec>` instant to wall-clock time.
// The inverse of FormatTAINow.
func TAIToTime(sec, nsec int64) time.Time {
	return time.Unix(sec-TAILeapSeconds, nsec)
}
