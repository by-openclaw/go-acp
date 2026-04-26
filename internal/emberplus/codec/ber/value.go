package ber

import (
	"encoding/binary"
	"math"
	"math/bits"
)

// --- Encode value types ---

// EncodeBoolean returns BER BOOLEAN value bytes.
//
//	| Offset | Field | Width | Notes                                  |
//	|--------|-------|-------|----------------------------------------|
//	|   0    | flag  |   1   | 0x00 = FALSE; any non-zero = TRUE      |
//	|        |       |       | (this encoder emits 0xFF for TRUE as   |
//	|        |       |       | DER §11.1 mandates)                    |
//
// Spec reference: ITU-T X.690 §8.2 (Encoding of a boolean value).
func EncodeBoolean(v bool) []byte {
	if v {
		return []byte{0xFF}
	}
	return []byte{0x00}
}

// EncodeInteger returns BER INTEGER value bytes (two's complement, big-endian).
// Uses minimum number of octets.
//
//	| Offset | Field    | Width | Notes                                     |
//	|--------|----------|-------|-------------------------------------------|
//	|   0    | MSB      |   1   | sign-significant byte; two's-complement   |
//	|  1..N  | low bytes|  N-1  | big-endian remaining payload              |
//
// Shortest form rule (X.690 §8.3.2): the first nine bits must not all be
// the same — the codec prepends 0x00 for positive values whose top bit
// would otherwise flip the sign, and prepends 0xFF for negative values
// whose top bit would be cleared.
//
// Spec reference: ITU-T X.690 §8.3 (Encoding of an integer value).
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
//
//	| Offset | Field        | Width | Notes                                    |
//	|--------|--------------|-------|------------------------------------------|
//	|   0    | first octet  |   1   | bit7=1 binary; bit6 sign; 5-4 base;      |
//	|        |              |       | 3-2 scale; 1-0 exponent length code      |
//	|   1    | exp-len ext  | 0/1   | only when bits 1-0 = 11 (long form)      |
//	|  1..E  | exponent     |   E   | two's-complement, big-endian             |
//	| E+1..N | mantissa     |   M   | unsigned, big-endian, LSB-trimmed        |
//
// Spec reference: ITU-T X.690 §8.5 (Encoding of a real value).
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

	fbits := math.Float64bits(v)
	sign := byte((fbits >> 63) & 1)
	expRaw := int((fbits >> 52) & 0x7FF)
	frac := fbits & ((1 << 52) - 1)

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

	// X.690 §8.5.6 reads the mantissa N as an unsigned integer:
	//   value = N × 2^F × B^E
	// libember-cpp / libember-slim and every Ember+ viewer in the wild
	// (EmberViewer, EmberPlusView, Lawo VSM) instead read N as a
	// normalised fraction with the binary point implicit after the
	// leading 1 bit:
	//   value = (N / 2^(bitlen(N)-1)) × B^E × 2^F
	// Two interpretations of the same X.690 clauses; the ecosystem
	// reading is universal in practice. Bias the wire exponent by
	// (bitlen(N)-1) so viewers display the value our caller passed in.
	// Verified live 2026-04-26 against EmberViewer v2.40.0.35 + Lawo
	// VSM Studio (issue #68): without this bias, 50.0 displays as
	// 3.125, 100.0 as 6.25, etc.
	exponent += bits.Len64(mantissa) - 1

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
//
//	| Offset | Field   | Width | Notes                            |
//	|--------|---------|-------|----------------------------------|
//	|  0..N  | UTF-8   |   N   | raw UTF-8 code units (no BOM)    |
//
// Spec reference: ITU-T X.690 §8.21 / X.680 §41 (RestrictedString).
func EncodeUTF8String(s string) []byte {
	return []byte(s)
}

// EncodeOctetString returns BER OCTET STRING value bytes.
//
//	| Offset | Field  | Width | Notes                                    |
//	|--------|--------|-------|------------------------------------------|
//	|  0..N  | octets |   N   | raw opaque bytes, copied to new buffer   |
//
// Spec reference: ITU-T X.690 §8.7 (Encoding of an octetstring value).
func EncodeOctetString(b []byte) []byte {
	out := make([]byte, len(b))
	copy(out, b)
	return out
}

// --- Decode value types ---

// DecodeBoolean reads a BER BOOLEAN from value bytes.
//
//	| Offset | Field | Width | Notes                                |
//	|--------|-------|-------|--------------------------------------|
//	|   0    | flag  |   1   | 0x00 = false, anything else = true   |
//
// Spec reference: ITU-T X.690 §8.2 (Encoding of a boolean value).
func DecodeBoolean(data []byte) (bool, error) {
	if len(data) != 1 {
		return false, errTruncated
	}
	return data[0] != 0, nil
}

// DecodeInteger reads a BER INTEGER from value bytes (two's complement).
//
//	| Offset | Field    | Width | Notes                                     |
//	|--------|----------|-------|-------------------------------------------|
//	|   0    | MSB      |   1   | sign bit in bit 7; drives 64-bit sign-pad |
//	|  1..N  | low bytes|  N-1  | big-endian remainder                      |
//
// Rejects encodings longer than 8 octets (errOverflow); the Glow subset
// never emits INTEGER wider than int64 in practice.
//
// Spec reference: ITU-T X.690 §8.3 (Encoding of an integer value).
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
//
// Value-octets layout (binary form, first octet bit 7 = 1):
//
//	| Offset | Field        | Width | Notes                                    |
//	|--------|--------------|-------|------------------------------------------|
//	|   0    | first octet  |   1   | sign/base/scale/exp-len code             |
//	|   1    | exp-len ext  | 0/1   | present iff bits 1-0 of first = 11       |
//	|  1..E  | exponent     |   E   | signed two's-complement, big-endian      |
//	| E+1..N | mantissa     |   M   | unsigned, big-endian                     |
//
// Special first-octet sentinels (§8.5.9): 0x40=+∞, 0x41=-∞, 0x42=NaN,
// empty = 0. Decimal form (bit 7 = 0) is not emitted by Glow and is
// rejected here.
//
// Spec reference: ITU-T X.690 §8.5 (Encoding of a real value).
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
		// Mirror the EncodeReal bias: ecosystem readers (libember,
		// EmberViewer, Lawo VSM) interpret the mantissa N as a normalised
		// fraction with the implicit binary point after the leading 1
		// bit. Subtract (bitlen(N)-1) from the wire exponent so this
		// decoder matches what those peers produce.
		shift := 0
		if mant != 0 {
			shift = bits.Len64(mant) - 1
		}
		v := sign * float64(mant) * math.Pow(2.0, float64(int(exp)*baseFactor+scale-shift))
		return v, nil
	}
	return 0, errInvalidReal
}

// DecodeUTF8String reads a BER UTF8String from value bytes.
//
//	| Offset | Field | Width | Notes                            |
//	|--------|-------|-------|----------------------------------|
//	|  0..N  | UTF-8 |   N   | raw UTF-8 code units (no BOM)    |
//
// Spec reference: ITU-T X.690 §8.21 / X.680 §41 (RestrictedString).
func DecodeUTF8String(data []byte) string {
	return string(data)
}
