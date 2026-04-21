package probel

import (
	"fmt"
	"strings"
)

// NameLength is the SW-P-08 §3.1.18 "Length of Names Required" enum
// shared by commands 100-108, 114-116. Wire byte on the payload.
type NameLength uint8

const (
	NameLen4  NameLength = 0x00 // 4-char names
	NameLen8  NameLength = 0x01 // 8-char names
	NameLen12 NameLength = 0x02 // 12-char names
	NameLen16 NameLength = 0x03 // 16-char names (rx 117 UPDATE NAME + UMD labels)
)

// Bytes returns the fixed character width (4, 8, 12, 16) for this
// length enum. Unknown enum values fall back to 8.
func (n NameLength) Bytes() int {
	switch n {
	case NameLen4:
		return 4
	case NameLen8:
		return 8
	case NameLen12:
		return 12
	case NameLen16:
		return 16
	}
	return 8
}

// MaxNamesPerMessage caps how many names fit in one SW-P-08 response
// frame (spec §3.2.19: "134-byte DATA field, thus max 32 × 4-char, 16 ×
// 8-char, 10 × 12-char, or 8 × 16-char names"). Callers that need more
// names must paginate.
func (n NameLength) MaxNamesPerMessage() int {
	switch n {
	case NameLen4:
		return 32
	case NameLen8:
		return 16
	case NameLen12:
		return 10
	case NameLen16:
		return 8
	}
	return 16
}

// packName returns a fixed-width byte slice with the ASCII of s, right-
// padded with spaces to exactly width bytes. Longer strings are
// truncated. Non-ASCII runes collapse to '?' — SW-P-08 names are 8-bit
// ASCII per the spec examples.
func packName(s string, width int) []byte {
	out := make([]byte, width)
	for i := 0; i < width; i++ {
		out[i] = ' '
	}
	if s == "" {
		return out
	}
	for i, r := 0, 0; r < len(s) && i < width; r++ {
		b := s[r]
		if b >= 0x20 && b <= 0x7E {
			out[i] = b
		} else {
			out[i] = '?'
		}
		i++
	}
	return out
}

// unpackName decodes a fixed-width name, right-trimming spaces and NULs.
func unpackName(b []byte) string {
	return strings.TrimRight(strings.TrimRight(string(b), "\x00"), " ")
}

// encodeMatrixLevel packs (matrix, level) into one byte per SW-P-08
// §3.1.2 — Matrix in bits 4-7, Level in bits 0-3. Both capped at 15.
func encodeMatrixLevel(m, l uint8) byte {
	return (m << 4) | (l & 0x0F)
}

// decodeMatrixLevel is the inverse of encodeMatrixLevel.
func decodeMatrixLevel(b byte) (m, l uint8) {
	return b >> 4, b & 0x0F
}

// validateNameLength returns nil if n is one of NameLen{4,8,12}.
// Name-length 0x03 (16-char) is rejected here — it's only valid for
// rx 117 UPDATE NAME + UMD commands, which use validateNameLengthExt.
func validateNameLength(n NameLength) error {
	switch n {
	case NameLen4, NameLen8, NameLen12:
		return nil
	}
	return fmt.Errorf("probel: invalid NameLength %#x (must be 0x00, 0x01, or 0x02)", byte(n))
}

// validateNameLengthExt is like validateNameLength but also accepts
// NameLen16 — used by rx 117 and the UMD commands.
func validateNameLengthExt(n NameLength) error {
	switch n {
	case NameLen4, NameLen8, NameLen12, NameLen16:
		return nil
	}
	return fmt.Errorf("probel: invalid NameLength %#x (must be 0x00, 0x01, 0x02, or 0x03)", byte(n))
}
