// Package ber implements ASN.1 BER (Basic Encoding Rules) for the
// Ember+ Glow subset. Not a general-purpose ASN.1 library — only the
// ~15 tag types that Glow actually uses are implemented.
//
// Reference: ITU-T X.690 (ASN.1 encoding rules)
package ber

// Class is the ASN.1 tag class (bits 7-8 of the tag octet).
type Class uint8

const (
	ClassUniversal   Class = 0 // 00
	ClassApplication Class = 1 // 01
	ClassContext      Class = 2 // 10
	ClassPrivate      Class = 3 // 11
)

func (c Class) String() string {
	switch c {
	case ClassUniversal:
		return "UNIVERSAL"
	case ClassApplication:
		return "APPLICATION"
	case ClassContext:
		return "CONTEXT"
	case ClassPrivate:
		return "PRIVATE"
	default:
		return "UNKNOWN"
	}
}

// Universal tag numbers (ASN.1 X.680).
const (
	TagEOC         uint32 = 0
	TagBoolean     uint32 = 1
	TagInteger     uint32 = 2
	TagBitString   uint32 = 3
	TagOctetString uint32 = 4
	TagNull        uint32 = 5
	TagOID         uint32 = 6
	TagRelativeOID uint32 = 13
	TagReal        uint32 = 9
	TagEnum        uint32 = 10
	TagUTF8String  uint32 = 12
	TagSequence    uint32 = 16
	TagSet         uint32 = 17
)

// Tag is a decoded BER tag.
type Tag struct {
	Class       Class
	Constructed bool   // true = contains child TLVs
	Number      uint32 // tag number
}

// EncodeTag writes a BER tag to bytes.
//
// First identifier octet layout (X.690 §8.1.2):
//
//	| Bit(s) | Field       | Width | Notes                                   |
//	|--------|-------------|-------|-----------------------------------------|
//	|  7..6  | class       |   2   | 00=UNIV, 01=APP, 10=CTX, 11=PRIV        |
//	|   5    | constructed |   1   | 0=primitive, 1=constructed              |
//	|  4..0  | tag number  |   5   | short form (0..30); 31 = long-form flag |
//
// When the tag number is >= 31 the low 5 bits are set to 0x1F and the
// real number follows as base-128 octets (MSB first, continuation bit 7
// set on every octet except the last).
//
// Spec reference: ITU-T X.690 §8.1.2 (Identifier octets).
func EncodeTag(t Tag) []byte {
	first := byte(t.Class) << 6
	if t.Constructed {
		first |= 0x20
	}

	if t.Number <= 30 {
		// Short form: tag number fits in bits 0-4.
		first |= byte(t.Number)
		return []byte{first}
	}

	// Long form: bits 0-4 = 0x1F, then multi-byte tag number.
	first |= 0x1F
	return append([]byte{first}, encodeBase128(t.Number)...)
}

// DecodeTag reads a BER tag from buf, returning the tag and bytes consumed.
//
// Inverse of EncodeTag — parses the first identifier octet:
//
//	| Bit(s) | Field       | Width | Notes                                   |
//	|--------|-------------|-------|-----------------------------------------|
//	|  7..6  | class       |   2   | 00=UNIV, 01=APP, 10=CTX, 11=PRIV        |
//	|   5    | constructed |   1   | 0=primitive, 1=constructed              |
//	|  4..0  | tag number  |   5   | short form or 0x1F sentinel = long form |
//
// When the sentinel is seen, decodeBase128 reads the continuation-bit
// chain that carries the real tag number.
//
// Spec reference: ITU-T X.690 §8.1.2 (Identifier octets).
func DecodeTag(buf []byte) (Tag, int, error) {
	if len(buf) == 0 {
		return Tag{}, 0, errTruncated
	}

	first := buf[0]
	t := Tag{
		Class:       Class(first >> 6),
		Constructed: first&0x20 != 0,
	}

	num := uint32(first & 0x1F)
	if num < 31 {
		// Short form.
		t.Number = num
		return t, 1, nil
	}

	// Long form: read base-128 encoded tag number.
	n, consumed, err := decodeBase128(buf[1:])
	if err != nil {
		return Tag{}, 0, err
	}
	t.Number = n
	return t, 1 + consumed, nil
}

// encodeBase128 encodes a value in base-128 (high bit = continuation).
func encodeBase128(val uint32) []byte {
	if val == 0 {
		return []byte{0}
	}
	// Collect 7-bit groups, MSB first.
	var parts []byte
	for val > 0 {
		parts = append([]byte{byte(val & 0x7F)}, parts...)
		val >>= 7
	}
	// Set high bit on all but last byte.
	for i := 0; i < len(parts)-1; i++ {
		parts[i] |= 0x80
	}
	return parts
}

// decodeBase128 reads a base-128 encoded value, returning value and bytes consumed.
func decodeBase128(buf []byte) (uint32, int, error) {
	var val uint32
	for i, b := range buf {
		val = (val << 7) | uint32(b&0x7F)
		if b&0x80 == 0 {
			return val, i + 1, nil
		}
		if i > 4 {
			return 0, 0, errTagTooLong
		}
	}
	return 0, 0, errTruncated
}
