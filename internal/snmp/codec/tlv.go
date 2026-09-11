package codec

import (
	"errors"
	"fmt"
)

// ErrMalformed is the class of every decode failure: a datagram this
// package could not read as SNMP. Callers match on it with errors.Is and
// read the wrapped text for which field gave up.
var ErrMalformed = errors.New("snmp: malformed message")

// malformed builds an ErrMalformed with the field that failed. Written
// as one helper because every decode path needs it and a bare
// fmt.Errorf at each of them is a %w somebody eventually forgets.
func malformed(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrMalformed, fmt.Sprintf(format, args...))
}

// BER tag bytes SNMP uses. Every one is single-byte: SNMP's tag numbers
// all fit in five bits, so the high-tag-number form (tag & 0x1f == 0x1f)
// never appears in a valid message and is refused rather than parsed.
const (
	tagInteger      byte = 0x02
	tagOctetString  byte = 0x04
	tagNull         byte = 0x05
	tagOID          byte = 0x06
	tagSequence     byte = 0x30
	tagIPAddress    byte = 0x40 // APPLICATION 0, primitive
	tagCounter32    byte = 0x41 // APPLICATION 1
	tagGauge32      byte = 0x42 // APPLICATION 2 (Unsigned32 is the same tag)
	tagTimeTicks    byte = 0x43 // APPLICATION 3
	tagOpaque       byte = 0x44 // APPLICATION 4
	tagCounter64    byte = 0x46 // APPLICATION 6
	tagNoSuchObject byte = 0x80 // CONTEXT 0, RFC 3416 §4.1
	tagNoSuchInst   byte = 0x81 // CONTEXT 1
	tagEndOfMIBView byte = 0x82 // CONTEXT 2
)

// tlv is one BER element: its tag, its contents, and how many bytes of
// the input it spanned.
type tlv struct {
	tag   byte
	value []byte
	size  int
}

// readTLV reads one element from the front of b. what names the field
// being read, so a refusal says WHERE the datagram stopped making sense
// rather than only that it did — the difference between a bug report
// that can be acted on and one that says "malformed".
func readTLV(b []byte, what string) (tlv, error) {
	if len(b) < 2 {
		return tlv{}, malformed("%s: truncated element, %d byte(s) left", what, len(b))
	}
	tag := b[0]
	if tag&0x1f == 0x1f {
		return tlv{}, malformed("%s: high-tag-number form (0x%02X) is not used by SNMP", what, tag)
	}
	length, lenSize, err := readLength(b[1:], what)
	if err != nil {
		return tlv{}, err
	}
	start := 1 + lenSize
	if length > len(b)-start {
		return tlv{}, malformed("%s: element 0x%02X claims %d byte(s), %d available",
			what, tag, length, len(b)-start)
	}
	return tlv{tag: tag, value: b[start : start+length], size: start + length}, nil
}

// expect reads one element and insists on its tag, which is what every
// caller here wants: SNMP's grammar is fixed, so an unexpected tag is a
// malformed message rather than an alternative to try.
func expect(b []byte, want byte, what string) (tlv, error) {
	e, err := readTLV(b, what)
	if err != nil {
		return tlv{}, err
	}
	if e.tag != want {
		return tlv{}, malformed("%s: tag 0x%02X, want 0x%02X", what, e.tag, want)
	}
	return e, nil
}

// readLength decodes a BER definite length.
//
// The indefinite form (0x80) is refused: it is legal BER and illegal
// SNMP (RFC 3417 §8 requires the definite form), and accepting it would
// mean scanning for an end-of-contents marker in a datagram an attacker
// supplied.
func readLength(b []byte, what string) (length, size int, err error) {
	if len(b) == 0 {
		return 0, 0, malformed("%s: truncated length", what)
	}
	first := b[0]
	if first&0x80 == 0 {
		return int(first), 1, nil
	}
	n := int(first & 0x7f)
	if n == 0 {
		return 0, 0, malformed("%s: indefinite length is not permitted in SNMP", what)
	}
	// More than four length bytes describes a datagram larger than any
	// UDP payload, so it is a lie however it was meant.
	if n > 4 {
		return 0, 0, malformed("%s: length of %d bytes exceeds any datagram", what, n)
	}
	if len(b) < 1+n {
		return 0, 0, malformed("%s: truncated long-form length", what)
	}
	for _, c := range b[1 : 1+n] {
		length = length<<8 | int(c)
	}
	// A length no datagram could carry is a lie whatever it decodes to,
	// and is refused before it becomes a slice bound.
	if length > MaxMessageSize {
		return 0, 0, malformed("%s: length of %d bytes exceeds the %d-byte datagram limit",
			what, length, MaxMessageSize)
	}
	return length, 1 + n, nil
}

// appendTLV writes one element. Lengths are emitted in the shortest form
// that fits, which is what BER's distinguished rules ask for and what
// every agent in the field expects to see.
func appendTLV(dst []byte, tag byte, value []byte) []byte {
	dst = append(dst, tag)
	dst = appendLength(dst, len(value))
	return append(dst, value...)
}

func appendLength(dst []byte, n int) []byte {
	if n < 0x80 {
		return append(dst, byte(n))
	}
	var buf [4]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte(n)
		n >>= 8
	}
	dst = append(dst, byte(0x80|(len(buf)-i)))
	return append(dst, buf[i:]...)
}

// appendInt writes a BER INTEGER in the minimal two's-complement form.
func appendInt(dst []byte, tag byte, v int64) []byte {
	var buf [8]byte
	i := len(buf)
	// Emit at least one byte, then keep going while the remaining value
	// is not already implied by the sign bit of the byte just written.
	for {
		i--
		buf[i] = byte(v)
		v >>= 8
		if (v == 0 && buf[i]&0x80 == 0) || (v == -1 && buf[i]&0x80 != 0) {
			break
		}
	}
	return appendTLV(dst, tag, buf[i:])
}

// appendUint writes an unsigned quantity (Counter32, Gauge32, TimeTicks,
// Counter64) as BER wants it: minimal, with a leading zero byte when the
// top bit would otherwise read as a sign.
func appendUint(dst []byte, tag byte, v uint64) []byte {
	var buf [9]byte
	i := len(buf)
	for {
		i--
		buf[i] = byte(v)
		v >>= 8
		if v == 0 {
			break
		}
	}
	if buf[i]&0x80 != 0 {
		i--
		buf[i] = 0
	}
	return appendTLV(dst, tag, buf[i:])
}

// parseInt reads a two's-complement integer of any width up to 8 bytes.
//
// Non-minimal encodings are ACCEPTED: agents pad, and a manager that
// refused a padded zero would refuse half the field. A width beyond 8
// bytes is refused, because no SNMP quantity is that wide and accepting
// it would silently truncate.
func parseInt(b []byte, what string) (int64, error) {
	if len(b) == 0 {
		return 0, malformed("%s: empty integer", what)
	}
	if len(b) > 8 {
		return 0, malformed("%s: %d-byte integer", what, len(b))
	}
	var v int64
	if b[0]&0x80 != 0 {
		v = -1 // sign-extend
	}
	for _, c := range b {
		v = v<<8 | int64(c)
	}
	return v, nil
}

// parseUint reads an unsigned quantity. Nine bytes are allowed for the
// leading zero a Counter64 with the top bit set is required to carry.
func parseUint(b []byte, what string) (uint64, error) {
	if len(b) == 0 {
		return 0, malformed("%s: empty integer", what)
	}
	if len(b) == 9 {
		if b[0] != 0 {
			return 0, malformed("%s: 9-byte integer without a zero pad", what)
		}
		b = b[1:]
	}
	if len(b) > 8 {
		return 0, malformed("%s: %d-byte integer", what, len(b))
	}
	var v uint64
	for _, c := range b {
		v = v<<8 | uint64(c)
	}
	return v, nil
}
