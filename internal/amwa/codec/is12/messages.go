package is12

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Message is the closed sum-type for every IS-12 wire frame. The
// Kind() method returns the integer discriminator.
type Message interface {
	Kind() MessageType
	validate() error
}

var (
	_ Message = CommandMessage{}
	_ Message = CommandResponseMessage{}
	_ Message = NotificationMessage{}
	_ Message = SubscriptionMessage{}
	_ Message = SubscriptionResponseMessage{}
	_ Message = ErrorMessage{}
)

func (CommandMessage) Kind() MessageType              { return MessageTypeCommand }
func (CommandResponseMessage) Kind() MessageType      { return MessageTypeCommandResponse }
func (NotificationMessage) Kind() MessageType         { return MessageTypeNotification }
func (SubscriptionMessage) Kind() MessageType         { return MessageTypeSubscription }
func (SubscriptionResponseMessage) Kind() MessageType { return MessageTypeSubscriptionResponse }
func (ErrorMessage) Kind() MessageType                { return MessageTypeError }

// envelope is the minimum-peek struct used by Decode to switch on
// messageType.
type envelope struct {
	MessageType MessageType `json:"messageType"`
}

// Decode parses any IS-12 wire frame and returns a typed Message.
// Strict-decode rejects unknown fields per dhs convention.
func Decode(raw []byte) (Message, error) {
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("is12: peek messageType: %w", err)
	}
	if !IsValidMessageType(env.MessageType) {
		return nil, fmt.Errorf("is12: messageType %d: unknown", env.MessageType)
	}
	switch env.MessageType {
	case MessageTypeCommand:
		var m CommandMessage
		if err := decodeStrict(raw, &m); err != nil {
			return nil, err
		}
		if err := m.validate(); err != nil {
			return nil, err
		}
		return m, nil
	case MessageTypeCommandResponse:
		var m CommandResponseMessage
		if err := decodeStrict(raw, &m); err != nil {
			return nil, err
		}
		if err := m.validate(); err != nil {
			return nil, err
		}
		return m, nil
	case MessageTypeNotification:
		var m NotificationMessage
		if err := decodeStrict(raw, &m); err != nil {
			return nil, err
		}
		if err := m.validate(); err != nil {
			return nil, err
		}
		return m, nil
	case MessageTypeSubscription:
		var m SubscriptionMessage
		if err := decodeStrict(raw, &m); err != nil {
			return nil, err
		}
		if err := m.validate(); err != nil {
			return nil, err
		}
		return m, nil
	case MessageTypeSubscriptionResponse:
		var m SubscriptionResponseMessage
		if err := decodeStrict(raw, &m); err != nil {
			return nil, err
		}
		if err := m.validate(); err != nil {
			return nil, err
		}
		return m, nil
	case MessageTypeError:
		var m ErrorMessage
		if err := decodeStrict(raw, &m); err != nil {
			return nil, err
		}
		if err := m.validate(); err != nil {
			return nil, err
		}
		return m, nil
	}
	return nil, fmt.Errorf("is12: messageType %d: dispatch fall-through", env.MessageType)
}

// Encode marshals any Message variant. The discriminator field is
// normalised to the canonical literal before marshalling so callers
// cannot accidentally ship a frame with the wrong `messageType`.
func Encode(m Message) ([]byte, error) {
	if m == nil {
		return nil, fmt.Errorf("is12: encode: nil")
	}
	if err := m.validate(); err != nil {
		return nil, err
	}
	switch v := m.(type) {
	case CommandMessage:
		v.MessageType = MessageTypeCommand
		return jsonIndent(v)
	case CommandResponseMessage:
		v.MessageType = MessageTypeCommandResponse
		return jsonIndent(v)
	case NotificationMessage:
		v.MessageType = MessageTypeNotification
		return jsonIndent(v)
	case SubscriptionMessage:
		v.MessageType = MessageTypeSubscription
		return jsonIndent(v)
	case SubscriptionResponseMessage:
		v.MessageType = MessageTypeSubscriptionResponse
		return jsonIndent(v)
	case ErrorMessage:
		v.MessageType = MessageTypeError
		return jsonIndent(v)
	}
	return nil, fmt.Errorf("is12: encode: unsupported variant %T", m)
}

func decodeStrict(raw []byte, dst any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return fmt.Errorf("is12: decode %T: %w", dst, err)
	}
	if d.More() {
		return fmt.Errorf("is12: decode %T: trailing JSON", dst)
	}
	return nil
}

func jsonIndent(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}
