package ber

// EncodeLength writes a BER length to bytes.
//   - Short form (0-127): single octet
//   - Long form (128+): 0x80|n followed by n octets big-endian
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
