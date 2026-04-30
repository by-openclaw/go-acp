package is07

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Message is the closed sum-type for every sender → receiver
// envelope. Implementations: EventBoolean / EventNumber / EventString
// / EventObject / MessageHealth / MessageShutdownReboot /
// MessageConnectionStatus.
//
// Kind returns the discriminator that selects the variant on the
// wire — the same string that appears in the `message_type` field.
type Message interface {
	Kind() MessageType
	validate() error
}

// EventCommon holds the fields shared by every state-change event
// (event_core.json) — embedded by all four event payload variants.
type EventCommon struct {
	MessageType MessageType `json:"message_type"`
	Identity    Identity    `json:"identity"`
	Timing      Timing      `json:"timing"`
	EventType   string      `json:"event_type"`
}

// EventBoolean carries a `boolean(/<sub>)*` event_type payload.
// Spec: APIs/schemas/event_boolean.json.
type EventBoolean struct {
	EventCommon
	Payload PayloadBoolean `json:"payload"`
}

// EventNumber carries a `number(/<sub>)*` event_type payload.
// Spec: APIs/schemas/event_number.json.
type EventNumber struct {
	EventCommon
	Payload Number `json:"payload"`
}

// EventString carries a `string(/<sub>)*` event_type payload.
// Spec: APIs/schemas/event_string.json.
type EventString struct {
	EventCommon
	Payload PayloadString `json:"payload"`
}

// EventObject carries an `object(/<sub>)*` event_type payload.
// Spec: APIs/schemas/event_object.json.
type EventObject struct {
	EventCommon
	Payload PayloadObject `json:"payload"`
}

// MessageHealth is the heartbeat response. Spec:
// APIs/schemas/message_health.json.
type MessageHealth struct {
	MessageType MessageType `json:"message_type"`
	Timing      Timing      `json:"timing"`
}

// MessageShutdownReboot covers both `reboot` and `shutdown`
// envelopes. Spec: APIs/schemas/message_shutdown_reboot.json.
type MessageShutdownReboot struct {
	MessageType MessageType `json:"message_type"`
	Identity    Identity    `json:"identity"`
	Timing      Timing      `json:"timing"`
}

// MessageConnectionStatus is the MQTT-only Will/announce envelope
// indicating the client's connection state to the broker. Spec:
// APIs/schemas/message_connection_status.json.
type MessageConnectionStatus struct {
	MessageType MessageType `json:"message_type"`
	Active      bool        `json:"active"`
}

// Compile-time interface assertions.
var (
	_ Message = EventBoolean{}
	_ Message = EventNumber{}
	_ Message = EventString{}
	_ Message = EventObject{}
	_ Message = MessageHealth{}
	_ Message = MessageShutdownReboot{}
	_ Message = MessageConnectionStatus{}
)

// Kind getters — required by the Message interface. We return the
// spec literal even when the receiver was constructed with a blank
// field, so encoders can rely on it for the wire form.
func (e EventBoolean) Kind() MessageType            { return MessageTypeState }
func (e EventNumber) Kind() MessageType             { return MessageTypeState }
func (e EventString) Kind() MessageType             { return MessageTypeState }
func (e EventObject) Kind() MessageType             { return MessageTypeState }
func (m MessageHealth) Kind() MessageType           { return MessageTypeHealth }
func (m MessageShutdownReboot) Kind() MessageType   { return m.MessageType }
func (m MessageConnectionStatus) Kind() MessageType { return MessageTypeConnectionStatus }

// Command is the closed sum-type for every receiver → sender
// envelope. Implementations: CommandHealth / CommandSubscription.
type Command interface {
	Kind() CommandType
	validate() error
}

// CommandHealth probes the sender. Spec:
// APIs/schemas/command_health.json.
type CommandHealth struct {
	Command   CommandType `json:"command"`
	Timestamp string      `json:"timestamp"`
}

// CommandSubscription subscribes the receiver to the listed source
// IDs. Spec: APIs/schemas/command_subscription.json. Per spec
// `sources` MUST be unique; the encoder rejects duplicates.
type CommandSubscription struct {
	Command CommandType `json:"command"`
	Sources []string    `json:"sources"`
}

// Compile-time interface assertions.
var (
	_ Command = CommandHealth{}
	_ Command = CommandSubscription{}
)

// Kind getters — required by the Command interface.
func (c CommandHealth) Kind() CommandType       { return CommandTypeHealth }
func (c CommandSubscription) Kind() CommandType { return CommandTypeSubscription }

// envelope is the minimal peek-ahead struct used by DecodeMessage
// and DecodeCommand to decide which concrete variant to unmarshal
// into.
type envelope struct {
	MessageType MessageType `json:"message_type"`
	Command     CommandType `json:"command"`
}

// DecodeMessage parses any sender → receiver wire frame, switching
// on `message_type`. For state events it further switches on the
// `event_type` prefix to pick the boolean / number / string / object
// variant. Returns a typed error for unknown discriminators so
// callers can fire a compliance event without string-matching.
func DecodeMessage(raw []byte) (Message, error) {
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("is07: peek message_type: %w", err)
	}
	switch env.MessageType {
	case MessageTypeState:
		return decodeStateEvent(raw)
	case MessageTypeHealth:
		var m MessageHealth
		if err := decodeStrict(raw, &m); err != nil {
			return nil, err
		}
		if err := m.validate(); err != nil {
			return nil, err
		}
		return m, nil
	case MessageTypeReboot, MessageTypeShutdown:
		var m MessageShutdownReboot
		if err := decodeStrict(raw, &m); err != nil {
			return nil, err
		}
		if err := m.validate(); err != nil {
			return nil, err
		}
		return m, nil
	case MessageTypeConnectionStatus:
		var m MessageConnectionStatus
		if err := decodeStrict(raw, &m); err != nil {
			return nil, err
		}
		if err := m.validate(); err != nil {
			return nil, err
		}
		return m, nil
	case "":
		return nil, fmt.Errorf("is07: message_type: required")
	}
	return nil, fmt.Errorf("is07: message_type %q: unknown", env.MessageType)
}

// decodeStateEvent inspects event_type to pick the correct payload
// variant. Strict-decode rejects unknown fields per dhs convention.
func decodeStateEvent(raw []byte) (Message, error) {
	var head struct {
		EventType string `json:"event_type"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return nil, fmt.Errorf("is07: peek event_type: %w", err)
	}
	switch CategoryOf(head.EventType) {
	case EventCategoryBoolean:
		var e EventBoolean
		if err := decodeStrict(raw, &e); err != nil {
			return nil, err
		}
		if err := e.validate(); err != nil {
			return nil, err
		}
		return e, nil
	case EventCategoryNumber:
		var e EventNumber
		if err := decodeStrict(raw, &e); err != nil {
			return nil, err
		}
		if err := e.validate(); err != nil {
			return nil, err
		}
		return e, nil
	case EventCategoryString:
		var e EventString
		if err := decodeStrict(raw, &e); err != nil {
			return nil, err
		}
		if err := e.validate(); err != nil {
			return nil, err
		}
		return e, nil
	case EventCategoryObject:
		var e EventObject
		if err := decodeStrict(raw, &e); err != nil {
			return nil, err
		}
		if err := e.validate(); err != nil {
			return nil, err
		}
		return e, nil
	}
	return nil, fmt.Errorf("is07: event_type %q: must start with boolean/number/string/object", head.EventType)
}

// DecodeCommand parses any receiver → sender wire frame, switching
// on `command`.
func DecodeCommand(raw []byte) (Command, error) {
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("is07: peek command: %w", err)
	}
	switch env.Command {
	case CommandTypeHealth:
		var c CommandHealth
		if err := decodeStrict(raw, &c); err != nil {
			return nil, err
		}
		if err := c.validate(); err != nil {
			return nil, err
		}
		return c, nil
	case CommandTypeSubscription:
		var c CommandSubscription
		if err := decodeStrict(raw, &c); err != nil {
			return nil, err
		}
		if err := c.validate(); err != nil {
			return nil, err
		}
		return c, nil
	case "":
		return nil, fmt.Errorf("is07: command: required")
	}
	return nil, fmt.Errorf("is07: command %q: unknown", env.Command)
}

// EncodeMessage marshals any Message variant. The discriminator
// fields are normalised (filled with the canonical literal) before
// marshalling so callers cannot accidentally ship a frame with a
// blank `message_type`.
func EncodeMessage(m Message) ([]byte, error) {
	if m == nil {
		return nil, fmt.Errorf("is07: encode message: nil")
	}
	if err := m.validate(); err != nil {
		return nil, err
	}
	return marshalNormalised(m)
}

// EncodeCommand marshals any Command variant.
func EncodeCommand(c Command) ([]byte, error) {
	if c == nil {
		return nil, fmt.Errorf("is07: encode command: nil")
	}
	if err := c.validate(); err != nil {
		return nil, err
	}
	return marshalNormalised(c)
}

// marshalNormalised injects the canonical discriminator value into
// the in-memory struct before marshalling. We deliberately don't
// mutate the caller's struct — callers may keep state across many
// encodes.
func marshalNormalised(v any) ([]byte, error) {
	switch x := v.(type) {
	case EventBoolean:
		x.MessageType = MessageTypeState
		return jsonMarshalIndent(x)
	case EventNumber:
		x.MessageType = MessageTypeState
		return jsonMarshalIndent(x)
	case EventString:
		x.MessageType = MessageTypeState
		return jsonMarshalIndent(x)
	case EventObject:
		x.MessageType = MessageTypeState
		return jsonMarshalIndent(x)
	case MessageHealth:
		x.MessageType = MessageTypeHealth
		return jsonMarshalIndent(x)
	case MessageShutdownReboot:
		// `reboot` and `shutdown` are both legal — caller's intent
		// stays. validate() above already rejected unknown values.
		return jsonMarshalIndent(x)
	case MessageConnectionStatus:
		x.MessageType = MessageTypeConnectionStatus
		return jsonMarshalIndent(x)
	case CommandHealth:
		x.Command = CommandTypeHealth
		return jsonMarshalIndent(x)
	case CommandSubscription:
		x.Command = CommandTypeSubscription
		return jsonMarshalIndent(x)
	}
	return nil, fmt.Errorf("is07: encode: unsupported variant %T", v)
}

// decodeStrict is shared strict JSON decode helper — rejects unknown
// fields and trailing content for every IS-07 wire variant.
func decodeStrict(raw []byte, dst any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return fmt.Errorf("is07: decode %T: %w", dst, err)
	}
	if d.More() {
		return fmt.Errorf("is07: decode %T: trailing JSON", dst)
	}
	return nil
}

// jsonMarshalIndent is the canonical pretty-printer.
func jsonMarshalIndent(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}
