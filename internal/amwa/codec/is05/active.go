package is05

// The /active view of a Sender or a Receiver: the same shape as /staged,
// read by different rules.
//
// IS-05 words the null of an immediate activation's requested_time for
// one endpoint — "for an immediate activation this field will always be
// null on the staged endpoint" (activation-response-schema.json). On
// /active the activation object is the record of the activation that
// last took effect, and nmos-cpp reports there the time the request
// arrived:
//
//	{"mode": "activate_immediate", "requested_time": "1791123035:736877991",
//	 "activation_time": "1791123035:738690889"}
//
// Read by the staged rule, a Sender that has been activated once could
// not be read again.

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// validateActiveActivation checks what holds on /active: a known mode,
// and TAI timestamps where there are any.
func validateActiveActivation(a Activation) error {
	if !IsValidActivationMode(a.Mode) {
		return fmt.Errorf("is05: activation.mode %q: invalid", a.Mode)
	}
	if a.RequestedTime != nil && !taiPattern.MatchString(*a.RequestedTime) {
		return fmt.Errorf("is05: activation.requested_time %q: must match `<sec>:<nsec>` TAI form", *a.RequestedTime)
	}
	return nil
}

// decodeActive parses one /active body into dst, refusing unknown
// fields and trailing content, as the staged decoders do.
func decodeActive(raw []byte, dst any, what string) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return fmt.Errorf("is05: decode active %s: %w", what, err)
	}
	if d.More() {
		return fmt.Errorf("is05: decode active %s: trailing JSON", what)
	}
	return nil
}

// DecodeActiveSender parses + validates a Sender's /active body.
func DecodeActiveSender(raw []byte) (*ActiveSender, error) {
	var s ActiveSender
	if err := decodeActive(raw, &s, "sender"); err != nil {
		return nil, err
	}
	if err := validateActiveActivation(s.Activation); err != nil {
		return nil, err
	}
	if s.TransportParams == nil {
		return nil, fmt.Errorf("is05: active.sender.transport_params: required (may be empty array)")
	}
	return &s, nil
}

// DecodeActiveReceiver parses + validates a Receiver's /active body.
func DecodeActiveReceiver(raw []byte) (*ActiveReceiver, error) {
	var r ActiveReceiver
	if err := decodeActive(raw, &r, "receiver"); err != nil {
		return nil, err
	}
	if err := validateActiveActivation(r.Activation); err != nil {
		return nil, err
	}
	if r.TransportParams == nil {
		return nil, fmt.Errorf("is05: active.receiver.transport_params: required (may be empty array)")
	}
	return &r, nil
}
