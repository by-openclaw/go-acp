package is12

import (
	"encoding/json"
)

// MessageType is the integer discriminator on every IS-12 frame.
// Spec: APIs/schemas/base-message.json `messageType` enum.
type MessageType int

// Recognised IS-12 message types per v1.0.1 §5.
const (
	MessageTypeCommand              MessageType = 0
	MessageTypeCommandResponse      MessageType = 1
	MessageTypeNotification         MessageType = 2
	MessageTypeSubscription         MessageType = 3
	MessageTypeSubscriptionResponse MessageType = 4
	MessageTypeError                MessageType = 5
)

// IsValidMessageType is true for the six recognised values.
func IsValidMessageType(m MessageType) bool {
	return m >= MessageTypeCommand && m <= MessageTypeError
}

// String returns the human label for diagnostic logs / Info columns.
// Empty for unrecognised values.
func (m MessageType) String() string {
	switch m {
	case MessageTypeCommand:
		return "Command"
	case MessageTypeCommandResponse:
		return "CommandResponse"
	case MessageTypeNotification:
		return "Notification"
	case MessageTypeSubscription:
		return "Subscription"
	case MessageTypeSubscriptionResponse:
		return "SubscriptionResponse"
	case MessageTypeError:
		return "Error"
	}
	return ""
}

// MS-05-02 NcMethodStatus baseline values referenced by the wire
// codec — full enum lives in the ms05 package. We pin the success
// value here so transports can short-circuit `result.status == 200`
// without a cross-package import.
const NcMethodStatusOK = 200

// MethodID is the {level, index} identifier used to address methods
// + properties + events on an MS-05-02 class. Both components are
// 1-based.
type MethodID struct {
	Level int `json:"level"`
	Index int `json:"index"`
}

// PropertyID is structurally identical to MethodID — separate type
// to keep call sites self-documenting.
type PropertyID = MethodID

// EventID is structurally identical to MethodID.
type EventID = MethodID

// Command is one entry in a Command message's `commands` array.
type Command struct {
	Handle    int             `json:"handle"`
	OID       int             `json:"oid"`
	MethodID  MethodID        `json:"methodId"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// MethodResult is the shape of `responses[*].result` on
// CommandResponse messages. Status is an MS-05-02 NcMethodStatus
// (200 for success); Value is the typed return value as raw JSON
// (callers unmarshal per the called method's return type); ErrorMessage
// is the optional human-readable error string.
type MethodResult struct {
	Status       int             `json:"status"`
	Value        json.RawMessage `json:"value,omitempty"`
	ErrorMessage string          `json:"errorMessage,omitempty"`
}

// CommandResponseEntry is one entry in a CommandResponse message's
// `responses` array.
type CommandResponseEntry struct {
	Handle int          `json:"handle"`
	Result MethodResult `json:"result"`
}

// Notification is one entry in a Notification message's
// `notifications` array.
type Notification struct {
	OID       int       `json:"oid"`
	EventID   EventID   `json:"eventId"`
	EventData EventData `json:"eventData"`
}

// EventData is the union body carried in Notification.eventData. The
// only standardised variant in v1.0.1 is property-changed; the
// `oneOf` schema means future sub-types layer in transparently.
type EventData = PropertyChangedEventData

// PropertyChangedEventData carries a property update event from the
// Node to a subscribed Controller. SequenceItemIndex is non-null
// only for sequence-typed properties.
type PropertyChangedEventData struct {
	PropertyID        PropertyID      `json:"propertyId"`
	ChangeType        int             `json:"changeType"`
	Value             json.RawMessage `json:"value"`
	SequenceItemIndex *int            `json:"sequenceItemIndex"`
}

// CommandMessage is the body of a messageType=0 frame.
type CommandMessage struct {
	MessageType MessageType `json:"messageType"`
	Commands    []Command   `json:"commands"`
}

// CommandResponseMessage is the body of a messageType=1 frame.
type CommandResponseMessage struct {
	MessageType MessageType            `json:"messageType"`
	Responses   []CommandResponseEntry `json:"responses"`
}

// NotificationMessage is the body of a messageType=2 frame.
type NotificationMessage struct {
	MessageType   MessageType    `json:"messageType"`
	Notifications []Notification `json:"notifications"`
}

// SubscriptionMessage is the body of a messageType=3 frame.
type SubscriptionMessage struct {
	MessageType   MessageType `json:"messageType"`
	Subscriptions []int       `json:"subscriptions"`
}

// SubscriptionResponseMessage is the body of a messageType=4 frame.
type SubscriptionResponseMessage struct {
	MessageType   MessageType `json:"messageType"`
	Subscriptions []int       `json:"subscriptions"`
}

// ErrorMessage is the body of a messageType=5 frame.
type ErrorMessage struct {
	MessageType  MessageType `json:"messageType"`
	Status       int         `json:"status"`
	ErrorMessage string      `json:"errorMessage"`
}
