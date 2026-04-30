package is08

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
)

// ValidateActivation enforces the request-side activation rules.
//
//	activate_immediate           — requested_time MUST be null/absent.
//	activate_scheduled_relative  — requested_time MUST be set, TAI grammar.
//	activate_scheduled_absolute  — same.
func ValidateActivation(a Activation) error {
	if !IsValidActivationMode(a.Mode) {
		return fmt.Errorf("is08: activation.mode %q: invalid", a.Mode)
	}
	switch a.Mode {
	case ActivationModeImmediate:
		if a.RequestedTime != nil {
			return fmt.Errorf("is08: activation.requested_time: must be null for activate_immediate")
		}
	case ActivationModeScheduledRelative, ActivationModeScheduledAbsolute:
		if a.RequestedTime == nil || *a.RequestedTime == "" {
			return fmt.Errorf("is08: activation.requested_time: required for %s", a.Mode)
		}
		if !taiPattern.MatchString(*a.RequestedTime) {
			return fmt.Errorf("is08: activation.requested_time %q: must match `<sec>:<nsec>` TAI form",
				*a.RequestedTime)
		}
	}
	return nil
}

// ValidateActivationResponse enforces the response-side rules — every
// field nullable + every non-null timestamp must be TAI-valid + mode
// must be a recognised value when non-nil.
func ValidateActivationResponse(a ActivationResponse) error {
	if a.Mode != nil && !IsValidActivationMode(*a.Mode) {
		return fmt.Errorf("is08: activation.mode %q: invalid", *a.Mode)
	}
	if a.RequestedTime != nil && *a.RequestedTime != "" && !taiPattern.MatchString(*a.RequestedTime) {
		return fmt.Errorf("is08: activation.requested_time %q: must match `<sec>:<nsec>` TAI form",
			*a.RequestedTime)
	}
	if a.ActivationTime != nil && *a.ActivationTime != "" && !taiPattern.MatchString(*a.ActivationTime) {
		return fmt.Errorf("is08: activation.activation_time %q: must match `<sec>:<nsec>` TAI form",
			*a.ActivationTime)
	}
	return nil
}

// ValidateMapEntries enforces the patternProperties rules on the
// outer (output id) and inner (channel index string) keys + the
// per-entry consistency invariant: input + channel_index are either
// both nil (unrouted) or both non-nil (routed).
func ValidateMapEntries(m MapEntries) error {
	if m == nil {
		return fmt.Errorf("is08: map: required (may be empty object)")
	}
	for outID, channels := range m {
		if !idPattern.MatchString(outID) {
			return fmt.Errorf("is08: map: output id %q: must match [a-zA-Z0-9-_]+", outID)
		}
		for ch, e := range channels {
			if !chanPattern.MatchString(ch) {
				return fmt.Errorf("is08: map[%s]: channel index key %q: must be a non-negative integer",
					outID, ch)
			}
			if _, err := strconv.Atoi(ch); err != nil {
				return fmt.Errorf("is08: map[%s][%s]: channel index parse: %w", outID, ch, err)
			}
			if (e.Input == nil) != (e.ChannelIndex == nil) {
				return fmt.Errorf("is08: map[%s][%s]: input and channel_index must be both null or both set",
					outID, ch)
			}
			if e.Input != nil && !idPattern.MatchString(*e.Input) {
				return fmt.Errorf("is08: map[%s][%s].input %q: must match [a-zA-Z0-9-_]+",
					outID, ch, *e.Input)
			}
			if e.ChannelIndex != nil && *e.ChannelIndex < 0 {
				return fmt.Errorf("is08: map[%s][%s].channel_index %d: must be >= 0",
					outID, ch, *e.ChannelIndex)
			}
		}
	}
	return nil
}

// ValidateMapActive validates a /map/active body — response-shaped
// activation + map.
func ValidateMapActive(m MapActive) error {
	if err := ValidateActivationResponse(m.Activation); err != nil {
		return err
	}
	return ValidateMapEntries(m.Map)
}

// ValidateMapActivationRequest validates a POST /map/activations body.
func ValidateMapActivationRequest(r MapActivationRequest) error {
	if err := ValidateActivation(r.Activation); err != nil {
		return err
	}
	return ValidateMapEntries(r.Action)
}

// ValidateMapActivationResponse validates a GET /map/activations
// response entry.
func ValidateMapActivationResponse(r MapActivationResponse) error {
	if err := ValidateActivationResponse(r.Activation); err != nil {
		return err
	}
	return ValidateMapEntries(r.Action)
}

// ValidateInputCaps validates a /inputs/{id}/caps body.
func ValidateInputCaps(c InputCaps) error {
	if c.BlockSize < 1 {
		return fmt.Errorf("is08: input.caps.block_size %d: must be >= 1", c.BlockSize)
	}
	return nil
}

// ValidateOutputCaps validates a /outputs/{id}/caps body. nil is OK
// (unrestricted); non-nil entries follow the id grammar (with
// explicit-null permitted).
func ValidateOutputCaps(c OutputCaps) error {
	for i, in := range c.RoutableInputs {
		if in == nil {
			continue // explicit-null permitted
		}
		if !idPattern.MatchString(*in) {
			return fmt.Errorf("is08: output.caps.routable_inputs[%d] %q: must match [a-zA-Z0-9-_]+",
				i, *in)
		}
	}
	return nil
}

// ValidateInputParent validates a /inputs/{id}/parent body.
func ValidateInputParent(p InputParent) error {
	// Per spec, both id and type required (response shape) but each
	// may be null. id pattern only when non-null.
	if p.ID != nil && *p.ID != "" && !uuidPattern.MatchString(*p.ID) {
		return fmt.Errorf("is08: input.parent.id %q: not a UUID", *p.ID)
	}
	if p.Type != nil {
		switch *p.Type {
		case "source", "receiver":
		default:
			return fmt.Errorf("is08: input.parent.type %q: must be source/receiver/null", *p.Type)
		}
	}
	return nil
}

// ValidateChannels validates a /inputs/{id}/channels or
// /outputs/{id}/channels body — minItems 1, label required.
func ValidateChannels(cs []Channel) error {
	if len(cs) < 1 {
		return fmt.Errorf("is08: channels: must contain at least 1 entry")
	}
	for i, c := range cs {
		if c.Label == "" {
			return fmt.Errorf("is08: channels[%d].label: required", i)
		}
	}
	return nil
}

// ValidateInputProperties / OutputProperties validate the shared
// {name, description} body.
func ValidateInputProperties(p InputProperties) error {
	if p.Name == "" {
		return fmt.Errorf("is08: input.properties.name: required")
	}
	if p.Description == "" {
		return fmt.Errorf("is08: input.properties.description: required")
	}
	return nil
}

// ValidateIO validates a /map/io body — every embedded property
// section is validated when present; missing sections are allowed
// (the dedicated per-property endpoints are authoritative).
func ValidateIO(io IO) error {
	if io.Inputs == nil {
		return fmt.Errorf("is08: io.inputs: required (may be empty object)")
	}
	if io.Outputs == nil {
		return fmt.Errorf("is08: io.outputs: required (may be empty object)")
	}
	for id, in := range io.Inputs {
		if !idPattern.MatchString(id) {
			return fmt.Errorf("is08: io.inputs: id %q: must match [a-zA-Z0-9-_]+", id)
		}
		if in.Properties != nil {
			if err := ValidateInputProperties(*in.Properties); err != nil {
				return err
			}
		}
		if in.Caps != nil {
			if err := ValidateInputCaps(*in.Caps); err != nil {
				return err
			}
		}
		if in.Parent != nil {
			if err := ValidateInputParent(*in.Parent); err != nil {
				return err
			}
		}
		if len(in.Channels) > 0 {
			if err := ValidateChannels(in.Channels); err != nil {
				return err
			}
		}
	}
	for id, out := range io.Outputs {
		if !idPattern.MatchString(id) {
			return fmt.Errorf("is08: io.outputs: id %q: must match [a-zA-Z0-9-_]+", id)
		}
		if out.Properties != nil {
			if err := ValidateInputProperties(*out.Properties); err != nil {
				return err
			}
		}
		if out.Caps != nil {
			if err := ValidateOutputCaps(*out.Caps); err != nil {
				return err
			}
		}
		if out.SourceID != nil && *out.SourceID != "" && !uuidPattern.MatchString(*out.SourceID) {
			return fmt.Errorf("is08: io.outputs[%s].source_id %q: not a UUID", id, *out.SourceID)
		}
		if len(out.Channels) > 0 {
			if err := ValidateChannels(out.Channels); err != nil {
				return err
			}
		}
	}
	return nil
}

// EncodeMapActive marshals + validates a /map/active body.
func EncodeMapActive(m MapActive) ([]byte, error) {
	if err := ValidateMapActive(m); err != nil {
		return nil, err
	}
	return jsonIndent(m)
}

// DecodeMapActive parses + validates a /map/active body.
func DecodeMapActive(raw []byte) (MapActive, error) {
	var m MapActive
	if err := decodeStrict(raw, &m); err != nil {
		return MapActive{}, err
	}
	if err := ValidateMapActive(m); err != nil {
		return MapActive{}, err
	}
	return m, nil
}

// EncodeMapActivationRequest marshals + validates a POST
// /map/activations body.
func EncodeMapActivationRequest(r MapActivationRequest) ([]byte, error) {
	if err := ValidateMapActivationRequest(r); err != nil {
		return nil, err
	}
	return jsonIndent(r)
}

// DecodeMapActivationRequest parses + validates a POST
// /map/activations body.
func DecodeMapActivationRequest(raw []byte) (MapActivationRequest, error) {
	var r MapActivationRequest
	if err := decodeStrict(raw, &r); err != nil {
		return MapActivationRequest{}, err
	}
	if err := ValidateMapActivationRequest(r); err != nil {
		return MapActivationRequest{}, err
	}
	return r, nil
}

// EncodeIO marshals + validates a /map/io body.
func EncodeIO(io IO) ([]byte, error) {
	if err := ValidateIO(io); err != nil {
		return nil, err
	}
	return jsonIndent(io)
}

// DecodeIO parses + validates a /map/io body.
func DecodeIO(raw []byte) (IO, error) {
	var io IO
	if err := decodeStrict(raw, &io); err != nil {
		return IO{}, err
	}
	if err := ValidateIO(io); err != nil {
		return IO{}, err
	}
	return io, nil
}

// decodeStrict is the shared strict decode helper.
func decodeStrict(raw []byte, dst any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return fmt.Errorf("is08: decode %T: %w", dst, err)
	}
	if d.More() {
		return fmt.Errorf("is08: decode %T: trailing JSON", dst)
	}
	return nil
}

// jsonIndent is the canonical pretty-printer.
func jsonIndent(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}
