package acp2

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"
)

// Property is one decoded ACP2 property header plus its value.
type Property struct {
	PID   uint8  // property id (1-20)
	VType uint8  // byte 1: vtype for numeric, or inline small value
	PLen  uint16 // total bytes including the 4-byte header, excluding padding
	Data  []byte // raw value bytes (PLen - 4 bytes); nil when PLen <= 4
}

// propertyPadding returns the alignment padding after a property.
// ACP2 aligns each property to a 4-byte boundary: skip (4 - (plen % 4)) % 4.
func propertyPadding(plen uint16) int {
	return int((4 - (plen % 4)) % 4)
}

// DecodeProperties parses a sequence of ACP2 property headers from a byte
// buffer. Each property is 4-byte aligned with padding after.
func DecodeProperties(data []byte) ([]Property, error) {
	var props []Property
	offset := 0

	for offset+4 <= len(data) {
		pid := data[offset]
		vtype := data[offset+1]
		plen := binary.BigEndian.Uint16(data[offset+2 : offset+4])

		if plen < 4 {
			return nil, fmt.Errorf("acp2: property pid=%d plen=%d < 4", pid, plen)
		}
		if offset+int(plen) > len(data) {
			return nil, fmt.Errorf("acp2: property pid=%d plen=%d exceeds buffer (offset=%d, buflen=%d)",
				pid, plen, offset, len(data))
		}

		var propData []byte
		if plen > 4 {
			propData = make([]byte, plen-4)
			copy(propData, data[offset+4:offset+int(plen)])
		}

		props = append(props, Property{
			PID:   pid,
			VType: vtype,
			PLen:  plen,
			Data:  propData,
		})

		// Advance past property + alignment padding.
		pad := propertyPadding(plen)
		offset += int(plen) + pad
	}

	return props, nil
}

// EncodeProperty serialises one Property into wire bytes including padding.
func EncodeProperty(p *Property) ([]byte, error) {
	if p == nil {
		return nil, fmt.Errorf("acp2: encode nil property")
	}

	dataLen := len(p.Data)
	plen := uint16(4 + dataLen)
	pad := propertyPadding(plen)
	buf := make([]byte, int(plen)+pad)

	buf[0] = p.PID
	buf[1] = p.VType
	binary.BigEndian.PutUint16(buf[2:4], plen)
	if dataLen > 0 {
		copy(buf[4:], p.Data)
	}
	// Padding bytes are zero (already zeroed by make).
	return buf, nil
}

// EncodeProperties serialises multiple properties into a single buffer.
func EncodeProperties(props []Property) ([]byte, error) {
	var out []byte
	for i := range props {
		b, err := EncodeProperty(&props[i])
		if err != nil {
			return nil, err
		}
		out = append(out, b...)
	}
	return out, nil
}

// ---- Value extraction helpers ----

// PropertyU32 extracts a u32 value from a property's Data.
func PropertyU32(p *Property) (uint32, error) {
	if len(p.Data) < 4 {
		return 0, fmt.Errorf("acp2: pid=%d data too short for u32: %d", p.PID, len(p.Data))
	}
	return binary.BigEndian.Uint32(p.Data[0:4]), nil
}

// PropertyU16 extracts a u16 value from a property's Data.
func PropertyU16(p *Property) (uint16, error) {
	if len(p.Data) < 2 {
		return 0, fmt.Errorf("acp2: pid=%d data too short for u16: %d", p.PID, len(p.Data))
	}
	return binary.BigEndian.Uint16(p.Data[0:2]), nil
}

// PropertyString extracts a null-terminated UTF-8 string from property Data.
func PropertyString(p *Property) string {
	if p.Data == nil {
		return ""
	}
	s := string(p.Data)
	// Strip null terminator if present.
	if idx := strings.IndexByte(s, 0); idx >= 0 {
		s = s[:idx]
	}
	return s
}

// PropertyChildren extracts u32[] child obj-ids from a pid=14 property.
func PropertyChildren(p *Property) ([]uint32, error) {
	if len(p.Data)%4 != 0 {
		return nil, fmt.Errorf("acp2: children data length %d not multiple of 4", len(p.Data))
	}
	n := len(p.Data) / 4
	ids := make([]uint32, n)
	for i := 0; i < n; i++ {
		ids[i] = binary.BigEndian.Uint32(p.Data[i*4 : i*4+4])
	}
	return ids, nil
}

// ACP2OptionSize is the fixed on-wire size of one enum option per spec
// §5.4 pid 15: 4-byte u32 index + 68-byte NUL-padded UTF-8 name = 72 bytes.
const ACP2OptionSize = 72

// PropertyOptions extracts enum option labels (ordered by wire position)
// from a pid=15 property. Fixed 72-byte stride per spec §5.4.
func PropertyOptions(p *Property) []string {
	if len(p.Data) < ACP2OptionSize {
		return nil
	}
	n := len(p.Data) / ACP2OptionSize
	labels := make([]string, 0, n)
	for i := 0; i < n; i++ {
		off := i * ACP2OptionSize
		labels = append(labels, trimZero(p.Data[off+4:off+ACP2OptionSize]))
	}
	return labels
}

// PropertyOptionsMap extracts enum options as a map of index → label.
// Fixed 72-byte stride per spec §5.4 pid 15.
func PropertyOptionsMap(p *Property) map[uint32]string {
	if len(p.Data) < ACP2OptionSize {
		return nil
	}
	n := len(p.Data) / ACP2OptionSize
	m := make(map[uint32]string, n)
	for i := 0; i < n; i++ {
		off := i * ACP2OptionSize
		idx := binary.BigEndian.Uint32(p.Data[off : off+4])
		m[idx] = trimZero(p.Data[off+4 : off+ACP2OptionSize])
	}
	return m
}

// trimZero returns the UTF-8 prefix of b up to the first NUL byte (or
// the full slice if none). Used for fixed-width NUL-padded name slots.
func trimZero(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}

// PropertyEventMessages extracts the two event message strings from pid=19.
// Format: two null-terminated strings back to back.
func PropertyEventMessages(p *Property) (onMsg, offMsg string) {
	if p.Data == nil {
		return "", ""
	}
	s := string(p.Data)
	parts := strings.SplitN(s, "\x00", 3)
	if len(parts) >= 1 {
		onMsg = parts[0]
	}
	if len(parts) >= 2 {
		offMsg = parts[1]
	}
	return
}

// DecodeNumericValue decodes a numeric property value based on its NumberType.
// Returns (intVal, uintVal, floatVal) with only the relevant one set.
func DecodeNumericValue(nt NumberType, data []byte) (int64, uint64, float64, error) {
	switch nt {
	case NumTypeS8:
		if len(data) < 4 {
			return 0, 0, 0, fmt.Errorf("acp2: s8 data too short")
		}
		v := int32(binary.BigEndian.Uint32(data[0:4]))
		return int64(int8(v)), 0, 0, nil
	case NumTypeS16:
		if len(data) < 4 {
			return 0, 0, 0, fmt.Errorf("acp2: s16 data too short")
		}
		v := int32(binary.BigEndian.Uint32(data[0:4]))
		return int64(int16(v)), 0, 0, nil
	case NumTypeS32:
		if len(data) < 4 {
			return 0, 0, 0, fmt.Errorf("acp2: s32 data too short")
		}
		v := int32(binary.BigEndian.Uint32(data[0:4]))
		return int64(v), 0, 0, nil
	case NumTypeS64:
		if len(data) < 8 {
			return 0, 0, 0, fmt.Errorf("acp2: s64 data too short")
		}
		v := int64(binary.BigEndian.Uint64(data[0:8]))
		return v, 0, 0, nil
	case NumTypeU8:
		if len(data) < 4 {
			return 0, 0, 0, fmt.Errorf("acp2: u8 data too short")
		}
		v := binary.BigEndian.Uint32(data[0:4])
		return 0, uint64(uint8(v)), 0, nil
	case NumTypeU16:
		if len(data) < 4 {
			return 0, 0, 0, fmt.Errorf("acp2: u16 data too short")
		}
		v := binary.BigEndian.Uint32(data[0:4])
		return 0, uint64(uint16(v)), 0, nil
	case NumTypeU32:
		if len(data) < 4 {
			return 0, 0, 0, fmt.Errorf("acp2: u32 data too short")
		}
		v := binary.BigEndian.Uint32(data[0:4])
		return 0, uint64(v), 0, nil
	case NumTypeU64:
		if len(data) < 8 {
			return 0, 0, 0, fmt.Errorf("acp2: u64 data too short")
		}
		v := binary.BigEndian.Uint64(data[0:8])
		return 0, v, 0, nil
	case NumTypeFloat:
		if len(data) < 4 {
			return 0, 0, 0, fmt.Errorf("acp2: float data too short")
		}
		bits := binary.BigEndian.Uint32(data[0:4])
		return 0, 0, float64(math.Float32frombits(bits)), nil
	case NumTypePreset:
		if len(data) < 4 {
			return 0, 0, 0, fmt.Errorf("acp2: preset data too short")
		}
		v := binary.BigEndian.Uint32(data[0:4])
		return 0, uint64(v), 0, nil
	case NumTypeIPv4:
		if len(data) < 4 {
			return 0, 0, 0, fmt.Errorf("acp2: ipv4 data too short")
		}
		v := binary.BigEndian.Uint32(data[0:4])
		return 0, uint64(v), 0, nil
	default:
		return 0, 0, 0, fmt.Errorf("acp2: unknown number type %d", nt)
	}
}

// EncodeNumericValue encodes a value to wire bytes based on NumberType.
func EncodeNumericValue(nt NumberType, intVal int64, uintVal uint64, floatVal float64) ([]byte, error) {
	switch nt {
	case NumTypeS8, NumTypeS16, NumTypeS32:
		buf := make([]byte, 4)
		binary.BigEndian.PutUint32(buf, uint32(int32(intVal)))
		return buf, nil
	case NumTypeS64:
		buf := make([]byte, 8)
		binary.BigEndian.PutUint64(buf, uint64(intVal))
		return buf, nil
	case NumTypeU8, NumTypeU16, NumTypeU32, NumTypePreset, NumTypeIPv4:
		buf := make([]byte, 4)
		binary.BigEndian.PutUint32(buf, uint32(uintVal))
		return buf, nil
	case NumTypeU64:
		buf := make([]byte, 8)
		binary.BigEndian.PutUint64(buf, uintVal)
		return buf, nil
	case NumTypeFloat:
		buf := make([]byte, 4)
		binary.BigEndian.PutUint32(buf, math.Float32bits(float32(floatVal)))
		return buf, nil
	default:
		return nil, fmt.Errorf("acp2: cannot encode number type %d", nt)
	}
}

// EncodeStringValue encodes a string value with null terminator and padding.
func EncodeStringValue(s string) []byte {
	raw := append([]byte(s), 0)
	return raw
}

// MakeValueProperty builds a Property for pid=8 (value) given a NumberType
// and the encoded data bytes.
func MakeValueProperty(pid uint8, nt NumberType, data []byte) Property {
	plen := uint16(4 + len(data))
	return Property{
		PID:   pid,
		VType: uint8(nt),
		PLen:  plen,
		Data:  data,
	}
}

// MakeStringProperty builds a string Property (for pid=8 string values).
func MakeStringProperty(pid uint8, s string) Property {
	raw := EncodeStringValue(s)
	plen := uint16(4 + len(raw))
	return Property{
		PID:   pid,
		VType: uint8(NumTypeString),
		PLen:  plen,
		Data:  raw,
	}
}
