package consumer

import (
	"fmt"

	"dhs/internal/errcode"
)

// Typed sentinels — replace the prior `errors.New` variables. Identical
// names for caller compatibility; type changes from *errors.errorString
// to *errcode.Code so the CLI exit dispatcher recognises them.
//
// Per memory feedback_error_contract_cross_os — wire shape on stderr:
//   "<layer>:<code>: <human message>".
//
// Class mapping (per R1 #468):
//   plugin:*   → exit 2 (caller / state errors — "you sent something invalid")
//   session:*  → exit 1 (runtime — wire interaction problem)
//
// ErrWriteCoerced is treated as a runtime error: the wire write succeeded,
// but the provider returned a coerced value. Callers that want to tolerate
// coerced writes (clamp / round / enum-remap) can `errors.Is(err,
// ErrWriteCoerced)` and continue.

var (
	// ErrNotImplemented is returned by plugin stubs for operations a
	// protocol does not support.
	ErrNotImplemented = errcode.New(errcode.LayerPlugin, "not-implemented", errcode.ClassUsage)

	// ErrNotConnected is returned when a call requires a live transport
	// and none has been established (or it has been torn down).
	ErrNotConnected = errcode.New(errcode.LayerPlugin, "not-connected", errcode.ClassUsage)

	// ErrUnknownLabel is returned by GetValue/SetValue when the request
	// uses a Label that was never seen by the walker.
	ErrUnknownLabel = errcode.New(errcode.LayerPlugin, "unknown-label", errcode.ClassUsage)

	// ErrWriteTimeout is returned by SetValue when the set was
	// transmitted but the provider did not echo a confirming announce
	// within the configured write-confirm window.
	ErrWriteTimeout = errcode.New(errcode.LayerSession, "write-timeout", errcode.ClassRuntime)

	// ErrWriteCoerced is returned by SetValue when the provider
	// announced back a value different from the one requested.
	// Callers can opt-in to tolerate via errors.Is.
	ErrWriteCoerced = errcode.New(errcode.LayerSession, "write-coerced", errcode.ClassRuntime)

	// ErrWriteRejected is returned by SetValue when the provider's
	// echo indicates the write was refused (target locked, element
	// offline, access denied).
	ErrWriteRejected = errcode.New(errcode.LayerSession, "write-rejected", errcode.ClassRuntime)

	// ErrInvalidFormat is returned when a CLI verb's --format flag
	// receives a value outside the verb's supported set. Each verb
	// owns its own supported set; the code is shared so scripts can
	// dispatch on it uniformly.
	ErrInvalidFormat = errcode.New(errcode.LayerValidation, "invalid-format", errcode.ClassUsage)
)

// DHSError is the root of all protocol-family errors. Both transport-layer
// and object-layer failures implement this, so call sites can do one
// errors.As check.
type DHSError interface {
	error
	dhsError() // tag method
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
func (e *TransportError) dhsError()     {}

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
func (e *ValidationError) dhsError() {}
