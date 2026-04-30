package is12

import (
	"fmt"
)

func validateMethodID(m MethodID) error {
	if m.Level < 1 {
		return fmt.Errorf("is12: methodId.level %d: must be >= 1", m.Level)
	}
	if m.Index < 1 {
		return fmt.Errorf("is12: methodId.index %d: must be >= 1", m.Index)
	}
	return nil
}

func validateHandle(h int) error {
	if h < 1 || h > 65535 {
		return fmt.Errorf("is12: handle %d: must be in [1, 65535]", h)
	}
	return nil
}

func validateOID(o int) error {
	if o < 1 {
		return fmt.Errorf("is12: oid %d: must be >= 1", o)
	}
	return nil
}

func validateStatus(s int) error {
	if s < 0 || s > 65535 {
		return fmt.Errorf("is12: status %d: must be in [0, 65535]", s)
	}
	return nil
}

func (m CommandMessage) validate() error {
	if m.MessageType != 0 && m.MessageType != MessageTypeCommand {
		return fmt.Errorf("is12: command.messageType %d: must be %d",
			m.MessageType, MessageTypeCommand)
	}
	if m.Commands == nil {
		return fmt.Errorf("is12: command.commands: required (may be empty array)")
	}
	for i, c := range m.Commands {
		if err := validateHandle(c.Handle); err != nil {
			return fmt.Errorf("is12: command.commands[%d]: %w", i, err)
		}
		if err := validateOID(c.OID); err != nil {
			return fmt.Errorf("is12: command.commands[%d]: %w", i, err)
		}
		if err := validateMethodID(c.MethodID); err != nil {
			return fmt.Errorf("is12: command.commands[%d]: %w", i, err)
		}
	}
	return nil
}

func (m CommandResponseMessage) validate() error {
	if m.MessageType != 0 && m.MessageType != MessageTypeCommandResponse {
		return fmt.Errorf("is12: response.messageType %d: must be %d",
			m.MessageType, MessageTypeCommandResponse)
	}
	if m.Responses == nil {
		return fmt.Errorf("is12: response.responses: required (may be empty array)")
	}
	for i, r := range m.Responses {
		if err := validateHandle(r.Handle); err != nil {
			return fmt.Errorf("is12: response.responses[%d]: %w", i, err)
		}
		if err := validateStatus(r.Result.Status); err != nil {
			return fmt.Errorf("is12: response.responses[%d].result: %w", i, err)
		}
	}
	return nil
}

func (m NotificationMessage) validate() error {
	if m.MessageType != 0 && m.MessageType != MessageTypeNotification {
		return fmt.Errorf("is12: notification.messageType %d: must be %d",
			m.MessageType, MessageTypeNotification)
	}
	if m.Notifications == nil {
		return fmt.Errorf("is12: notification.notifications: required (may be empty array)")
	}
	for i, n := range m.Notifications {
		if err := validateOID(n.OID); err != nil {
			return fmt.Errorf("is12: notification.notifications[%d]: %w", i, err)
		}
		if err := validateMethodID(n.EventID); err != nil {
			return fmt.Errorf("is12: notification.notifications[%d].eventId: %w", i, err)
		}
		if err := validateMethodID(n.EventData.PropertyID); err != nil {
			return fmt.Errorf("is12: notification.notifications[%d].eventData.propertyId: %w", i, err)
		}
		if n.EventData.ChangeType < 0 || n.EventData.ChangeType > 65535 {
			return fmt.Errorf("is12: notification.notifications[%d].eventData.changeType %d: must be in [0, 65535]",
				i, n.EventData.ChangeType)
		}
	}
	return nil
}

func (m SubscriptionMessage) validate() error {
	if m.MessageType != 0 && m.MessageType != MessageTypeSubscription {
		return fmt.Errorf("is12: subscription.messageType %d: must be %d",
			m.MessageType, MessageTypeSubscription)
	}
	if m.Subscriptions == nil {
		return fmt.Errorf("is12: subscription.subscriptions: required (may be empty array)")
	}
	return nil
}

func (m SubscriptionResponseMessage) validate() error {
	if m.MessageType != 0 && m.MessageType != MessageTypeSubscriptionResponse {
		return fmt.Errorf("is12: subscription_response.messageType %d: must be %d",
			m.MessageType, MessageTypeSubscriptionResponse)
	}
	if m.Subscriptions == nil {
		return fmt.Errorf("is12: subscription_response.subscriptions: required (may be empty array)")
	}
	return nil
}

func (m ErrorMessage) validate() error {
	if m.MessageType != 0 && m.MessageType != MessageTypeError {
		return fmt.Errorf("is12: error.messageType %d: must be %d",
			m.MessageType, MessageTypeError)
	}
	if err := validateStatus(m.Status); err != nil {
		return err
	}
	if m.ErrorMessage == "" {
		return fmt.Errorf("is12: error.errorMessage: required")
	}
	return nil
}
