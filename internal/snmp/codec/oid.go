package codec

import (
	"strconv"
	"strings"
)

// OID is an object identifier: the sequence of arcs that names one
// object in a MIB. 1.3.6.1.2.1.1.1.0 is sysDescr.0.
//
// It is a slice of uint32 rather than a string because every operation
// that matters is arithmetic on arcs — GETNEXT compares them, a subtree
// check prefixes them, an agent indexes on them — and a dotted string
// turns each of those into a parse.
type OID []uint32

// MaxOIDLen is the ceiling RFC 3416 puts on a name: 128 sub-identifiers.
// A longer one is refused rather than allocated, since a decoder reading
// a hostile datagram must not size a slice from it.
const MaxOIDLen = 128

// ParseOID reads the dotted decimal form. A leading dot is accepted
// because half the world's documentation writes .1.3.6.1 and the other
// half writes 1.3.6.1, and they mean the same object.
func ParseOID(s string) (OID, error) {
	s = strings.TrimPrefix(s, ".")
	if s == "" {
		return nil, malformed("empty OID")
	}
	parts := strings.Split(s, ".")
	if len(parts) > MaxOIDLen {
		return nil, malformed("OID of %d arcs exceeds the %d permitted", len(parts), MaxOIDLen)
	}
	out := make(OID, 0, len(parts))
	for _, p := range parts {
		v, err := strconv.ParseUint(p, 10, 32)
		if err != nil {
			return nil, malformed("OID arc %q is not a number", p)
		}
		out = append(out, uint32(v))
	}
	if len(out) < 2 {
		return nil, malformed("OID %q has fewer than two arcs", s)
	}
	return out, nil
}

// MustParseOID is ParseOID for constants written in this repo. It panics,
// which is what a bad literal in our own source deserves; never give it
// anything that came off a wire or out of a config file.
func MustParseOID(s string) OID {
	o, err := ParseOID(s)
	if err != nil {
		panic(err)
	}
	return o
}

// String renders the dotted form without a leading dot.
func (o OID) String() string {
	if len(o) == 0 {
		return ""
	}
	var b strings.Builder
	for i, arc := range o {
		if i > 0 {
			b.WriteByte('.')
		}
		b.WriteString(strconv.FormatUint(uint64(arc), 10))
	}
	return b.String()
}

// Compare orders two OIDs lexicographically by arc, which is the order
// GETNEXT walks and therefore the only order an agent may serve its tree
// in. A shorter OID that is a prefix of a longer one sorts first.
func (o OID) Compare(other OID) int {
	for i := range o {
		if i >= len(other) {
			return 1
		}
		switch {
		case o[i] < other[i]:
			return -1
		case o[i] > other[i]:
			return 1
		}
	}
	if len(o) < len(other) {
		return -1
	}
	return 0
}

// HasPrefix reports whether o names this object or something beneath it.
// This is the subtree test a GETNEXT walk stops on and an agent's
// registration matches with.
func (o OID) HasPrefix(prefix OID) bool {
	if len(o) < len(prefix) {
		return false
	}
	for i, arc := range prefix {
		if o[i] != arc {
			return false
		}
	}
	return true
}

// Append returns o with more arcs, sharing nothing with o — an instance
// index appended in place would alias the table's own key.
func (o OID) Append(arcs ...uint32) OID {
	out := make(OID, 0, len(o)+len(arcs))
	out = append(out, o...)
	return append(out, arcs...)
}

// appendOID writes the BER encoding: the first two arcs packed into one
// sub-identifier, then base-128 with the continuation bit set on every
// byte but the last.
func appendOID(dst []byte, o OID) []byte {
	var body []byte
	if len(o) >= 2 {
		body = appendBase128(body, uint64(o[0])*40+uint64(o[1]))
		for _, arc := range o[2:] {
			body = appendBase128(body, uint64(arc))
		}
	}
	return appendTLV(dst, tagOID, body)
}

func appendBase128(dst []byte, v uint64) []byte {
	var buf [10]byte
	i := len(buf)
	i--
	buf[i] = byte(v & 0x7f)
	for v >>= 7; v > 0; v >>= 7 {
		i--
		buf[i] = byte(v&0x7f) | 0x80
	}
	return append(dst, buf[i:]...)
}

// parseOID decodes the BER contents of an OBJECT IDENTIFIER.
//
// The first sub-identifier packs two arcs: values below 80 split as
// (n/40, n%40), and 80 or above are all arc 2 with the remainder — which
// is how 2.100.3 and everything under the joint-iso-itu-t tree encode.
func parseOID(b []byte) (OID, error) {
	if len(b) == 0 {
		return nil, malformed("empty OID")
	}
	out := make(OID, 0, 16)
	first := true
	var v uint64
	var started bool
	for i, c := range b {
		if !started && c == 0x80 {
			// A continuation byte of 0x80 with nothing before it pads
			// the sub-identifier, which BER forbids and which is the
			// classic way to smuggle a second encoding of one OID.
			return nil, malformed("OID sub-identifier at byte %d has a padded encoding", i)
		}
		started = true
		if v > (1<<32-1)>>7 {
			return nil, malformed("OID sub-identifier at byte %d overflows 32 bits", i)
		}
		v = v<<7 | uint64(c&0x7f)
		if c&0x80 != 0 {
			continue
		}
		if first {
			switch {
			case v < 40:
				out = append(out, 0, uint32(v))
			case v < 80:
				out = append(out, 1, uint32(v-40))
			default:
				out = append(out, 2, uint32(v-80))
			}
			first = false
		} else {
			out = append(out, uint32(v))
		}
		if len(out) > MaxOIDLen {
			return nil, malformed("OID of more than %d arcs", MaxOIDLen)
		}
		v, started = 0, false
	}
	if started {
		return nil, malformed("OID ends mid sub-identifier")
	}
	return out, nil
}
