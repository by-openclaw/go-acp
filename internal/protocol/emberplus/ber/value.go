package ber

import (
	"encoding/binary"
	"math"
)

// --- Encode value types ---

// EncodeBoolean returns BER BOOLEAN value bytes.
func EncodeBoolean(v bool) []byte {
	if v {
		return []byte{0xFF}
	}
	return []byte{0x00}
}

// EncodeInteger returns BER INTEGER value bytes (two's complement, big-endian).
// Uses minimum number of octets.
func EncodeInteger(v int64) []byte {
	if v == 0 {
		return []byte{0x00}
	}

	// Work with bytes, MSB first.
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(v))

	// Find first significant byte.
	start := 0
	if v > 0 {
		for start < 7 && buf[start] == 0x00 {
			start++
		}
		// If high bit set, prepend 0x00 to keep positive.
		if buf[start]&0x80 != 0 {
			start--
		}
	} else {
		for start < 7 && buf[start] == 0xFF {
			start++
		}
		// If high bit not set, prepend 0xFF to keep negative.
		if buf[start]&0x80 == 0 {
			start--
		}
	}
	return buf[start:]
}

// EncodeReal returns BER REAL value bytes for an IEEE 754 double.
// Uses binary encoding (X.690 8.5.6): first octet 0x80 + 8-byte double.
func EncodeReal(v float64) []byte {
	if v == 0 {
		return nil // zero-length content = 0.0
	}
	if math.IsInf(v, 1) {
		return []byte{0x40} // PLUS-INFINITY
	}
	if math.IsInf(v, -1) {
		return []byte{0x41} // MINUS-INFINITY
	}

	// Binary encoding, base 2, positive exponent format.
	// Simplest correct encoding: store as raw IEEE 754 double.
	var buf [9]byte
	buf[0] = 0x80 // binary encoding, sign+, base 2, exponent length 2
	bits := math.Float64bits(v)

	// Handle negative.
	if v < 0 {
		buf[0] |= 0x40 // sign bit
		bits = math.Float64bits(-v)
	}

	binary.BigEndian.PutUint64(buf[1:], bits)
	return buf[:]
}

// EncodeUTF8String returns BER UTF8String value bytes (raw UTF-8).
func EncodeUTF8String(s string) []byte {
	return []byte(s)
}

// EncodeOctetString returns BER OCTET STRING value bytes.
func EncodeOctetString(b []byte) []byte {
	out := make([]byte, len(b))
	copy(out, b)
	return out
}

// --- Decode value types ---

// DecodeBoolean reads a BER BOOLEAN from value bytes.
func DecodeBoolean(data []byte) (bool, error) {
	if len(data) != 1 {
		return false, errTruncated
	}
	return data[0] != 0, nil
}

// DecodeInteger reads a BER INTEGER from value bytes (two's complement).
func DecodeInteger(data []byte) (int64, error) {
	if len(data) == 0 {
		return 0, errTruncated
	}
	if len(data) > 8 {
		return 0, errOverflow
	}

	// Sign-extend to 8 bytes.
	var buf [8]byte
	pad := byte(0x00)
	if data[0]&0x80 != 0 {
		pad = 0xFF
	}
	for i := range buf {
		buf[i] = pad
	}
	copy(buf[8-len(data):], data)
	return int64(binary.BigEndian.Uint64(buf[:])), nil
}

// DecodeReal reads a BER REAL from value bytes.
func DecodeReal(data []byte) (float64, error) {
	if len(data) == 0 {
		return 0.0, nil // zero-length = 0.0
	}

	first := data[0]

	// Special values.
	if first == 0x40 {
		return math.Inf(1), nil
	}
	if first == 0x41 {
		return math.Inf(-1), nil
	}

	// Binary encoding.
	if first&0x80 != 0 {
		negative := first&0x40 != 0
		if len(data) < 9 {
			return 0, errInvalidReal
		}
		bits := binary.BigEndian.Uint64(data[1:9])
		v := math.Float64frombits(bits)
		if negative {
			v = -v
		}
		return v, nil
	}

	return 0, errInvalidReal
}

// DecodeUTF8String reads a BER UTF8String from value bytes.
func DecodeUTF8String(data []byte) string {
	return string(data)
}
