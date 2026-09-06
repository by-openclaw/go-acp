package is07

import (
	"regexp"
)

// MessageType is the discriminator on the sender → receiver envelope.
// Spec: APIs/schemas/{event_core,message_health,message_shutdown_reboot,
// message_connection_status}.json `message_type` field.
type MessageType string

// Recognised message types per IS-07 v1.0.1 §2.
const (
	MessageTypeState            MessageType = "state"
	MessageTypeHealth           MessageType = "health"
	MessageTypeReboot           MessageType = "reboot"
	MessageTypeShutdown         MessageType = "shutdown"
	MessageTypeConnectionStatus MessageType = "connection_status"
)

// CommandType is the discriminator on the receiver → sender envelope.
// Spec: APIs/schemas/{command_health,command_subscription}.json
// `command` field.
type CommandType string

// Recognised command types per IS-07 v1.0.1 §2.
const (
	CommandTypeHealth       CommandType = "health"
	CommandTypeSubscription CommandType = "subscription"
)

// EventCategory is the top-level prefix on event_type strings.
// Spec: APIs/schemas/event_*.json `event_type` pattern.
type EventCategory string

// Recognised event-type prefixes.
const (
	EventCategoryBoolean EventCategory = "boolean"
	EventCategoryNumber  EventCategory = "number"
	EventCategoryString  EventCategory = "string"
	EventCategoryObject  EventCategory = "object"
)

// Identity is the per-event identity sub-object. `source_id` is
// always required; `flow_id` is optional and identifies the IS-04
// flow carrying the message when one is configured.
type Identity struct {
	SourceID string `json:"source_id"`
	FlowID   string `json:"flow_id,omitempty"`
}

// Timing is the per-event timing sub-object. All timestamps are TAI
// `<seconds>:<nanoseconds>` strings. `creation_timestamp` is
// mandatory; `origin_timestamp` and `action_timestamp` are optional.
type Timing struct {
	CreationTimestamp string `json:"creation_timestamp"`
	OriginTimestamp   string `json:"origin_timestamp,omitempty"`
	ActionTimestamp   string `json:"action_timestamp,omitempty"`
}

// Number is the rational-number payload used by event_number and
// type_number. `Scale` defaults to 1; final value is Value/Scale.
type Number struct {
	Value float64 `json:"value"`
	Scale int     `json:"scale,omitempty"`
}

// PayloadBoolean is the payload of an event_boolean.
type PayloadBoolean struct {
	Value bool `json:"value"`
}

// PayloadString is the payload of an event_string.
type PayloadString struct {
	Value string `json:"value"`
}

// PayloadObject is the payload of an event_object — opaque object
// allowed by spec.
type PayloadObject = map[string]any

// Source is the resource at `/sources/{id}` — the spec mandates
// exactly the two-element array `["type/", "state/"]`.
type Source []string

// SourcesIndex is the resource at `/sources` — array of source-id
// folders.
type SourcesIndex []string

// EnumValueBoolean is one entry of a type_boolean_enum.values array.
type EnumValueBoolean struct {
	Value       bool   `json:"value"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

// EnumValueNumber is one entry of a type_number_enum.values array.
type EnumValueNumber struct {
	Value       Number `json:"value"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

// EnumValueString is one entry of a type_string_enum.values array.
type EnumValueString struct {
	Value       string `json:"value"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

// TypeBaseName is the base type discriminator on every type_*
// descriptor (spec: APIs/schemas/type_*.json `type` enum).
type TypeBaseName string

// Recognised base type names per IS-07 v1.0.1.
const (
	TypeBaseNameBoolean TypeBaseName = "boolean"
	TypeBaseNameNumber  TypeBaseName = "number"
	TypeBaseNameString  TypeBaseName = "string"
)

// TypeBoolean is the type descriptor for a boolean source.
type TypeBoolean struct {
	Type TypeBaseName `json:"type"`
}

// TypeBooleanEnum is the type descriptor for a boolean-enum source.
type TypeBooleanEnum struct {
	Type   TypeBaseName       `json:"type"`
	Values []EnumValueBoolean `json:"values"`
}

// TypeNumber is the type descriptor for a numeric source. `Min` /
// `Max` are mandatory; `Step` and `Unit` are optional.
type TypeNumber struct {
	Type  TypeBaseName `json:"type"`
	Min   Number       `json:"min"`
	Max   Number       `json:"max"`
	Scale int          `json:"scale,omitempty"`
	Step  *Number      `json:"step,omitempty"`
	Unit  string       `json:"unit,omitempty"`
}

// TypeNumberEnum is the type descriptor for a numeric-enum source.
type TypeNumberEnum struct {
	Type   TypeBaseName      `json:"type"`
	Values []EnumValueNumber `json:"values"`
}

// TypeString is the type descriptor for a string source.
type TypeString struct {
	Type      TypeBaseName `json:"type"`
	MinLength *int         `json:"min_length,omitempty"`
	MaxLength *int         `json:"max_length,omitempty"`
	Pattern   string       `json:"pattern,omitempty"`
}

// TypeStringEnum is the type descriptor for a string-enum source.
type TypeStringEnum struct {
	Type   TypeBaseName      `json:"type"`
	Values []EnumValueString `json:"values"`
}

// ErrorBody is the standard 4xx/5xx response body per IS-07 §5.
type ErrorBody struct {
	Code  int    `json:"code"`
	Error string `json:"error"`
	Debug string `json:"debug,omitempty"`
}

// uuidPattern matches the IS-04 UUID grammar reused by IS-07.
var uuidPattern = regexp.MustCompile(
	`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`,
)

// taiPattern matches `<sec>:<nsec>` per IS-04 / IS-05 / IS-07.
var taiPattern = regexp.MustCompile(`^[0-9]+:[0-9]+$`)

// eventTypeBoolean / eventTypeNumber / eventTypeString /
// eventTypeObject match the per-prefix event_type pattern from each
// event_*.json schema.
var (
	eventTypeBoolean = regexp.MustCompile(`^boolean(/[^\s/]+)*$`)
	eventTypeNumber  = regexp.MustCompile(`^number(/[^\s/]+)*$`)
	eventTypeString  = regexp.MustCompile(`^string(/[^\s/]+)*$`)
	eventTypeObject  = regexp.MustCompile(`^object(/[^\s/]+)*$`)
)

// CategoryOf returns the prefix of an event_type string, or empty if
// it does not match any recognised category.
func CategoryOf(eventType string) EventCategory {
	switch {
	case eventTypeBoolean.MatchString(eventType):
		return EventCategoryBoolean
	case eventTypeNumber.MatchString(eventType):
		return EventCategoryNumber
	case eventTypeString.MatchString(eventType):
		return EventCategoryString
	case eventTypeObject.MatchString(eventType):
		return EventCategoryObject
	}
	return ""
}
