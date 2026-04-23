package acp1

import (
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"

	"acp/internal/protocol"
)

// ValueCodec encodes and decodes the "value bytes" that ACP1 getValue and
// setValue methods carry inside MDATA. Those bytes are the raw typed
// value only — no object-type preamble, no num_properties preamble —
// because getValue/setValue are scoped to an already-known object.
//
// Wire formats per spec §"Methods" p. 28 and per-type tables pp. 21–27:
//
//	Integer   int16 big-endian   (2 bytes)
//	Long      int32 big-endian   (4 bytes)
//	Byte      uint8              (1 byte)
//	Float     float32 big-endian (4 bytes, IEEE-754)
//	IPAddr    uint32 big-endian  (4 bytes, stored as 4 octets)
//	Enum      uint8              (1 byte index into the item list)
//	String    NUL-terminated bytes, trailing NUL OPTIONAL in replies
//
// Alarm and Frame values are not handled here — their getValue semantics
// carry compound payloads best decoded via getObject and the property
// decoder in property.go.

// DecodeValueBytes converts the raw value bytes from a getValue/setValue
// reply into a typed protocol.Value, using the cached Object to resolve
// type-specific context (enum items, integer width, etc.).
//
// The fine-grained ACP1 type is passed in explicitly because the widened
// protocol.ValueKind cannot distinguish Integer (int16) from Long (int32)
// — both surface as KindInt to the rest of the system.
//
// Per-type raw-value layout (no type/num_props prefix — scoped by object):
//
//	| ACP1 type | Width | Wire layout                                    |
//	|-----------|-------|------------------------------------------------|
//	| Integer   |   2   | int16 big-endian                               |
//	| Long      |   4   | int32 big-endian                               |
//	| Byte      |   1   | u8                                             |
//	| Float     |   4   | IEEE-754 float32 big-endian                    |
//	| IPAddr    |   4   | uint32 big-endian (4 octets a.b.c.d)           |
//	| Enum      |   1   | u8 index into item list                        |
//	| String    |   ?   | NUL-terminated ASCII; terminator optional      |
//	| Alarm     |   1   | u8 priority (0 = disabled)                     |
//	| Frame     |  1+N  | u8 num_slots then N × u8 slot_status           |
//	| Root/File |   ?   | raw bytes pass-through                         |
//
// Spec reference: AXON-ACP_v1_4.pdf §"Methods" p. 28 and
// §"Objects by Type" pp. 21–27.
func DecodeValueBytes(obj protocol.Object, acpType ObjectType, raw []byte) (protocol.Value, error) {
	v := protocol.Value{
		Kind: obj.Kind,
		Raw:  append([]byte(nil), raw...),
	}
	switch acpType {
	case TypeInteger:
		if len(raw) < 2 {
			return v, fmt.Errorf("acp1 decode integer: need 2 bytes, got %d", len(raw))
		}
		v.Int = int64(int16(binary.BigEndian.Uint16(raw)))
		return v, nil

	case TypeLong:
		if len(raw) < 4 {
			return v, fmt.Errorf("acp1 decode long: need 4 bytes, got %d", len(raw))
		}
		v.Int = int64(int32(binary.BigEndian.Uint32(raw)))
		return v, nil

	case TypeByte:
		if len(raw) < 1 {
			return v, fmt.Errorf("acp1 decode byte: need 1 byte, got %d", len(raw))
		}
		v.Uint = uint64(raw[0])
		return v, nil

	case TypeFloat:
		if len(raw) < 4 {
			return v, fmt.Errorf("acp1 decode float: need 4 bytes, got %d", len(raw))
		}
		v.Float = float64(math.Float32frombits(binary.BigEndian.Uint32(raw)))
		return v, nil

	case TypeIPAddr:
		if len(raw) < 4 {
			return v, fmt.Errorf("acp1 decode ipaddr: need 4 bytes, got %d", len(raw))
		}
		u := binary.BigEndian.Uint32(raw)
		v.Uint = uint64(u)
		v.IPAddr = [4]byte{
			byte(u >> 24), byte(u >> 16), byte(u >> 8), byte(u),
		}
		return v, nil

	case TypeEnum:
		if len(raw) < 1 {
			return v, fmt.Errorf("acp1 decode enum: need 1 byte, got %d", len(raw))
		}
		v.Enum = raw[0]
		v.Uint = uint64(raw[0])
		// Resolve the item name for display if the walker captured items.
		if int(raw[0]) < len(obj.EnumItems) {
			v.Str = obj.EnumItems[raw[0]]
		}
		return v, nil

	case TypeString:
		// Strings on the wire are NUL-terminated, but some replies omit
		// the trailing NUL. Strip at the first NUL if present, otherwise
		// take everything.
		end := len(raw)
		for i, b := range raw {
			if b == 0 {
				end = i
				break
			}
		}
		v.Str = string(raw[:end])
		return v, nil

	case TypeAlarm:
		// getValue on an Alarm returns a single byte = the current
		// priority (0 = disabled, 1..255 = enabled with that priority).
		// Surface it as a Uint so formatValue can render it cleanly.
		if len(raw) < 1 {
			return v, fmt.Errorf("acp1 decode alarm: need 1 byte, got %d", len(raw))
		}
		v.Kind = protocol.KindUint
		v.Uint = uint64(raw[0])
		return v, nil

	case TypeFrame:
		// Frame-status announcements and getValue replies both carry
		// [num_slots, status_0, status_1, ...] per spec p. 24. Decode
		// the byte array into the structured SlotStatus slice so
		// callers don't have to parse raw bytes on every event.
		if len(raw) < 1 {
			return v, fmt.Errorf("acp1 decode frame: empty value")
		}
		num := int(raw[0])
		if len(raw) < 1+num {
			return v, fmt.Errorf("acp1 decode frame: %d slots declared, %d status bytes",
				num, len(raw)-1)
		}
		v.Kind = protocol.KindFrame
		v.SlotStatus = make([]protocol.SlotStatus, num)
		for i := 0; i < num; i++ {
			v.SlotStatus[i] = protocol.SlotStatus(raw[1+i])
		}
		return v, nil

	case TypeRoot, TypeFile, TypeReserved:
		// Other compound types — leave as raw bytes.
		v.Kind = protocol.KindRaw
		return v, nil
	}
	return v, fmt.Errorf("acp1 decode: unsupported type %d", acpType)
}

// EncodeValueBytes produces the wire bytes for a setValue method, using
// the object's declared ACP1 type and (for Enum / String) its captured
// metadata to validate the input.
//
// The caller provides the desired value in the protocol.Value field that
// matches the object's kind: Int for integer/long, Uint for byte,
// Float for float, Str for string/enum/ipaddr (with permissive parsing).
// This is the inverse of DecodeValueBytes with a forgiving input policy:
// Value.Str takes precedence when present so CLI users can type
// `--value "On"` or `--value "192.168.1.5"` regardless of object kind.
//
// Per-type raw-value layout produced on the wire:
//
//	| ACP1 type | Width  | Wire layout                                   |
//	|-----------|--------|-----------------------------------------------|
//	| Integer   |   2    | int16 big-endian; range [-32768..32767]       |
//	| Long      |   4    | int32 big-endian; range [MinInt32..MaxInt32]  |
//	| Byte      |   1    | u8; range [0..255]                            |
//	| Float     |   4    | IEEE-754 float32 big-endian                   |
//	| IPAddr    |   4    | uint32 big-endian (4 octets a.b.c.d)          |
//	| Enum      |   1    | u8 index (validated against item list)        |
//	| String    | len+1  | bytes + NUL terminator; bounded by MaxLen     |
//
// Spec reference: AXON-ACP_v1_4.pdf §"Methods" p. 28 and
// §"Objects by Type" pp. 21–27.
func EncodeValueBytes(obj protocol.Object, acpType ObjectType, val protocol.Value) ([]byte, error) {
	switch acpType {
	case TypeInteger:
		n, err := coerceInt(val, -32768, 32767)
		if err != nil {
			return nil, fmt.Errorf("acp1 encode integer: %w", err)
		}
		out := make([]byte, 2)
		binary.BigEndian.PutUint16(out, uint16(int16(n)))
		return out, nil

	case TypeLong:
		n, err := coerceInt(val, math.MinInt32, math.MaxInt32)
		if err != nil {
			return nil, fmt.Errorf("acp1 encode long: %w", err)
		}
		out := make([]byte, 4)
		binary.BigEndian.PutUint32(out, uint32(int32(n)))
		return out, nil

	case TypeByte:
		n, err := coerceUint(val, 255)
		if err != nil {
			return nil, fmt.Errorf("acp1 encode byte: %w", err)
		}
		return []byte{byte(n)}, nil

	case TypeFloat:
		f, err := coerceFloat(val)
		if err != nil {
			return nil, fmt.Errorf("acp1 encode float: %w", err)
		}
		out := make([]byte, 4)
		binary.BigEndian.PutUint32(out, math.Float32bits(float32(f)))
		return out, nil

	case TypeIPAddr:
		u, err := coerceIP(val)
		if err != nil {
			return nil, fmt.Errorf("acp1 encode ipaddr: %w", err)
		}
		out := make([]byte, 4)
		binary.BigEndian.PutUint32(out, u)
		return out, nil

	case TypeEnum:
		idx, err := coerceEnum(obj.EnumItems, val)
		if err != nil {
			return nil, fmt.Errorf("acp1 encode enum: %w", err)
		}
		return []byte{idx}, nil

	case TypeString:
		s := val.Str
		if obj.MaxLen > 0 && len(s) > obj.MaxLen {
			return nil, fmt.Errorf("acp1 encode string: %d > max %d", len(s), obj.MaxLen)
		}
		// Include the NUL terminator to match the property format.
		out := make([]byte, len(s)+1)
		copy(out, s)
		return out, nil
	}
	return nil, fmt.Errorf("acp1 encode: unsupported type %d", acpType)
}

// coerceInt resolves a protocol.Value to a signed integer. Priority:
// Value.Str (parsed) → Value.Int → Value.Uint → Value.Float (truncated).
func coerceInt(v protocol.Value, min, max int64) (int64, error) {
	var n int64
	switch {
	case v.Str != "":
		p, err := strconv.ParseInt(strings.TrimSpace(v.Str), 0, 64)
		if err != nil {
			return 0, fmt.Errorf("parse %q: %w", v.Str, err)
		}
		n = p
	case v.Int != 0:
		n = v.Int
	case v.Uint != 0:
		n = int64(v.Uint)
	case v.Float != 0:
		n = int64(v.Float)
	default:
		n = v.Int // accept explicit zero
	}
	if n < min || n > max {
		return 0, fmt.Errorf("value %d out of range [%d..%d]", n, min, max)
	}
	return n, nil
}

// coerceUint is the unsigned counterpart for Byte objects.
func coerceUint(v protocol.Value, max uint64) (uint64, error) {
	var n uint64
	switch {
	case v.Str != "":
		p, err := strconv.ParseUint(strings.TrimSpace(v.Str), 0, 64)
		if err != nil {
			return 0, fmt.Errorf("parse %q: %w", v.Str, err)
		}
		n = p
	case v.Uint != 0:
		n = v.Uint
	case v.Int > 0:
		n = uint64(v.Int)
	case v.Float > 0:
		n = uint64(v.Float)
	}
	if n > max {
		return 0, fmt.Errorf("value %d out of range [0..%d]", n, max)
	}
	return n, nil
}

// coerceFloat accepts any numeric form plus decimal strings.
func coerceFloat(v protocol.Value) (float64, error) {
	if v.Str != "" {
		return strconv.ParseFloat(strings.TrimSpace(v.Str), 64)
	}
	switch {
	case v.Float != 0:
		return v.Float, nil
	case v.Int != 0:
		return float64(v.Int), nil
	case v.Uint != 0:
		return float64(v.Uint), nil
	}
	return 0, nil
}

// coerceIP accepts dotted-quad strings and raw uint32 values.
func coerceIP(v protocol.Value) (uint32, error) {
	if v.Str != "" {
		parts := strings.Split(strings.TrimSpace(v.Str), ".")
		if len(parts) != 4 {
			return 0, fmt.Errorf("expected dotted-quad, got %q", v.Str)
		}
		var out uint32
		for i, p := range parts {
			n, err := strconv.ParseUint(p, 10, 8)
			if err != nil {
				return 0, fmt.Errorf("octet %d: %w", i, err)
			}
			out = out<<8 | uint32(n)
		}
		return out, nil
	}
	if v.Uint != 0 {
		return uint32(v.Uint), nil
	}
	// Fall back to IPAddr field if populated.
	var out uint32
	for _, b := range v.IPAddr {
		out = out<<8 | uint32(b)
	}
	return out, nil
}

// coerceEnum resolves a user-supplied value to a byte index into the
// object's captured EnumItems. Accepts either an item name ("On") or a
// numeric index ("1"). Numeric form lets users bypass the label when the
// enum items contain tricky characters.
func coerceEnum(items []string, v protocol.Value) (byte, error) {
	if v.Str != "" {
		s := strings.TrimSpace(v.Str)
		// Map lookup: label → index. ACP1 enums are 0-based sequential.
		enumMap := make(map[string]byte, len(items))
		for i, item := range items {
			if i <= 255 {
				enumMap[item] = byte(i)
			}
		}
		if idx, ok := enumMap[s]; ok {
			return idx, nil
		}
		// Numeric fallback: "--value 2" on an enum still works.
		if n, err := strconv.ParseUint(s, 0, 8); err == nil && int(n) < len(items) {
			return byte(n), nil
		}
		return 0, fmt.Errorf("enum item %q not in %v", s, items)
	}
	idx := v.Enum
	if int(idx) >= len(items) && len(items) > 0 {
		return 0, fmt.Errorf("enum index %d out of range [0..%d]", idx, len(items)-1)
	}
	return idx, nil
}
