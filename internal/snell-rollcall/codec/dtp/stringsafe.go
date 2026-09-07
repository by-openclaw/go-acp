package dtp

import "fmt"

// String-safe escape bytes.
//
// A block travelling in a string parameter must contain no interior NUL,
// because the vendor's own code copies such parameters with strcpy. The escape
// removes NUL by spending a byte on the two values that need it.
const (
	escapePrefix = 0xFF
	escapeZero   = 0xFD // 0xFF 0xFD stands for 0x00
	escapeFF     = 0xFE // 0xFF 0xFE stands for 0xFF
)

// appendEscaped appends body with its interior bytes escaped.
//
// The first and last bytes are left alone, which is what the specification
// says: a NUL in the first byte cannot occur because the count byte always has
// the string-safe flag set, and a NUL in the last byte is harmless because
// strcpy copies it as the terminator ("Full Control Command Set",
// §String-Safe Encoding).
func appendEscaped(dst, body []byte) []byte {
	for i, c := range body {
		last := i == len(body)-1
		switch {
		case last:
			dst = append(dst, c)
		case c == 0x00:
			dst = append(dst, escapePrefix, escapeZero)
		case c == escapePrefix:
			dst = append(dst, escapePrefix, escapeFF)
		default:
			dst = append(dst, c)
		}
	}
	return dst
}

// unescape reverses appendEscaped.
//
// It accepts an unescaped trailing 0x00 or 0xFF, because the encoder is not
// required to escape the final byte and the vendor's does not. A 0xFF followed
// by anything other than the two defined codes is malformed: silently passing
// it through would corrupt the item stream that follows.
func unescape(b []byte) ([]byte, error) {
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); i++ {
		if b[i] != escapePrefix {
			out = append(out, b[i])
			continue
		}
		if i == len(b)-1 {
			// A trailing prefix byte is a literal: the encoder does not
			// escape the final byte.
			out = append(out, escapePrefix)
			continue
		}
		switch b[i+1] {
		case escapeZero:
			out = append(out, 0x00)
		case escapeFF:
			out = append(out, escapePrefix)
		default:
			return nil, fmt.Errorf("%w: FF %02X at offset %d", ErrBadEscape, b[i+1], i)
		}
		i++
	}
	return out, nil
}

// AppendStringValue appends a string-safe block wrapped for carriage in the
// string component of a command: a single size byte, then the block.
//
// This is the form a router controller expects when it reads parameters from
// the string half of a numeric-plus-string command.
func AppendStringValue(dst []byte, p Params) ([]byte, error) {
	block, err := Encode(p, true)
	if err != nil {
		return nil, err
	}
	if len(block) > MaxStringLen {
		return nil, fmt.Errorf("%w: encoded block is %d bytes, limit %d",
			ErrStringTooLong, len(block), MaxStringLen)
	}
	dst = append(dst, byte(len(block)))
	return append(dst, block...), nil
}

// ParseStringValue reads a block wrapped by AppendStringValue and reports how
// many bytes it consumed, so a caller can tell whether anything followed.
func ParseStringValue(b []byte) (Params, int, error) {
	if len(b) == 0 {
		return nil, 0, fmt.Errorf("%w: no size byte", ErrShortBuffer)
	}
	n := int(b[0])
	if len(b)-1 < n {
		return nil, 0, fmt.Errorf("%w: size byte says %d, %d bytes present",
			ErrShortBuffer, n, len(b)-1)
	}
	p, err := Decode(b[1 : 1+n])
	if err != nil {
		return p, 1 + n, err
	}
	return p, 1 + n, nil
}
