package ber

// EncodeLength writes a BER length to bytes.
//   - Short form (0-127): single octet
//   - Long form (128+): 0x80|n followed by n octets big-endian
//
// First length octet layout (X.690 §8.1.3):
//
//	| Bit(s) | Field           | Width | Notes                                |
//	|--------|-----------------|-------|--------------------------------------|
//	|   7    | form            |   1   | 0=short, 1=long                      |
//	|  6..0  | length or count |   7   | short: L itself (0..127);            |
//	|        |                 |       | long: number of following octets (N) |
//	|  1..N  | length octets   |   N   | long-form big-endian length value    |
//
// A single 0x80 first octet signals indefinite-length encoding (the
// value ends with an EOC 0x00 0x00 sentinel — used by constructed
// constructions that stream children).
//
// Spec reference: ITU-T X.690 §8.1.3 (Length octets).
func EncodeLength(length int) []byte {
	if length < 0 {
		// Indefinite length: 0x80
		return []byte{0x80}
	}
	if length <= 127 {
		return []byte{byte(length)}
	}

	// Count bytes needed.
	n := 0
	for v := length; v > 0; v >>= 8 {
		n++
	}

	out := make([]byte, 1+n)
	out[0] = 0x80 | byte(n)
	for i := n; i > 0; i-- {
		out[i] = byte(length)
		length >>= 8
	}
	return out
}

// DecodeLength reads a BER length from buf, returning length and bytes consumed.
// Returns -1 for indefinite length.
//
// Inverse of EncodeLength — dispatches on bit 7 of the first octet:
//
//	| First octet | Form              | Result                              |
//	|-------------|-------------------|-------------------------------------|
//	| 0x00..0x7F  | short definite    | length = first octet (0..127)       |
//	| 0x80        | indefinite        | length = -1 (EOC-terminated value)  |
//	| 0x81..0x84  | long definite (N) | next N octets = big-endian length   |
//	| >= 0x85     | rejected          | errLengthTooLong (cap at 4 octets)  |
//
// Spec reference: ITU-T X.690 §8.1.3 (Length octets).
func DecodeLength(buf []byte) (int, int, error) {
	if len(buf) == 0 {
		return 0, 0, errTruncated
	}

	first := buf[0]

	// Short form.
	if first&0x80 == 0 {
		return int(first), 1, nil
	}

	// Indefinite length.
	if first == 0x80 {
		return -1, 1, nil
	}

	// Long form: n = number of subsequent length octets.
	n := int(first & 0x7F)
	if n > 4 {
		return 0, 0, errLengthTooLong
	}
	if len(buf) < 1+n {
		return 0, 0, errTruncated
	}

	length := 0
	for i := 0; i < n; i++ {
		length = (length << 8) | int(buf[1+i])
	}
	return length, 1 + n, nil
}
