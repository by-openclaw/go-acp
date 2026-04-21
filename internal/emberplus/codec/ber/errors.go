package ber

import "errors"

var (
	errTruncated      = errors.New("ber: truncated input")
	errTagTooLong     = errors.New("ber: tag number exceeds 4 bytes")
	errLengthTooLong  = errors.New("ber: length exceeds 4 bytes")
	errInvalidReal    = errors.New("ber: invalid REAL encoding")
	errOverflow       = errors.New("ber: integer overflow")
)
