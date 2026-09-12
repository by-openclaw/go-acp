package codec

import (
	"errors"
	"fmt"
)

// Sentinel errors. Call sites use errors.Is / errors.As, never string matching
// (root CLAUDE.md).
var (
	// ErrShortBuffer means the input ended before a structure was complete.
	ErrShortBuffer = errors.New("rollcall: short buffer")

	// ErrBadTxFlags means the transmission header did not carry the only
	// compatibility mode still in use (TxFlagsMode3, spec 10.2.1).
	ErrBadTxFlags = errors.New("rollcall: bad transmission header flags")

	// ErrBadTxLength means the transmission header length was outside the
	// range the specification permits.
	ErrBadTxLength = errors.New("rollcall: bad transmission header length")

	// ErrLengthMismatch means the transmission header length and the message
	// header RLength disagree about how long the frame is.
	ErrLengthMismatch = errors.New("rollcall: header length mismatch")

	// ErrPayloadTooLong means an encode would exceed MaxPayload.
	ErrPayloadTooLong = errors.New("rollcall: payload too long")

	// ErrBadRoute means a Net route has a zero nibble below a non-zero one.
	// Routes fill from the top nibble down and may not contain gaps
	// (vendor Core/Transmit.c CheckForRoutingError).
	ErrBadRoute = errors.New("rollcall: malformed net route")

	// ErrBadAddress means an address string was not NNNN-UU-PP or
	// NNNN-UU-PP:SS with hexadecimal fields.
	ErrBadAddress = errors.New("rollcall: malformed address")

	// ErrStringTooLong means a string did not fit the field it was destined
	// for: 20 bytes including the terminator for the 16-bit generation, 64
	// for the 32-bit one.
	ErrStringTooLong = errors.New("rollcall: string too long for field")

	// ErrFieldRange means a value does not fit the field it was destined
	// for. It is what projecting a 32-bit command number onto a 16-bit
	// session returns: truncating it would silently address a different
	// command, which is worse than refusing.
	ErrFieldRange = errors.New("rollcall: value out of range for field")
)

// DecodeError locates a decode failure inside a structure so that a log line
// or a test failure names the field rather than a byte offset alone.
type DecodeError struct {
	Struct string // e.g. "DeviceInfo"
	Field  string // e.g. "Name"
	Offset int    // byte offset within the payload
	Err    error
}

func (e *DecodeError) Error() string {
	if e.Field == "" {
		return fmt.Sprintf("rollcall: decode %s at +%d: %v", e.Struct, e.Offset, e.Err)
	}
	return fmt.Sprintf("rollcall: decode %s.%s at +%d: %v", e.Struct, e.Field, e.Offset, e.Err)
}

func (e *DecodeError) Unwrap() error { return e.Err }

// decodeErr is the internal shorthand for building a DecodeError.
func decodeErr(structName, field string, offset int, err error) error {
	return &DecodeError{Struct: structName, Field: field, Offset: offset, Err: err}
}

// need reports a short-buffer DecodeError when b is smaller than want.
func need(b []byte, want int, structName, field string) error {
	if len(b) < want {
		return decodeErr(structName, field, len(b),
			fmt.Errorf("%w: need %d bytes, have %d", ErrShortBuffer, want, len(b)))
	}
	return nil
}
