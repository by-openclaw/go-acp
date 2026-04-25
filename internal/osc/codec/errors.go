package codec

import "errors"

// Errors returned by the OSC codec.
var (
	ErrTruncated          = errors.New("osc: truncated payload")
	ErrAlignment          = errors.New("osc: 4-byte alignment violated")
	ErrStringNotTerminated = errors.New("osc: OSC-string not NUL-terminated within payload")
	ErrCommaMissing       = errors.New("osc: type-tag string does not begin with ','")
	ErrTagUnknown         = errors.New("osc: unknown type tag")
	ErrBlobTooLarge       = errors.New("osc: OSC-blob size negative or > payload")
	ErrBundleNotBundle    = errors.New("osc: bundle element does not start with '#bundle' or '/'")
	ErrBundleElementSize  = errors.New("osc: bundle element size negative or > remaining")
	ErrArrayUnbalanced    = errors.New("osc: '[' without matching ']' in type-tag string")
)

// ComplianceNote records a spec deviation surfaced by the decoder.
type ComplianceNote struct {
	Kind   string // e.g. "osc_alignment_violation"
	Detail string
}
