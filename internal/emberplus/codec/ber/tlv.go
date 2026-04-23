package ber

// TLV is one decoded BER Tag-Length-Value element.
type TLV struct {
	Tag      Tag
	Value    []byte // raw value bytes (for primitive) or nil (for constructed)
	Children []TLV  // child TLVs (for constructed) or nil (for primitive)
}

// EncodeTLV encodes a TLV element to bytes.
//
// Every BER TLV is the concatenation of three fields:
//
//	| Tag | Field   | Encoding                | Notes                      |
//	|-----|---------|-------------------------|----------------------------|
//	|  T  | tag     | EncodeTag               | 1+ octets, class+form+num  |
//	|  L  | length  | EncodeLength            | 1+ octets, short or long   |
//	|  V  | value   | raw / recursive(TLV...) | primitive: t.Value bytes;  |
//	|     |         |                         | constructed: concatenated  |
//	|     |         |                         | EncodeTLV(child) bodies    |
//
// Spec reference: ITU-T X.690 §8.1.1 (General rules for BER).
func EncodeTLV(t TLV) []byte {
	tag := EncodeTag(t.Tag)

	if t.Tag.Constructed {
		// Encode children, concatenate, wrap with length.
		var content []byte
		for _, child := range t.Children {
			content = append(content, EncodeTLV(child)...)
		}
		length := EncodeLength(len(content))
		out := make([]byte, 0, len(tag)+len(length)+len(content))
		out = append(out, tag...)
		out = append(out, length...)
		out = append(out, content...)
		return out
	}

	// Primitive: use Value directly.
	length := EncodeLength(len(t.Value))
	out := make([]byte, 0, len(tag)+len(length)+len(t.Value))
	out = append(out, tag...)
	out = append(out, length...)
	out = append(out, t.Value...)
	return out
}

// DecodeTLV reads one TLV element from buf. Returns the TLV and total
// bytes consumed (tag + length + value).
//
// Layout parsed (inverse of EncodeTLV):
//
//	| Tag | Field   | Encoding     | Notes                                |
//	|-----|---------|--------------|--------------------------------------|
//	|  T  | tag     | DecodeTag    | identifier octets (1+)               |
//	|  L  | length  | DecodeLength | length octets (1+); -1 = indefinite  |
//	|  V  | value   | L bytes      | primitive: raw; constructed: TLVs    |
//	|     |         |              | until L reached OR EOC 0x00 0x00     |
//	|     |         |              | sentinel for indefinite length       |
//
// Spec reference: ITU-T X.690 §8.1.1 (General rules for BER).
func DecodeTLV(buf []byte) (TLV, int, error) {
	if len(buf) == 0 {
		return TLV{}, 0, errTruncated
	}

	tag, tagLen, err := DecodeTag(buf)
	if err != nil {
		return TLV{}, 0, err
	}

	length, lenLen, err := DecodeLength(buf[tagLen:])
	if err != nil {
		return TLV{}, 0, err
	}

	headerLen := tagLen + lenLen

	// Indefinite length.
	if length < 0 {
		// Read children until EOC (0x00 0x00).
		t := TLV{Tag: tag}
		pos := headerLen
		for {
			if pos+2 > len(buf) {
				return TLV{}, 0, errTruncated
			}
			// Check for EOC.
			if buf[pos] == 0x00 && buf[pos+1] == 0x00 {
				pos += 2
				break
			}
			child, n, cerr := DecodeTLV(buf[pos:])
			if cerr != nil {
				return TLV{}, 0, cerr
			}
			t.Children = append(t.Children, child)
			pos += n
		}
		return t, pos, nil
	}

	// Definite length.
	if headerLen+length > len(buf) {
		return TLV{}, 0, errTruncated
	}

	t := TLV{Tag: tag}
	content := buf[headerLen : headerLen+length]

	if tag.Constructed {
		// Parse children from content.
		pos := 0
		for pos < len(content) {
			child, n, cerr := DecodeTLV(content[pos:])
			if cerr != nil {
				return TLV{}, 0, cerr
			}
			t.Children = append(t.Children, child)
			pos += n
		}
	} else {
		t.Value = content
	}

	return t, headerLen + length, nil
}

// DecodeAll reads all TLV elements from buf.
func DecodeAll(buf []byte) ([]TLV, error) {
	var result []TLV
	pos := 0
	for pos < len(buf) {
		t, n, err := DecodeTLV(buf[pos:])
		if err != nil {
			return nil, err
		}
		result = append(result, t)
		pos += n
	}
	return result, nil
}

// Helper constructors for common TLV patterns.

// Primitive creates a primitive (non-constructed) TLV.
func Primitive(class Class, tag uint32, value []byte) TLV {
	return TLV{
		Tag:   Tag{Class: class, Constructed: false, Number: tag},
		Value: value,
	}
}

// Constructed creates a constructed TLV with children.
func Constructed(class Class, tag uint32, children ...TLV) TLV {
	return TLV{
		Tag:      Tag{Class: class, Constructed: true, Number: tag},
		Children: children,
	}
}

// Universal helpers.

// Integer creates a UNIVERSAL INTEGER TLV.
func Integer(v int64) TLV {
	return Primitive(ClassUniversal, TagInteger, EncodeInteger(v))
}

// Boolean creates a UNIVERSAL BOOLEAN TLV.
func Boolean(v bool) TLV {
	return Primitive(ClassUniversal, TagBoolean, EncodeBoolean(v))
}

// Real creates a UNIVERSAL REAL TLV.
func Real(v float64) TLV {
	return Primitive(ClassUniversal, TagReal, EncodeReal(v))
}

// UTF8 creates a UNIVERSAL UTF8String TLV.
func UTF8(s string) TLV {
	return Primitive(ClassUniversal, TagUTF8String, EncodeUTF8String(s))
}

// OctetStr creates a UNIVERSAL OCTET STRING TLV.
func OctetStr(b []byte) TLV {
	return Primitive(ClassUniversal, TagOctetString, EncodeOctetString(b))
}

// Sequence creates a UNIVERSAL SEQUENCE TLV.
func Sequence(children ...TLV) TLV {
	return Constructed(ClassUniversal, TagSequence, children...)
}

// Set creates a UNIVERSAL SET TLV.
func Set(children ...TLV) TLV {
	return Constructed(ClassUniversal, TagSet, children...)
}

// Context helpers for Glow application tags.

// ContextPrimitive creates a CONTEXT primitive TLV.
func ContextPrimitive(tag uint32, value []byte) TLV {
	return Primitive(ClassContext, tag, value)
}

// ContextConstructed creates a CONTEXT constructed TLV.
func ContextConstructed(tag uint32, children ...TLV) TLV {
	return Constructed(ClassContext, tag, children...)
}

// RelOID creates a UNIVERSAL RELATIVE-OID TLV.
func RelOID(data []byte) TLV {
	return Primitive(ClassUniversal, TagRelativeOID, data)
}

// AppConstructed creates an APPLICATION constructed TLV.
func AppConstructed(tag uint32, children ...TLV) TLV {
	return Constructed(ClassApplication, tag, children...)
}
