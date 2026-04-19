// Package-level error taxonomy for the Ember+ plugin. Each category
// names a specific layer so the CLI, API, and tests can match on
// `errors.Is(err, ember.ErrX)` or type-assert via `errors.As`.
//
// Layers, from lowest to highest:
//
//	ErrS101    — TCP framing, CRC, BOF/EOF, byte-stuffing
//	ErrBER     — ASN.1 BER: tag, length, primitive/constructed
//	ErrGlow    — Glow DTD semantics: wrong tag number, missing field
//	ErrProto   — consumer-facing protocol error (path unknown, etc.)
//
// Wrapper types carry extra context (path, tag, hex) without losing
// the sentinel. Use Err<Layer>Wrap(err, ...) to produce one.
package emberplus

import (
	"errors"
	"fmt"
)

// Sentinel errors. Match with errors.Is(err, ErrX).
var (
	// ErrS101 indicates a transport-level failure: bad frame, CRC
	// mismatch, truncation, or connection drop.
	ErrS101 = errors.New("emberplus: s101 framing error")

	// ErrBER indicates an ASN.1 BER decode failure (bad tag / length,
	// truncated TLV). The wire was understood at the S101 layer but
	// its BER body could not be parsed.
	ErrBER = errors.New("emberplus: ber decode error")

	// ErrGlow indicates a Glow-DTD-level error: unrecognised
	// APPLICATION tag in a context where it must be known, missing
	// required CTX field, wrong CHOICE branch, etc.
	ErrGlow = errors.New("emberplus: glow dtd error")

	// ErrProto indicates a consumer-facing protocol error: path not
	// found, validation rejected, timeout waiting for a reply.
	ErrProto = errors.New("emberplus: protocol error")
)

// Layer names the error origin in structured logs and the profile
// audit trail. Derived from the sentinel an error wraps.
type Layer string

const (
	LayerS101  Layer = "s101"
	LayerBER   Layer = "ber"
	LayerGlow  Layer = "glow"
	LayerProto Layer = "proto"
)

// ClassifyError returns the coarse layer for an error, "unknown" if
// it wraps none of the sentinels. Callers use this to feed the
// compliance profile or log taxonomy.
func ClassifyError(err error) Layer {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrS101):
		return LayerS101
	case errors.Is(err, ErrBER):
		return LayerBER
	case errors.Is(err, ErrGlow):
		return LayerGlow
	case errors.Is(err, ErrProto):
		return LayerProto
	default:
		return "unknown"
	}
}

// WrapS101, WrapBER, WrapGlow, WrapProto attach one of the sentinels
// to a lower-level cause while preserving the original message and
// supporting errors.Is. Use the one whose layer the caller belongs to.
func WrapS101(msg string, cause error) error  { return layerErr(ErrS101, msg, cause) }
func WrapBER(msg string, cause error) error   { return layerErr(ErrBER, msg, cause) }
func WrapGlow(msg string, cause error) error  { return layerErr(ErrGlow, msg, cause) }
func WrapProto(msg string, cause error) error { return layerErr(ErrProto, msg, cause) }

type layeredError struct {
	sentinel error
	msg      string
	cause    error
}

func (e *layeredError) Error() string {
	if e.cause == nil {
		return fmt.Sprintf("%s: %s", e.sentinel, e.msg)
	}
	return fmt.Sprintf("%s: %s: %v", e.sentinel, e.msg, e.cause)
}

// Is reports whether target matches either the sentinel or the inner cause.
func (e *layeredError) Is(target error) bool {
	return errors.Is(e.sentinel, target) || (e.cause != nil && errors.Is(e.cause, target))
}

// Unwrap exposes the inner cause to errors.As / errors.Unwrap.
func (e *layeredError) Unwrap() error { return e.cause }

func layerErr(sentinel error, msg string, cause error) error {
	return &layeredError{sentinel: sentinel, msg: msg, cause: cause}
}
