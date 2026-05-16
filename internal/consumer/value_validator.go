package consumer

import (
	"context"
	"errors"
)

// ValueValidator is the optional contract a Protocol implementation MAY
// satisfy to support offline pre-flight checks on a single (obj, value)
// pair. It mirrors SetValue's resolution + type-check pipeline minus the
// wire send, so the CLI (notably `import --dry-run`) can detect bad
// obj-ids, out-of-options enum values, and type mismatches before any
// state-changing call.
//
// Plugins that can't validate offline simply don't implement this
// interface — callers fall back to "ship the SetValue and catch the
// device error". Use type assertion to detect support:
//
//	if v, ok := plug.(consumer.ValueValidator); ok { v.Validate(...) }
//
// Naming intentionally differs from the trace-decoder Validator (see
// internal/protocol/validator.go) — that interface decodes captured
// trames offline, this one checks a single ValueRequest against the
// live device's object catalogue.
type ValueValidator interface {
	ValidateValue(ctx context.Context, req ValueRequest, val Value) error
}

// ErrObjectNotFound is returned by Validate when the request's object
// resolution fails — for ACP2 this is the device replying stat=1 to a
// single-object get_object. Callers (the importer) map this to a skip
// reason of "not_found" so the operator sees the row was dropped
// because the obj-id doesn't exist on the live device.
var ErrObjectNotFound = errors.New("protocol: object not found")

// ErrValidationFailed is returned by Validate when the value fails a
// client-side check (wrong type, enum out of options, IPv4 not
// parseable, etc.). Callers map this to a skip reason of
// "validation_failed".
var ErrValidationFailed = errors.New("protocol: validation failed")
