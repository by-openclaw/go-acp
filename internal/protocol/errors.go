package protocol

import (
	"errors"
	"fmt"
)

// ErrNotImplemented is returned by plugin stubs for operations a protocol
// does not support (e.g. Subscribe on a connectionless mode, or any method
// not defined in the plugin's spec version).
var ErrNotImplemented = errors.New("protocol: not implemented")

// ErrNotConnected is returned when a call requires a live transport and
// none has been established (or it has been torn down).
var ErrNotConnected = errors.New("protocol: not connected")

// ErrUnknownLabel is returned by GetValue/SetValue when the request uses a
// Label that was never seen by the walker.
var ErrUnknownLabel = errors.New("protocol: label not found in walker map")

// ErrWriteTimeout is returned by SetValue when the set was transmitted
// but the provider did not echo a confirming announce within the
// configured write-confirm window. The tree's value stays unchanged.
var ErrWriteTimeout = errors.New("protocol: write confirmation timeout")

// ErrWriteCoerced is returned by SetValue when the provider announced
// back a value different from the one requested (clamp, round,
// enum-remap). The returned Value reflects what the provider actually
// applied, so the caller can decide to accept or retry.
var ErrWriteCoerced = errors.New("protocol: write accepted but value coerced")

// ErrWriteRejected is returned by SetValue when the provider's echo
// indicates the write was refused (target locked, element offline,
// access denied). Tree's value stays unchanged.
var ErrWriteRejected = errors.New("protocol: write rejected by provider")

// ACPError is the root of all protocol-family errors. Both transport-layer
// and object-layer failures implement this, so call sites can do one
// errors.As check.
type ACPError interface {
	error
	acpError() // tag method
}

// TransportError is returned for socket, framing, and timeout failures —
// anything below the protocol's message boundary. These are typically
// retryable by the client retry loop.
type TransportError struct {
	Op  string // "send", "receive", "connect", "decode"
	Err error
}

func (e *TransportError) Error() string {
	if e.Err == nil {
		return "transport: " + e.Op
	}
	return fmt.Sprintf("transport %s: %v", e.Op, e.Err)
}

func (e *TransportError) Unwrap() error { return e.Err }
func (e *TransportError) acpError()     {}

// ValidationError is raised client-side before a request hits the wire
// (out-of-range value, wrong type for the object, string too long, unknown
// enum item). Never retryable.
type ValidationError struct {
	Field  string
	Reason string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation: %s: %s", e.Field, e.Reason)
}
func (e *ValidationError) acpError() {}
