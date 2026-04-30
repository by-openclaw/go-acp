package is07

import (
	"fmt"
)

// validateIdentity enforces source_id presence + UUID grammar +
// optional flow_id grammar.
func validateIdentity(i Identity) error {
	if i.SourceID == "" {
		return fmt.Errorf("is07: identity.source_id: required")
	}
	if !uuidPattern.MatchString(i.SourceID) {
		return fmt.Errorf("is07: identity.source_id %q: not a UUID", i.SourceID)
	}
	if i.FlowID != "" && !uuidPattern.MatchString(i.FlowID) {
		return fmt.Errorf("is07: identity.flow_id %q: not a UUID", i.FlowID)
	}
	return nil
}

// validateTiming enforces creation_timestamp presence + optional TAI
// grammar on every timestamp field.
func validateTiming(t Timing, requireOrigin bool) error {
	if t.CreationTimestamp == "" {
		return fmt.Errorf("is07: timing.creation_timestamp: required")
	}
	if !taiPattern.MatchString(t.CreationTimestamp) {
		return fmt.Errorf("is07: timing.creation_timestamp %q: must match `<sec>:<nsec>` TAI form",
			t.CreationTimestamp)
	}
	if requireOrigin {
		if t.OriginTimestamp == "" {
			return fmt.Errorf("is07: timing.origin_timestamp: required")
		}
	}
	if t.OriginTimestamp != "" && !taiPattern.MatchString(t.OriginTimestamp) {
		return fmt.Errorf("is07: timing.origin_timestamp %q: must match `<sec>:<nsec>` TAI form",
			t.OriginTimestamp)
	}
	if t.ActionTimestamp != "" && !taiPattern.MatchString(t.ActionTimestamp) {
		return fmt.Errorf("is07: timing.action_timestamp %q: must match `<sec>:<nsec>` TAI form",
			t.ActionTimestamp)
	}
	return nil
}

// validateEventCommon enforces the shared event_core fields, with
// the per-category event_type prefix supplied by the caller.
func validateEventCommon(c EventCommon, want EventCategory) error {
	if c.MessageType != "" && c.MessageType != MessageTypeState {
		return fmt.Errorf("is07: event message_type %q: must be %q",
			c.MessageType, MessageTypeState)
	}
	if err := validateIdentity(c.Identity); err != nil {
		return err
	}
	if err := validateTiming(c.Timing, false); err != nil {
		return err
	}
	if c.EventType == "" {
		return fmt.Errorf("is07: event_type: required")
	}
	if got := CategoryOf(c.EventType); got != want {
		return fmt.Errorf("is07: event_type %q: expected category %q, got %q",
			c.EventType, want, got)
	}
	return nil
}

func (e EventBoolean) validate() error {
	return validateEventCommon(e.EventCommon, EventCategoryBoolean)
}

func (e EventNumber) validate() error {
	if err := validateEventCommon(e.EventCommon, EventCategoryNumber); err != nil {
		return err
	}
	if e.Payload.Scale != 0 && e.Payload.Scale < 1 {
		return fmt.Errorf("is07: event_number.payload.scale %d: must be >= 1", e.Payload.Scale)
	}
	return nil
}

func (e EventString) validate() error {
	return validateEventCommon(e.EventCommon, EventCategoryString)
}

func (e EventObject) validate() error {
	if err := validateEventCommon(e.EventCommon, EventCategoryObject); err != nil {
		return err
	}
	if e.Payload == nil {
		return fmt.Errorf("is07: event_object.payload: required (may be empty object)")
	}
	return nil
}

func (m MessageHealth) validate() error {
	if m.MessageType != "" && m.MessageType != MessageTypeHealth {
		return fmt.Errorf("is07: health.message_type %q: must be %q",
			m.MessageType, MessageTypeHealth)
	}
	return validateTiming(m.Timing, true)
}

// (note: MessageType is the struct's JSON-bearing field, NOT the
// Kind() interface method.)

func (m MessageShutdownReboot) validate() error {
	if m.MessageType != MessageTypeReboot && m.MessageType != MessageTypeShutdown {
		return fmt.Errorf("is07: shutdown_reboot.message_type %q: must be reboot or shutdown",
			m.MessageType)
	}
	if err := validateIdentity(m.Identity); err != nil {
		return err
	}
	return validateTiming(m.Timing, false)
}

func (m MessageConnectionStatus) validate() error {
	if m.MessageType != "" && m.MessageType != MessageTypeConnectionStatus {
		return fmt.Errorf("is07: connection_status.message_type %q: must be %q",
			m.MessageType, MessageTypeConnectionStatus)
	}
	return nil
}

func (c CommandHealth) validate() error {
	if c.Command != "" && c.Command != CommandTypeHealth {
		return fmt.Errorf("is07: command_health.command %q: must be %q",
			c.Command, CommandTypeHealth)
	}
	if c.Timestamp == "" {
		return fmt.Errorf("is07: command_health.timestamp: required")
	}
	if !taiPattern.MatchString(c.Timestamp) {
		return fmt.Errorf("is07: command_health.timestamp %q: must match `<sec>:<nsec>` TAI form",
			c.Timestamp)
	}
	return nil
}

func (c CommandSubscription) validate() error {
	if c.Command != "" && c.Command != CommandTypeSubscription {
		return fmt.Errorf("is07: command_subscription.command %q: must be %q",
			c.Command, CommandTypeSubscription)
	}
	if c.Sources == nil {
		return fmt.Errorf("is07: command_subscription.sources: required (may be empty array)")
	}
	seen := make(map[string]struct{}, len(c.Sources))
	for i, s := range c.Sources {
		if !uuidPattern.MatchString(s) {
			return fmt.Errorf("is07: command_subscription.sources[%d] %q: not a UUID", i, s)
		}
		if _, dup := seen[s]; dup {
			return fmt.Errorf("is07: command_subscription.sources[%d] %q: duplicate", i, s)
		}
		seen[s] = struct{}{}
	}
	return nil
}

// ValidateTypeBoolean enforces the type_boolean shape.
func ValidateTypeBoolean(t TypeBoolean) error {
	if t.Type != TypeBaseNameBoolean {
		return fmt.Errorf("is07: type_boolean.type %q: must be %q", t.Type, TypeBaseNameBoolean)
	}
	return nil
}

// ValidateTypeBooleanEnum enforces the type_boolean_enum shape.
func ValidateTypeBooleanEnum(t TypeBooleanEnum) error {
	if t.Type != TypeBaseNameBoolean {
		return fmt.Errorf("is07: type_boolean_enum.type %q: must be %q", t.Type, TypeBaseNameBoolean)
	}
	if len(t.Values) == 0 {
		return fmt.Errorf("is07: type_boolean_enum.values: required (must be non-empty)")
	}
	for i, v := range t.Values {
		if v.Label == "" {
			return fmt.Errorf("is07: type_boolean_enum.values[%d].label: required", i)
		}
		if v.Description == "" {
			return fmt.Errorf("is07: type_boolean_enum.values[%d].description: required", i)
		}
	}
	return nil
}

// ValidateTypeNumber enforces the type_number shape — Min/Max
// required, optional Step / Unit / Scale all > 0.
func ValidateTypeNumber(t TypeNumber) error {
	if t.Type != TypeBaseNameNumber {
		return fmt.Errorf("is07: type_number.type %q: must be %q", t.Type, TypeBaseNameNumber)
	}
	if t.Scale != 0 && t.Scale < 1 {
		return fmt.Errorf("is07: type_number.scale %d: must be >= 1", t.Scale)
	}
	if t.Min.Scale != 0 && t.Min.Scale < 1 {
		return fmt.Errorf("is07: type_number.min.scale %d: must be >= 1", t.Min.Scale)
	}
	if t.Max.Scale != 0 && t.Max.Scale < 1 {
		return fmt.Errorf("is07: type_number.max.scale %d: must be >= 1", t.Max.Scale)
	}
	return nil
}

// ValidateTypeNumberEnum enforces the type_number_enum shape.
func ValidateTypeNumberEnum(t TypeNumberEnum) error {
	if t.Type != TypeBaseNameNumber {
		return fmt.Errorf("is07: type_number_enum.type %q: must be %q", t.Type, TypeBaseNameNumber)
	}
	if len(t.Values) == 0 {
		return fmt.Errorf("is07: type_number_enum.values: required (must be non-empty)")
	}
	for i, v := range t.Values {
		if v.Label == "" {
			return fmt.Errorf("is07: type_number_enum.values[%d].label: required", i)
		}
		if v.Description == "" {
			return fmt.Errorf("is07: type_number_enum.values[%d].description: required", i)
		}
	}
	return nil
}

// ValidateTypeString enforces the type_string shape.
func ValidateTypeString(t TypeString) error {
	if t.Type != TypeBaseNameString {
		return fmt.Errorf("is07: type_string.type %q: must be %q", t.Type, TypeBaseNameString)
	}
	if t.MinLength != nil && *t.MinLength < 0 {
		return fmt.Errorf("is07: type_string.min_length %d: must be >= 0", *t.MinLength)
	}
	if t.MaxLength != nil && *t.MaxLength < 1 {
		return fmt.Errorf("is07: type_string.max_length %d: must be >= 1", *t.MaxLength)
	}
	return nil
}

// ValidateTypeStringEnum enforces the type_string_enum shape.
func ValidateTypeStringEnum(t TypeStringEnum) error {
	if t.Type != TypeBaseNameString {
		return fmt.Errorf("is07: type_string_enum.type %q: must be %q", t.Type, TypeBaseNameString)
	}
	if len(t.Values) == 0 {
		return fmt.Errorf("is07: type_string_enum.values: required (must be non-empty)")
	}
	for i, v := range t.Values {
		if v.Label == "" {
			return fmt.Errorf("is07: type_string_enum.values[%d].label: required", i)
		}
		if v.Description == "" {
			return fmt.Errorf("is07: type_string_enum.values[%d].description: required", i)
		}
	}
	return nil
}
