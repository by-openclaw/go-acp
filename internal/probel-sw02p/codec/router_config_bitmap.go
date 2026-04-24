package codec

import "math/bits"

// RouterConfigLevelMapBytes is the fixed size of the 28-bit "levels
// bit map" prefix carried by both tx 076 ROUTER CONFIGURATION
// RESPONSE - 1 (§3.2.58) and tx 077 ROUTER CONFIGURATION RESPONSE - 2
// (§3.2.59). Layout:
//
//	| Byte | Bits 0-6 meaning                |
//	|------|---------------------------------|
//	|  1   | levels 21-27                    |
//	|  2   | levels 14-20                    |
//	|  3   | levels 7-13                     |
//	|  4   | levels 0-6                      |
//
// Every byte's bit 7 is always 0. Each set bit signals "this level is
// present, and a per-level entry follows in the same byte-position
// order (bit 0 first, then bit 1, etc.)".
const RouterConfigLevelMapBytes = 4

// RouterConfigMaxLevels is the total number of level bits available
// across the four 7-bit bytes (4 × 7 = 28).
const RouterConfigMaxLevels = 28

// packLevelMap serialises the 28-bit bit map into 4 bytes per
// §3.2.58. Bits 28-31 of in are ignored.
func packLevelMap(in uint32) [RouterConfigLevelMapBytes]byte {
	return [RouterConfigLevelMapBytes]byte{
		byte((in >> 21) & 0x7F), // levels 21-27
		byte((in >> 14) & 0x7F), // levels 14-20
		byte((in >> 7) & 0x7F),  // levels 7-13
		byte(in & 0x7F),         // levels 0-6
	}
}

// unpackLevelMap reverses packLevelMap. Bits 7 of each input byte
// are masked off — spec mandates them to be 0 but we tolerate peers
// that set them.
func unpackLevelMap(b [RouterConfigLevelMapBytes]byte) uint32 {
	return (uint32(b[0]&0x7F) << 21) |
		(uint32(b[1]&0x7F) << 14) |
		(uint32(b[2]&0x7F) << 7) |
		uint32(b[3]&0x7F)
}

// levelMapCount returns the number of set bits in the 28-bit map —
// the number of per-level entries that follow the map in tx 076 /
// tx 077. Bits 28-31 are masked off before counting.
func levelMapCount(m uint32) int {
	return bits.OnesCount32(m & 0x0FFFFFFF)
}
