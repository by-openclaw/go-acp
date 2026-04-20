package acp2

import (
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"

	"acp/internal/export/canonical"
	iacp2 "acp/internal/protocol/acp2"
)

// buildProperties assembles the ACP2 property list a get_object reply
// must carry for one entry. Per spec §"Property IDs" the ordering is
// not strictly required but consumers generally expect:
//
//	pid=1  object_type (all)
//	pid=2  label        (all)
//	pid=3  access       (all)
//	pid=5  number_type  (Number, Enum)
//	pid=6  string_max_length (String)
//	pid=8  value        (Number, Enum, IPv4, String)
//	pid=9  default_value (Number)
//	pid=10 min_value    (Number)
//	pid=11 max_value    (Number)
//	pid=12 step_size    (Number)
//	pid=13 unit         (Number)
//	pid=14 children     (Node)
//	pid=15 options      (Enum)
//
// We emit in this order. The codec's EncodeProperties takes care of
// the per-property alignment.
func buildProperties(e *entry) ([]iacp2.Property, error) {
	props := make([]iacp2.Property, 0, 8)

	props = append(props, propU32(iacp2.PIDObjectType, uint32(e.objType)))
	props = append(props, iacp2.MakeStringProperty(iacp2.PIDLabel, e.label))
	props = append(props, propU32(iacp2.PIDAccess, uint32(e.access)))

	switch e.objType {
	case iacp2.ObjTypeNode:
		props = append(props, propChildren(e.children))
	case iacp2.ObjTypeNumber:
		props = append(props, propU32(iacp2.PIDNumberType, uint32(e.numType)))
		val, err := encodeValueProp(iacp2.PIDValue, e)
		if err != nil {
			return nil, err
		}
		props = append(props, val)
		if cp, ok, err := encodeOptionalConstraint(iacp2.PIDDefaultValue, e.numType, e.param.Default); err != nil {
			return nil, err
		} else if ok {
			props = append(props, cp)
		}
		if cp, ok, err := encodeOptionalConstraint(iacp2.PIDMinValue, e.numType, e.param.Minimum); err != nil {
			return nil, err
		} else if ok {
			props = append(props, cp)
		}
		if cp, ok, err := encodeOptionalConstraint(iacp2.PIDMaxValue, e.numType, e.param.Maximum); err != nil {
			return nil, err
		} else if ok {
			props = append(props, cp)
		}
		if cp, ok, err := encodeOptionalConstraint(iacp2.PIDStepSize, e.numType, e.param.Step); err != nil {
			return nil, err
		} else if ok {
			props = append(props, cp)
		}
		if e.param.Unit != nil && *e.param.Unit != "" {
			props = append(props, iacp2.MakeStringProperty(iacp2.PIDUnit, *e.param.Unit))
		}
	case iacp2.ObjTypeEnum:
		props = append(props, propU32(iacp2.PIDNumberType, uint32(iacp2.NumTypeU32)))
		val, err := encodeValueProp(iacp2.PIDValue, e)
		if err != nil {
			return nil, err
		}
		props = append(props, val)
		if cp, ok, err := encodeOptionalConstraint(iacp2.PIDDefaultValue, iacp2.NumTypeU32, e.param.Default); err != nil {
			return nil, err
		} else if ok {
			props = append(props, cp)
		}
		props = append(props, propOptions(enumOptions(e.param)))
	case iacp2.ObjTypeIPv4:
		val, err := encodeValueProp(iacp2.PIDValue, e)
		if err != nil {
			return nil, err
		}
		props = append(props, val)
	case iacp2.ObjTypeString:
		if ml := maxLenHint(e.param); ml > 0 {
			props = append(props, propU32(iacp2.PIDStringMaxLength, uint32(ml)))
		}
		val, err := encodeValueProp(iacp2.PIDValue, e)
		if err != nil {
			return nil, err
		}
		props = append(props, val)
	}

	return props, nil
}

// propU32 builds a 4-byte big-endian u32 property. Used for the
// "small-value-in-u32" cases — object_type, access, number_type,
// string_max_length. The consumer's walker reads these from data[3]
// (the low byte of the big-endian u32).
func propU32(pid uint8, v uint32) iacp2.Property {
	data := make([]byte, 4)
	binary.BigEndian.PutUint32(data, v)
	return iacp2.Property{
		PID:   pid,
		VType: 0,
		PLen:  uint16(4 + 4),
		Data:  data,
	}
}

// propChildren builds the pid=14 (children) property: u32[] of direct
// child obj-ids, big-endian, packed contiguously.
func propChildren(ids []uint32) iacp2.Property {
	data := make([]byte, 4*len(ids))
	for i, id := range ids {
		binary.BigEndian.PutUint32(data[i*4:], id)
	}
	return iacp2.Property{
		PID:   iacp2.PIDChildren,
		VType: 0,
		PLen:  uint16(4 + len(data)),
		Data:  data,
	}
}

// propOptions builds the pid=15 (options) property: repeated
// [u32 BE index + NUL-terminated UTF-8 string + 0-3 bytes pad] per
// option. Index 0..N-1 matches EnumMap ordering.
func propOptions(opts []string) iacp2.Property {
	var data []byte
	var idx [4]byte
	for i, opt := range opts {
		binary.BigEndian.PutUint32(idx[:], uint32(i))
		data = append(data, idx[:]...)
		data = append(data, []byte(opt)...)
		data = append(data, 0) // NUL terminator
		// 4-byte align the next option's index.
		for len(data)%4 != 0 {
			data = append(data, 0)
		}
	}
	return iacp2.Property{
		PID:   iacp2.PIDOptions,
		VType: 0,
		PLen:  uint16(4 + len(data)),
		Data:  data,
	}
}

// encodeValueProp builds the pid=8 (value) property for one entry,
// pulling the typed Value off the canonical.Parameter.
func encodeValueProp(pid uint8, e *entry) (iacp2.Property, error) {
	switch e.objType {
	case iacp2.ObjTypeNumber:
		return encodeNumericProp(pid, e.numType, e.param.Value)
	case iacp2.ObjTypeEnum:
		v, err := asUint32(e.param.Value, "value")
		if err != nil {
			return iacp2.Property{}, err
		}
		return numericProp(pid, iacp2.NumTypeU32, u32Data(v)), nil
	case iacp2.ObjTypeIPv4:
		v, err := ipv4Uint32(e.param.Value)
		if err != nil {
			return iacp2.Property{}, err
		}
		return numericProp(pid, iacp2.NumTypeIPv4, u32Data(v)), nil
	case iacp2.ObjTypeString:
		s, err := asString(e.param.Value, "value")
		if err != nil {
			return iacp2.Property{}, err
		}
		return iacp2.MakeStringProperty(pid, s), nil
	}
	return iacp2.Property{}, fmt.Errorf("encodeValueProp: type %d not supported", e.objType)
}

// encodeOptionalConstraint emits a pid=9/10/11/12 property from a
// constraint field (Default/Min/Max/Step) if present on the canonical
// Parameter. Returns (prop, false, nil) when the field is nil so the
// caller can skip emission.
func encodeOptionalConstraint(pid uint8, nt iacp2.NumberType, v any) (iacp2.Property, bool, error) {
	if v == nil {
		return iacp2.Property{}, false, nil
	}
	p, err := encodeNumericProp(pid, nt, v)
	if err != nil {
		return iacp2.Property{}, false, err
	}
	return p, true, nil
}

// encodeNumericProp serialises a numeric constraint or value per its
// NumberType into the ACP2 wire form (4 or 8 bytes).
func encodeNumericProp(pid uint8, nt iacp2.NumberType, v any) (iacp2.Property, error) {
	switch nt {
	case iacp2.NumTypeS8, iacp2.NumTypeS16, iacp2.NumTypeS32:
		n, err := asInt64(v, "numeric")
		if err != nil {
			return iacp2.Property{}, err
		}
		data, err := iacp2.EncodeNumericValue(nt, n, 0, 0)
		if err != nil {
			return iacp2.Property{}, err
		}
		return numericProp(pid, nt, data), nil
	case iacp2.NumTypeS64:
		n, err := asInt64(v, "numeric")
		if err != nil {
			return iacp2.Property{}, err
		}
		data, err := iacp2.EncodeNumericValue(nt, n, 0, 0)
		if err != nil {
			return iacp2.Property{}, err
		}
		return numericProp(pid, nt, data), nil
	case iacp2.NumTypeU8, iacp2.NumTypeU16, iacp2.NumTypeU32, iacp2.NumTypePreset:
		u, err := asUint64(v, "numeric")
		if err != nil {
			return iacp2.Property{}, err
		}
		data, err := iacp2.EncodeNumericValue(nt, 0, u, 0)
		if err != nil {
			return iacp2.Property{}, err
		}
		return numericProp(pid, nt, data), nil
	case iacp2.NumTypeU64:
		u, err := asUint64(v, "numeric")
		if err != nil {
			return iacp2.Property{}, err
		}
		data, err := iacp2.EncodeNumericValue(nt, 0, u, 0)
		if err != nil {
			return iacp2.Property{}, err
		}
		return numericProp(pid, nt, data), nil
	case iacp2.NumTypeFloat:
		f, err := asFloat64(v, "numeric")
		if err != nil {
			return iacp2.Property{}, err
		}
		data, err := iacp2.EncodeNumericValue(nt, 0, 0, f)
		if err != nil {
			return iacp2.Property{}, err
		}
		return numericProp(pid, nt, data), nil
	case iacp2.NumTypeIPv4:
		u, err := ipv4Uint32(v)
		if err != nil {
			return iacp2.Property{}, err
		}
		return numericProp(pid, nt, u32Data(u)), nil
	}
	return iacp2.Property{}, fmt.Errorf("encodeNumericProp: unsupported NumberType %d", nt)
}

// numericProp wraps an already-encoded value in a Property with its
// vtype set to the NumberType so the consumer decodes correctly.
func numericProp(pid uint8, nt iacp2.NumberType, data []byte) iacp2.Property {
	return iacp2.Property{
		PID:   pid,
		VType: uint8(nt),
		PLen:  uint16(4 + len(data)),
		Data:  data,
	}
}

func u32Data(v uint32) []byte {
	out := make([]byte, 4)
	binary.BigEndian.PutUint32(out, v)
	return out
}

// enumOptions pulls the enum option labels (ordered by ordinal) from a
// canonical Parameter. Prefers EnumMap; falls back to parsing the
// newline- or comma-separated Enumeration string.
func enumOptions(p *canonical.Parameter) []string {
	if len(p.EnumMap) > 0 {
		out := make([]string, len(p.EnumMap))
		for i, e := range p.EnumMap {
			out[i] = e.Key
		}
		return out
	}
	if p.Enumeration != nil && *p.Enumeration != "" {
		raw := *p.Enumeration
		if strings.Contains(raw, "\n") {
			return strings.Split(raw, "\n")
		}
		return strings.Split(raw, ",")
	}
	return nil
}

// --------------------------------------------------------------- any -> typed

func asInt64(v any, field string) (int64, error) {
	switch x := v.(type) {
	case nil:
		return 0, fmt.Errorf("%s: missing", field)
	case int:
		return int64(x), nil
	case int64:
		return x, nil
	case int32:
		return int64(x), nil
	case int16:
		return int64(x), nil
	case uint:
		return int64(x), nil
	case uint32:
		return int64(x), nil
	case uint16:
		return int64(x), nil
	case uint8:
		return int64(x), nil
	case float32:
		if float32(int64(x)) != x {
			return 0, fmt.Errorf("%s: %v has fractional part", field, x)
		}
		return int64(x), nil
	case float64:
		if float64(int64(x)) != x {
			return 0, fmt.Errorf("%s: %v has fractional part", field, x)
		}
		return int64(x), nil
	case string:
		n, err := strconv.ParseInt(x, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("%s: %q is not an integer", field, x)
		}
		return n, nil
	}
	return 0, fmt.Errorf("%s: unexpected type %T", field, v)
}

func asUint64(v any, field string) (uint64, error) {
	n, err := asInt64(v, field)
	if err == nil {
		if n < 0 {
			return 0, fmt.Errorf("%s: negative value %d can't be uint", field, n)
		}
		return uint64(n), nil
	}
	if u, ok := v.(uint64); ok {
		return u, nil
	}
	if u, ok := v.(uint); ok {
		return uint64(u), nil
	}
	return 0, err
}

func asUint32(v any, field string) (uint32, error) {
	u, err := asUint64(v, field)
	if err != nil {
		return 0, err
	}
	if u > 0xFFFFFFFF {
		return 0, fmt.Errorf("%s: %d exceeds u32 range", field, u)
	}
	return uint32(u), nil
}

func asFloat64(v any, field string) (float64, error) {
	switch x := v.(type) {
	case nil:
		return 0, fmt.Errorf("%s: missing", field)
	case float64:
		return x, nil
	case float32:
		return float64(x), nil
	case int:
		return float64(x), nil
	case int64:
		return float64(x), nil
	case string:
		n, err := strconv.ParseFloat(x, 64)
		if err != nil {
			return 0, fmt.Errorf("%s: %q is not a number", field, x)
		}
		return n, nil
	}
	n, err := asInt64(v, field)
	if err != nil {
		return 0, err
	}
	return float64(n), nil
}

func asString(v any, field string) (string, error) {
	if v == nil {
		return "", nil
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("%s: expected string, got %T", field, v)
	}
	return s, nil
}

// ipv4Uint32 parses a dotted-quad string into the big-endian uint32
// form ACP2 carries on the wire.
func ipv4Uint32(v any) (uint32, error) {
	s, ok := v.(string)
	if !ok {
		return 0, fmt.Errorf("ipv4: expected dotted-quad string, got %T", v)
	}
	parts := strings.Split(s, ".")
	if len(parts) != 4 {
		return 0, fmt.Errorf("ipv4: %q is not a dotted-quad", s)
	}
	var out uint32
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || n > 255 {
			return 0, fmt.Errorf("ipv4: octet %d of %q invalid: %q", i, s, p)
		}
		out = (out << 8) | uint32(n)
	}
	return out, nil
}
