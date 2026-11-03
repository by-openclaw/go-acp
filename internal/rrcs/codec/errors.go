package codec

import "errors"

// ErrMalformed is a document that is not the XML-RPC the specification
// describes: not well-formed, a wrong root, a missing element, a value
// that does not parse as its declared type.
var ErrMalformed = errors.New("rrcs: malformed XML-RPC")

// ErrType is a well-formed value of a kind other than the one asked for.
var ErrType = errors.New("rrcs: unexpected value type")

// ErrRange is a number outside the range the specification gives it.
var ErrRange = errors.New("rrcs: value out of range")
