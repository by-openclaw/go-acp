// Package is11 — canonical types for AMWA IS-11 Stream Compatibility
// Management v1.0 (https://specs.amwa.tv/is-11/releases/v1.0.0/).
//
// IS-11 extends the IS-04 model with two resources and one mechanism:
//
//   - Input:  a physical interface consuming a signal from an upstream
//     unit (e.g. an SDI or HDMI input), associated with Senders;
//   - Output: a physical interface producing a signal for a downstream
//     unit, associated with Receivers;
//   - Active Constraints: a controller PUTs Constraint Sets (BCP-004-01
//     capability URNs) onto a Sender, and the device restricts what it
//     transmits until they are deleted.
//
// Validation doctrine is the repo-wide one: AMWA's own JSON Schemas
// (schemas/v1.0.0/, verbatim) are the authority; Go code carries the
// canonical shapes and only enforces structure the schemas fix.
package is11

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// ResourceCore mirrors IS-11's resource_core.json — the same identity
// block every NMOS resource carries.
type ResourceCore struct {
	ID          string              `json:"id"`
	Version     string              `json:"version"`
	Label       string              `json:"label"`
	Description string              `json:"description"`
	Tags        map[string][]string `json:"tags"`
}

// StatusState strings, per the three status schemas. Each resource
// kind has its own enum; they are kept as one string type with
// per-kind validators so callers cannot cross-assign silently.
const (
	// Input states (input.json).
	InputNoSignal       = "no_signal"
	InputAwaitingSignal = "awaiting_signal"
	InputSignalPresent  = "signal_present"
	// Output states (output.json).
	OutputNoSignal      = "no_signal"
	OutputDefaultSignal = "default_signal"
	OutputSignalPresent = "signal_present"
	// Sender states (sender-status.json).
	SenderUnconstrained        = "unconstrained"
	SenderConstrained          = "constrained"
	SenderConstraintsViolation = "active_constraints_violation"
	SenderNoEssence            = "no_essence"
	SenderAwaitingEssence      = "awaiting_essence"
	// Receiver states (receiver-status.json).
	ReceiverUnknown            = "unknown"
	ReceiverCompliantStream    = "compliant_stream"
	ReceiverNonCompliantStream = "non_compliant_stream"
)

// Status is the {state, debug} block shared by every IS-11 status
// surface. Debug is optional per every schema.
type Status struct {
	State string `json:"state"`
	Debug string `json:"debug,omitempty"`
}

// Input is the /inputs/{id}/properties resource (input.json).
type Input struct {
	ResourceCore

	// AdjustToCaps is OPTIONAL-BY-EXISTENCE: the property is present
	// only when the Input supports adjusting Base EDID to internal
	// capabilities. A pointer keeps absent distinct from false.
	AdjustToCaps *bool `json:"adjust_to_caps,omitempty"`

	BaseEDIDSupport bool   `json:"base_edid_support"`
	Connected       bool   `json:"connected"`
	EDIDSupport     bool   `json:"edid_support"`
	Status          Status `json:"status"`
	DeviceID        string `json:"device_id"`
}

// Output is the /outputs/{id}/properties resource (output.json).
type Output struct {
	ResourceCore

	Connected   bool   `json:"connected"`
	EDIDSupport bool   `json:"edid_support"`
	Status      Status `json:"status"`
	DeviceID    string `json:"device_id"`
}

// ConstraintSet is one BCP-004-01-style constraint set: capability
// URNs mapped to parameter constraints, plus the three meta keys
// (label / preference / enabled). Held as a raw map because the
// parameter-constraint value shapes are polymorphic per URN
// (param_constraint_{boolean,integer,number,rational,string}.json) —
// the same posture TransportParams takes in is05.
type ConstraintSet map[string]any

// ActiveConstraints is the body of
// /senders/{id}/constraints/active (constraints_active.json).
type ActiveConstraints struct {
	ConstraintSets []ConstraintSet `json:"constraint_sets"`
}

// SupportedConstraints is the body of
// /senders/{id}/constraints/supported (constraints_supported.json):
// the parameter-constraint URNs this Sender understands.
type SupportedConstraints struct {
	ParameterConstraints []string `json:"parameter_constraints"`
}

// metaPrefix marks the three meta keys a ConstraintSet may carry.
const (
	capMetaPrefix = "urn:x-nmos:cap:meta:"
	capPrefix     = "urn:x-nmos:cap:"
)

// ValidateStatus checks a status block against one resource kind's
// enum. kind is "input", "output", "sender" or "receiver".
func ValidateStatus(kind string, s Status) error {
	var ok bool
	switch kind {
	case "input":
		ok = s.State == InputNoSignal || s.State == InputAwaitingSignal || s.State == InputSignalPresent
	case "output":
		ok = s.State == OutputNoSignal || s.State == OutputDefaultSignal || s.State == OutputSignalPresent
	case "sender":
		ok = s.State == SenderUnconstrained || s.State == SenderConstrained ||
			s.State == SenderConstraintsViolation || s.State == SenderNoEssence || s.State == SenderAwaitingEssence
	case "receiver":
		ok = s.State == ReceiverUnknown || s.State == ReceiverCompliantStream || s.State == ReceiverNonCompliantStream
	default:
		return fmt.Errorf("is11: unknown status kind %q", kind)
	}
	if !ok {
		return fmt.Errorf("is11: %s status state %q: not a member of the schema enum", kind, s.State)
	}
	return nil
}

// ValidateConstraintSet enforces what constraint_set.json fixes:
// at least one property, and every non-meta key must be a capability
// URN ("urn:x-nmos:cap:" but not the meta namespace). Parameter value
// shapes stay with the schema validator.
func ValidateConstraintSet(cs ConstraintSet) error {
	if len(cs) == 0 {
		return fmt.Errorf("is11: constraint set must carry at least one property")
	}
	for k := range cs {
		if len(k) >= len(capMetaPrefix) && k[:len(capMetaPrefix)] == capMetaPrefix {
			continue
		}
		if len(k) < len(capPrefix) || k[:len(capPrefix)] != capPrefix {
			return fmt.Errorf("is11: constraint set key %q: not a urn:x-nmos:cap: URN", k)
		}
	}
	return nil
}

// ValidateActiveConstraints checks the constraint_sets array exists
// (empty = unconstrained, which is legal) and each member validates.
func ValidateActiveConstraints(a ActiveConstraints) error {
	if a.ConstraintSets == nil {
		return fmt.Errorf("is11: active constraints: constraint_sets is required (may be empty)")
	}
	for i, cs := range a.ConstraintSets {
		if err := ValidateConstraintSet(cs); err != nil {
			return fmt.Errorf("is11: constraint_sets[%d]: %w", i, err)
		}
	}
	return nil
}

// ---- Encode / Decode (canonical, strict) ----

func encodeJSON(v any) ([]byte, error) { return json.MarshalIndent(v, "", "  ") }

func decodeStrict(raw []byte, dst any, what string) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return fmt.Errorf("is11: decode %s: %w", what, err)
	}
	if d.More() {
		return fmt.Errorf("is11: decode %s: trailing JSON", what)
	}
	return nil
}

// EncodeInput marshals with validation.
func EncodeInput(in Input) ([]byte, error) {
	if err := ValidateStatus("input", in.Status); err != nil {
		return nil, err
	}
	return encodeJSON(in)
}

// DecodeInput parses + validates.
func DecodeInput(raw []byte) (Input, error) {
	var in Input
	if err := decodeStrict(raw, &in, "input"); err != nil {
		return Input{}, err
	}
	if err := ValidateStatus("input", in.Status); err != nil {
		return Input{}, err
	}
	return in, nil
}

// EncodeOutput marshals with validation.
func EncodeOutput(out Output) ([]byte, error) {
	if err := ValidateStatus("output", out.Status); err != nil {
		return nil, err
	}
	return encodeJSON(out)
}

// DecodeOutput parses + validates.
func DecodeOutput(raw []byte) (Output, error) {
	var out Output
	if err := decodeStrict(raw, &out, "output"); err != nil {
		return Output{}, err
	}
	if err := ValidateStatus("output", out.Status); err != nil {
		return Output{}, err
	}
	return out, nil
}

// EncodeActiveConstraints marshals with validation.
func EncodeActiveConstraints(a ActiveConstraints) ([]byte, error) {
	if err := ValidateActiveConstraints(a); err != nil {
		return nil, err
	}
	return encodeJSON(a)
}

// DecodeActiveConstraints parses + validates a PUT body.
func DecodeActiveConstraints(raw []byte) (ActiveConstraints, error) {
	var a ActiveConstraints
	if err := decodeStrict(raw, &a, "active constraints"); err != nil {
		return ActiveConstraints{}, err
	}
	if err := ValidateActiveConstraints(a); err != nil {
		return ActiveConstraints{}, err
	}
	return a, nil
}
