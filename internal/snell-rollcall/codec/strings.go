package codec

import (
	"bytes"
	"fmt"
	"unicode/utf8"
)

// String field budgets.
const (
	// MaxTextSize is MAXTEXTSIZE: the fixed field width of the 16-bit
	// generation, terminator included, so 19 usable bytes (rc3comm.h).
	MaxTextSize = 20

	// MaxLongString is LONGSTRLEN: the ceiling the 2014 extension sets for
	// NUL-terminated UTF-8 strings, terminator included (rc3comm.h).
	MaxLongString = 64
)

// fixedString reads a fixed-width field and returns the text up to the first
// NUL. The caller passes the exact field, so there is nothing here that can
// fail: every caller has already validated the structure's length.
//
// Bytes after the terminator are undefined by the specification. Real devices
// leave whatever was in the buffer there: the live Centra units and the vendor
// test client both pad with 0xCD. That is compliant, so this never reports it
// and callers must never compare it. Golden-frame tests must mask the padding
// or they become device-specific.
func fixedString(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	// No terminator inside the field is legal: the text simply fills it.
	return string(b)
}

// appendFixedString writes text into a fixed-width field, NUL-padded.
//
// The field is zero-filled first, so we never leak adjacent memory the way the
// vendor implementations do. A string that cannot fit with its terminator is
// an error rather than a silent truncation, because a truncated label is a
// data-loss bug the caller should decide about.
func appendFixedString(dst []byte, s string, width int) ([]byte, error) {
	if len(s) > width-1 {
		return nil, fmt.Errorf("%w: %d bytes into a %d-byte field", ErrStringTooLong, len(s), width)
	}
	var f [MaxTextSize]byte
	buf := f[:]
	if width > len(buf) {
		buf = make([]byte, width)
	}
	buf = buf[:width]
	for i := range buf {
		buf[i] = 0
	}
	copy(buf, s)
	return append(dst, buf...), nil
}

// TruncateFixed shortens s so it fits a fixed field with its terminator,
// cutting on a UTF-8 boundary. Used where the protocol mandates truncation
// rather than an error, such as projecting a long label into the 16-bit
// generation, or naming ourselves in an identity a peer will display.
//
// It is exported because the session layer builds identities from
// caller-supplied names, and a name too long for the field must be shortened
// rather than refused: a client that cannot connect because its own name is
// long is a worse outcome than one that appears under a shortened name.
func TruncateFixed(s string, width int) string {
	if len(s) <= width-1 {
		return s
	}
	cut := width - 1
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// CString reads a NUL-terminated string and returns it with the number of
// bytes consumed including the terminator.
//
// It is exported because several messages end with an optional string that is
// not part of any structure: the text a Nack may carry, the reason on a Wait.
// The session layer reads those directly.
//
// A field that runs to the end of the payload without a terminator is
// accepted: the vendor emits this when a value exactly fills its buffer.
func CString(b []byte) (string, int) {
	i := bytes.IndexByte(b, 0)
	if i < 0 {
		return string(b), len(b)
	}
	return string(b[:i]), i + 1
}

// appendCString appends a NUL-terminated string, refusing one that exceeds the
// long-string ceiling.
func appendCString(dst []byte, s string) ([]byte, error) {
	if len(s) > MaxLongString-1 {
		return nil, fmt.Errorf("%w: %d bytes, ceiling %d", ErrStringTooLong, len(s), MaxLongString)
	}
	dst = append(dst, s...)
	return append(dst, 0), nil
}
