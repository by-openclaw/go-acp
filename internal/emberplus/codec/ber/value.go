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
// EncodeReal encodes a float64 as ITU-T X.690 §8.5 BER REAL in binary
// form (base 2, scaling factor 0). The on-wire shape is:
//
//	first byte   : 1 S BB FF EE  (S=sign, BB=base, FF=scale, EE=exponent length)
//	exponent     : two's-complement signed integer, EE+1 octets (or long form)
//	mantissa     : unsigned big-endian integer, normalised (trailing zero bits trimmed)
//
// Such that  value = (-1)^S * mantissa * 2^exponent.
//
// Special values: zero → empty; ±∞ → 0x40 / 0x41; NaN → 0x42.
func EncodeReal(v float64) []byte {
	if v == 0 {
		return nil
	}
	if math.IsNaN(v) {
		return []byte{0x42}
	}
	if math.IsInf(v, 1) {
		return []byte{0x40}
	}
	if math.IsInf(v, -1) {
		return []byte{0x41}
	}

	bits := math.Float64bits(v)
	sign := byte((bits >> 63) & 1)
	expRaw := int((bits >> 52) & 0x7FF)
	frac := bits & ((1 << 52) - 1)

	var mantissa uint64
	var exponent int
	if expRaw == 0 {
		// Subnormal: no implicit leading 1.
		mantissa = frac
		exponent = -1022 - 52
	} else {
		// Normal: add implicit leading 1.
		mantissa = frac | (1 << 52)
		exponent = expRaw - 1023 - 52
	}

	// Normalise: drop trailing zero bits (BER REAL mantissa must be odd
	// unless zero, per X.690 §8.5.7.3 — the normalised form).
	for mantissa != 0 && mantissa&1 == 0 {
		mantissa >>= 1
		exponent++
	}

	// Exponent as two's-complement minimum-octet signed integer.
	expBytes := encodeSignedInt(int64(exponent))
	// Mantissa as unsigned big-endian minimum-octet integer.
	mantBytes := encodeUnsignedInt(mantissa)

	// First byte layout: bit 7 always 1 (binary), bit 6 sign, bits 5-4
	// base (00 = base 2), bits 3-2 scale (00), bits 1-0 exponent-length
	// encoding: 00=1 octet, 01=2 octets, 10=3 octets, 11=long form with
	// extra length octet following.
	first := byte(0x80) | (sign << 6)
	out := make([]byte, 0, 1+len(expBytes)+len(mantBytes)+1)
	switch len(expBytes) {
	case 1:
		first |= 0x00
		out = append(out, first)
	case 2:
		first |= 0x01
		out = append(out, first)
	case 3:
		first |= 0x02
		out = append(out, first)
	default:
		first |= 0x03
		out = append(out, first, byte(len(expBytes)))
	}
	out = append(out, expBytes...)
	out = append(out, mantBytes...)
	return out
}

// encodeSignedInt returns the minimum-octet two's-complement big-endian
// representation of v. Shared helper for BER REAL exponent encoding.
func encodeSignedInt(v int64) []byte {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(v))

	// Trim redundant leading 0x00 / 0xFF bytes while preserving sign.
	start := 0
	for start < 7 {
		next := start + 1
		if (buf[start] == 0x00 && buf[next]&0x80 == 0) ||
			(buf[start] == 0xFF && buf[next]&0x80 != 0) {
			start = next
			continue
		}
		break
	}
	return buf[start:]
}

// encodeUnsignedInt returns the minimum-octet big-endian representation
// of v, treated as unsigned. Shared helper for BER REAL mantissa.
func encodeUnsignedInt(v uint64) []byte {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], v)
	start := 0
	for start < 7 && buf[start] == 0 {
		start++
	}
	return buf[start:]
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

// DecodeReal reads an ITU-T X.690 §8.5 BER REAL from value bytes.
// Supports the binary form (base 2, base 8, base 16) with scale 0 —
// which covers every REAL shape Glow actually emits.
func DecodeReal(data []byte) (float64, error) {
	if len(data) == 0 {
		return 0.0, nil
	}

	first := data[0]

	// Special values (§8.5.9).
	if first == 0x40 {
		return math.Inf(1), nil
	}
	if first == 0x41 {
		return math.Inf(-1), nil
	}
	if first == 0x42 {
		return math.NaN(), nil
	}

	// Decimal form (bit 7 = 0, bit 6 = 0) — not emitted by Glow, reject.
	if first&0xC0 == 0x00 {
		return 0, errInvalidReal
	}

	// Binary form: bit 7 = 1.
	if first&0x80 != 0 {
		sign := 1.0
		if first&0x40 != 0 {
			sign = -1.0
		}
		// Base: bits 5-4. 00=2, 01=8, 10=16.
		var baseFactor int
		switch (first >> 4) & 0x03 {
		case 0:
			baseFactor = 1 // exponent bits are powers of 2
		case 1:
			baseFactor = 3 // × 2^3 = base 8
		case 2:
			baseFactor = 4 // × 2^4 = base 16
		default:
			return 0, errInvalidReal
		}
		// Scaling factor: bits 3-2. Added to mantissa.
		scale := int((first >> 2) & 0x03)
		// Exponent length: bits 1-0.
		var expLen int
		var expStart int
		switch first & 0x03 {
		case 0:
			expLen = 1
			expStart = 1
		case 1:
			expLen = 2
			expStart = 1
		case 2:
			expLen = 3
			expStart = 1
		default:
			if len(data) < 2 {
				return 0, errInvalidReal
			}
			expLen = int(data[1])
			expStart = 2
		}
		if expLen == 0 || expStart+expLen > len(data) {
			return 0, errInvalidReal
		}
		exp, err := DecodeInteger(data[expStart : expStart+expLen])
		if err != nil {
			return 0, err
		}
		// Mantissa — remaining bytes, unsigned big-endian.
		mantBytes := data[expStart+expLen:]
		if len(mantBytes) == 0 {
			return 0, errInvalidReal
		}
		var mant uint64
		for _, b := range mantBytes {
			mant = (mant << 8) | uint64(b)
		}
		v := sign * float64(mant) * math.Pow(2.0, float64(int(exp)*baseFactor+scale))
		return v, nil
	}
	return 0, errInvalidReal
}

// DecodeUTF8String reads a BER UTF8String from value bytes.
func DecodeUTF8String(data []byte) string {
	return string(data)
}
