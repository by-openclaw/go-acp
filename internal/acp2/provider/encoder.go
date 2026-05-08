package acp2

import (
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"

	"dhs/internal/export/canonical"
	"dhs/internal/acp2/codec"
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
func buildProperties(e *entry) ([]codec.Property, error) {
	props := make([]codec.Property, 0, 8)

	props = append(props, propInline(codec.PIDObjectType, uint8(e.objType)))
	props = append(props, propStringData0(codec.PIDLabel, e.label))
	props = append(props, propInline(codec.PIDAccess, e.access))

	switch e.objType {
	case codec.ObjTypeNode:
		props = append(props, propChildren(e.children))
	case codec.ObjTypeNumber:
		props = append(props, propInline(codec.PIDNumberType, uint8(e.numType)))
		val, err := encodeValueProp(codec.PIDValue, e)
		if err != nil {
			return nil, err
		}
		props = append(props, val)
		// pid 9/10/11 are required (Y) on Number per spec §"Property
		// fields" matrix. When canonical lacks an explicit value, fall
		// back to type-derived defaults (0 for default, type-min for
		// min, type-max for max).
		defProp, err := encodeNumericProp(codec.PIDDefaultValue, e.numType,
			constraintOrDefault(e.numType, e.param.Default, "default"))
		if err != nil {
			return nil, err
		}
		props = append(props, defProp)
		minProp, err := encodeNumericProp(codec.PIDMinValue, e.numType,
			constraintOrDefault(e.numType, e.param.Minimum, "min"))
		if err != nil {
			return nil, err
		}
		props = append(props, minProp)
		maxProp, err := encodeNumericProp(codec.PIDMaxValue, e.numType,
			constraintOrDefault(e.numType, e.param.Maximum, "max"))
		if err != nil {
			return nil, err
		}
		props = append(props, maxProp)
		if cp, ok, err := encodeOptionalConstraint(codec.PIDStepSize, e.numType, e.param.Step); err != nil {
			return nil, err
		} else if ok {
			props = append(props, cp)
		}
		if e.param.Unit != nil && *e.param.Unit != "" {
			props = append(props, propStringData0(codec.PIDUnit, *e.param.Unit))
		}
	case codec.ObjTypeEnum:
		// Enum per spec §"Property fields" matrix: pid 9/10/11 are
		// required (Y). The wire vtype for these is preset/enum (9), the
		// same as pid 8 — the value is an option index. Defaults derive
		// from EnumMap: pid 9 = canonical Default OR first option idx;
		// pid 10/11 = min/max of EnumMap[].Value. pid 5 (number_type) is
		// NOT emitted for Enum (spec: Number only).
		val, err := encodeValueProp(codec.PIDValue, e)
		if err != nil {
			return nil, err
		}
		props = append(props, val)
		defIdx, minIdx, maxIdx := enumConstraintBounds(e.param)
		defProp, err := encodeNumericProp(codec.PIDDefaultValue, codec.NumTypePreset, defIdx)
		if err != nil {
			return nil, err
		}
		props = append(props, defProp)
		minProp, err := encodeNumericProp(codec.PIDMinValue, codec.NumTypePreset, minIdx)
		if err != nil {
			return nil, err
		}
		props = append(props, minProp)
		maxProp, err := encodeNumericProp(codec.PIDMaxValue, codec.NumTypePreset, maxIdx)
		if err != nil {
			return nil, err
		}
		props = append(props, maxProp)
		props = append(props, propOptions(enumOptions(e.param)))
	case codec.ObjTypePreset:
		// Preset child per spec §"Property fields" matrix + §"Preset
		// depth": pid 7 preset_depth lists the N valid idx values;
		// pids 8/9/10/11 each appear N times in the reply, once per
		// idx. pid 5 (number_type) is also required for Preset (matrix
		// row 5 marks Y for Preset); spec table treats Preset like
		// Number for the wire vtype of pids 8-11. pid 12/13 are
		// optional. For N=1 the shape degenerates to "Number + pid 7".
		props = append(props, propInline(codec.PIDNumberType, uint8(e.numType)))
		props = append(props, propPresetDepth(e.presetDepth))
		for i := uint32(0); i < e.presetDepth; i++ {
			val, err := encodeValueProp(codec.PIDValue, e)
			if err != nil {
				return nil, err
			}
			props = append(props, val)
		}
		// pid 9/10/11 are required (Y) on Preset per matrix; emit N
		// times. Type-derived fallbacks when canonical lacks the value.
		defProp, err := encodeNumericProp(codec.PIDDefaultValue, e.numType,
			constraintOrDefault(e.numType, e.param.Default, "default"))
		if err != nil {
			return nil, err
		}
		for i := uint32(0); i < e.presetDepth; i++ {
			props = append(props, defProp)
		}
		minProp, err := encodeNumericProp(codec.PIDMinValue, e.numType,
			constraintOrDefault(e.numType, e.param.Minimum, "min"))
		if err != nil {
			return nil, err
		}
		for i := uint32(0); i < e.presetDepth; i++ {
			props = append(props, minProp)
		}
		maxProp, err := encodeNumericProp(codec.PIDMaxValue, e.numType,
			constraintOrDefault(e.numType, e.param.Maximum, "max"))
		if err != nil {
			return nil, err
		}
		for i := uint32(0); i < e.presetDepth; i++ {
			props = append(props, maxProp)
		}
		if cp, ok, err := encodeOptionalConstraint(codec.PIDStepSize, e.numType, e.param.Step); err != nil {
			return nil, err
		} else if ok {
			props = append(props, cp)
		}
		if e.param.Unit != nil && *e.param.Unit != "" {
			props = append(props, propStringData0(codec.PIDUnit, *e.param.Unit))
		}
	case codec.ObjTypeIPv4:
		val, err := encodeValueProp(codec.PIDValue, e)
		if err != nil {
			return nil, err
		}
		props = append(props, val)
	case codec.ObjTypeString:
		if ml := maxLenHint(e.param); ml > 0 {
			props = append(props, propU16Pad(codec.PIDStringMaxLength, uint16(ml)))
		}
		val, err := encodeValueProp(codec.PIDValue, e)
		if err != nil {
			return nil, err
		}
		props = append(props, val)
	}

	return props, nil
}

// propStringData0 builds a string property with data byte = 0 per spec
// §5.4 (pid 2 label, pid 13 unit). Body is a NUL-terminated UTF-8 string;
// plen = 4 + len(string+NUL). EncodeProperty adds 4-byte alignment pad.
//
//	| Offset | Field | Width    | Notes                                   |
//	|--------|-------|----------|-----------------------------------------|
//	| 0      | pid   | u8       | caller-supplied (pid 2 label / 13 unit) |
//	| 1      | data  | u8       | 0 per spec §5.4                         |
//	| 2-3    | plen  | u16 BE   | 4 + len(s) + 1                          |
//	| 4..    | utf8  | len(s)   | UTF-8 string body                       |
//	| 4+len  | NUL   | 1        | 0x00 terminator                         |
//
// Spec reference: acp2_protocol.pdf §5.4 (pid 2 label, pid 13 unit)
func propStringData0(pid uint8, s string) codec.Property {
	body := make([]byte, len(s)+1) // +1 for NUL terminator
	copy(body, s)
	return codec.Property{
		PID:   pid,
		VType: 0,
		PLen:  uint16(4 + len(body)),
		Data:  body,
	}
}

// propInline builds a property whose entire value rides in the header's
// data byte (pid=1 object_type, pid=3 access, pid=5 number_type per
// spec §5.4 table: "data: obj type | access | number type — plen: 4").
// There is no body.
//
//	| Offset | Field | Width  | Notes                                     |
//	|--------|-------|--------|-------------------------------------------|
//	| 0      | pid   | u8     | 1=object_type, 3=access, 5=number_type    |
//	| 1      | data  | u8     | the value itself (inline, no body)        |
//	| 2-3    | plen  | u16 BE | 4 (header only)                           |
//
// Spec reference: acp2_protocol.pdf §5.4 (inline-data properties)
func propInline(pid uint8, val uint8) codec.Property {
	return codec.Property{
		PID:   pid,
		VType: val, // the "data" byte carries the value itself
		PLen:  4,   // header only; no body
		Data:  nil,
	}
}

// propU16Pad builds pid=6 string_max_length per spec §5.4 table
// ("data: 0 — plen: 6 — body: u16 value + u16 pad"). The 2-byte body is
// the u16 length; EncodeProperty will tack on the u16 padding to reach
// the next 4-byte boundary.
//
//	| Offset | Field | Width  | Notes                                     |
//	|--------|-------|--------|-------------------------------------------|
//	| 0      | pid   | u8     | 6 = string_max_length                     |
//	| 1      | data  | u8     | 0 per spec §5.4                           |
//	| 2-3    | plen  | u16 BE | 6 (excludes the 2-byte alignment pad)     |
//	| 4-5    | value | u16 BE | max length                                |
//	| 6-7    | pad   | 2      | zero bytes added by EncodeProperty        |
//
// Spec reference: acp2_protocol.pdf §5.4 pid=6 string_max_length
func propU16Pad(pid uint8, v uint16) codec.Property {
	body := make([]byte, 2)
	binary.BigEndian.PutUint16(body, v)
	return codec.Property{
		PID:   pid,
		VType: 0,
		PLen:  uint16(4 + 2), // plen excludes padding per spec §5.3
		Data:  body,
	}
}

// propChildren builds the pid=14 (children) property: u32[] of direct
// child obj-ids, big-endian, packed contiguously.
//
//	| Offset    | Field   | Width    | Notes                              |
//	|-----------|---------|----------|------------------------------------|
//	| 0         | pid     | u8       | 14 = children                      |
//	| 1         | data    | u8       | 0                                  |
//	| 2-3       | plen    | u16 BE   | 4 + 4*len(ids)                     |
//	| 4 + 4*i   | child_i | u32 BE   | one entry per child obj-id         |
//
// Spec reference: acp2_protocol.pdf §5.4 pid=14 children
func propChildren(ids []uint32) codec.Property {
	data := make([]byte, 4*len(ids))
	for i, id := range ids {
		binary.BigEndian.PutUint32(data[i*4:], id)
	}
	return codec.Property{
		PID:   codec.PIDChildren,
		VType: 0,
		PLen:  uint16(4 + len(data)),
		Data:  data,
	}
}

// propPresetDepth builds the pid=7 (preset_depth) property per spec §5
// "Preset depth". Body is a u32[] big-endian list of valid preset idx
// values — consumers then know how many times pids 8/9/10/11 repeat in
// the same get_object reply (once per idx listed here).
//
//	| Offset    | Field   | Width    | Notes                              |
//	|-----------|---------|----------|------------------------------------|
//	| 0         | pid     | u8       | 7 = preset_depth                   |
//	| 1         | data    | u8       | 0                                  |
//	| 2-3       | plen    | u16 BE   | 4 + 4*depth                        |
//	| 4 + 4*i   | idx_i   | u32 BE   | valid preset idx value, 0..depth-1 |
//
// Spec reference: acp2_protocol.pdf §5 Preset depth,
// internal/acp2/CLAUDE.md "Preset depth".
func propPresetDepth(depth uint32) codec.Property {
	data := make([]byte, 4*depth)
	for i := uint32(0); i < depth; i++ {
		binary.BigEndian.PutUint32(data[i*4:], i)
	}
	return codec.Property{
		PID:   codec.PIDPresetDepth,
		VType: 0,
		PLen:  uint16(4 + len(data)),
		Data:  data,
	}
}

// acp2OptionSize is the fixed on-wire size of one enum option (pid 15
// entry) per spec §5.4: plen = 4 + (72 * option), so each option is
// exactly 72 bytes: 4-byte u32 index + 68-byte NUL-padded UTF-8 name.
const acp2OptionSize = 72

// propOptions builds the pid=15 (options) property per spec §5.4:
//
//	header: pid=15, data=num_option (INLINE), plen=4 + 72 * N
//	body  : N fixed-size slots, each {u32 index, 68-byte NUL-padded name}
//
// Matches real Axon firmware; Lawo VSM's driver parses with this layout.
// Index 0..N-1 matches EnumMap ordering.
//
//	| Offset          | Field   | Width  | Notes                         |
//	|-----------------|---------|--------|-------------------------------|
//	| 0               | pid     | u8     | 15 = options                  |
//	| 1               | data    | u8     | num options (N) — inline count|
//	| 2-3             | plen    | u16 BE | 4 + 72*N                      |
//	| 4 + 72*i        | idx_i   | u32 BE | option index (0..N-1)         |
//	| 8 + 72*i        | name_i  | 68     | UTF-8, zero-padded, truncates |
//
// Spec reference: acp2_protocol.pdf §5.4 pid=15 options
func propOptions(opts []string) codec.Property {
	n := len(opts)
	data := make([]byte, acp2OptionSize*n)
	for i, opt := range opts {
		off := i * acp2OptionSize
		binary.BigEndian.PutUint32(data[off:off+4], uint32(i))
		// Copy the UTF-8 name into the 68-byte slot. Truncate if
		// longer; zero-pad otherwise. No explicit NUL — the zero
		// padding serves as the terminator.
		name := opt
		if len(name) > acp2OptionSize-4-1 { // reserve at least 1 NUL
			name = name[:acp2OptionSize-4-1]
		}
		copy(data[off+4:off+acp2OptionSize], name)
	}
	return codec.Property{
		PID:   codec.PIDOptions,
		VType: uint8(n), // spec §5.4: "data: num option" — inline count
		PLen:  uint16(4 + acp2OptionSize*n),
		Data:  data,
	}
}

// encodeValueProp builds the pid=8 (value) property for one entry,
// pulling the typed Value off the canonical.Parameter.
//
// Output layout depends on objType:
//
//	| objType | vtype                 | Value width | Notes                 |
//	|---------|-----------------------|-------------|-----------------------|
//	| Number  | e.numType             | 4 or 8      | via encodeNumericProp |
//	| Enum    | NumTypePreset (9)     | 4           | u32 option index      |
//	| IPv4    | NumTypeIPv4  (10)     | 4           | packed octets         |
//	| String  | NumTypeString (11)    | len(s)+1    | UTF-8 + NUL + pad     |
//
// Spec reference: acp2_protocol.pdf §5.2.x (per-type value)
func encodeValueProp(pid uint8, e *entry) (codec.Property, error) {
	switch e.objType {
	case codec.ObjTypeNumber, codec.ObjTypePreset:
		// Preset children reuse the Number wire shape per depth slot
		// (pid 8/9/10/11 typed by e.numType). pid 7 preset_depth is
		// emitted separately in buildProperties.
		return encodeNumericProp(pid, e.numType, e.param.Value)
	case codec.ObjTypeEnum:
		v, err := asUint32(e.param.Value, "value")
		if err != nil {
			return codec.Property{}, err
		}
		// Enum value uses vtype=9 (preset/enum) per spec §5.2.2, stored as u32.
		return numericProp(pid, codec.NumTypePreset, u32Data(v)), nil
	case codec.ObjTypeIPv4:
		v, err := ipv4Uint32(e.param.Value)
		if err != nil {
			return codec.Property{}, err
		}
		return numericProp(pid, codec.NumTypeIPv4, u32Data(v)), nil
	case codec.ObjTypeString:
		s, err := asString(e.param.Value, "value")
		if err != nil {
			return codec.Property{}, err
		}
		return codec.MakeStringProperty(pid, s), nil
	}
	return codec.Property{}, fmt.Errorf("encodeValueProp: type %d not supported", e.objType)
}

// encodeOptionalConstraint emits a pid=12 step_size or pid=13 unit
// property from a canonical field if present. Returns (prop, false, nil)
// when the field is nil so the caller can skip emission. Used only for
// truly optional pids per the spec property-fields matrix; pids 9/10/11
// are required and use constraintOrDefault directly.
func encodeOptionalConstraint(pid uint8, nt codec.NumberType, v any) (codec.Property, bool, error) {
	if v == nil {
		return codec.Property{}, false, nil
	}
	p, err := encodeNumericProp(pid, nt, v)
	if err != nil {
		return codec.Property{}, false, err
	}
	return p, true, nil
}

// constraintOrDefault returns the canonical-supplied value when set,
// otherwise a NumberType-derived default. Used for pids 9/10/11 which
// the spec property-fields matrix marks required (Y) on Number / Enum /
// Preset — emit must always succeed even when the canonical fixture
// omits Default/Min/Max.
//
//	| kind    | fallback when canonical is nil                         |
//	|---------|--------------------------------------------------------|
//	| default | 0 (or 0.0 for float)                                   |
//	| min     | NumberType minimum (e.g. s8: -128, u8: 0, float: -max) |
//	| max     | NumberType maximum (e.g. s8:  127, u8: 255, float: max)|
//
// Spec reference: acp2_protocol.docx §"Property fields" matrix rows
// 9 (default_value), 10 (min_value), 11 (max_value).
func constraintOrDefault(nt codec.NumberType, canonical any, kind string) any {
	if canonical != nil {
		return canonical
	}
	switch kind {
	case "default":
		if nt == codec.NumTypeFloat {
			return float64(0)
		}
		return int64(0)
	case "min":
		return numericTypeMin(nt)
	case "max":
		return numericTypeMax(nt)
	}
	return int64(0)
}

// numericTypeMin returns the smallest representable value for an ACP2
// NumberType, formatted as the type encodeNumericProp expects.
func numericTypeMin(nt codec.NumberType) any {
	switch nt {
	case codec.NumTypeS8:
		return int64(-128)
	case codec.NumTypeS16:
		return int64(-32768)
	case codec.NumTypeS32:
		return int64(-2147483648)
	case codec.NumTypeS64:
		return int64(math.MinInt64)
	case codec.NumTypeU8, codec.NumTypeU16, codec.NumTypeU32,
		codec.NumTypeU64, codec.NumTypePreset, codec.NumTypeIPv4:
		return uint64(0)
	case codec.NumTypeFloat:
		return float64(-math.MaxFloat32)
	}
	return int64(0)
}

// numericTypeMax returns the largest representable value for an ACP2
// NumberType, formatted as the type encodeNumericProp expects.
func numericTypeMax(nt codec.NumberType) any {
	switch nt {
	case codec.NumTypeS8:
		return int64(127)
	case codec.NumTypeS16:
		return int64(32767)
	case codec.NumTypeS32:
		return int64(2147483647)
	case codec.NumTypeS64:
		return int64(math.MaxInt64)
	case codec.NumTypeU8:
		return uint64(255)
	case codec.NumTypeU16:
		return uint64(65535)
	case codec.NumTypeU32:
		return uint64(0xFFFFFFFF)
	case codec.NumTypeU64:
		return uint64(math.MaxUint64)
	case codec.NumTypePreset, codec.NumTypeIPv4:
		return uint64(0xFFFFFFFF)
	case codec.NumTypeFloat:
		return float64(math.MaxFloat32)
	}
	return int64(0)
}

// enumConstraintBounds derives pid 9/10/11 values for an Enum from its
// canonical EnumMap. Returns (default, min, max) as uint64 wire indices
// (pid 8 / 9 / 10 / 11 on Enum all use vtype = preset/enum = 9, body
// is u32 BE option idx).
//
//	default = canonical Default if set, else first EnumMap.Value
//	min     = smallest EnumMap.Value
//	max     = largest EnumMap.Value
//
// When EnumMap is empty (legacy fixtures) all three fall back to 0.
func enumConstraintBounds(p *canonical.Parameter) (defIdx, minIdx, maxIdx uint64) {
	if p == nil || len(p.EnumMap) == 0 {
		return 0, 0, 0
	}
	first := uint64(p.EnumMap[0].Value)
	minIdx = first
	maxIdx = first
	for _, em := range p.EnumMap {
		v := uint64(em.Value)
		if v < minIdx {
			minIdx = v
		}
		if v > maxIdx {
			maxIdx = v
		}
	}
	defIdx = first
	if p.Default != nil {
		if u, err := asUint64(p.Default, "enum default"); err == nil {
			defIdx = u
		}
	}
	return defIdx, minIdx, maxIdx
}

// encodeNumericProp serialises a numeric constraint or value per its
// NumberType into the ACP2 wire form (4 or 8 bytes).
//
// Emitted property layout:
//
//	| Offset | Field | Width      | Notes                                 |
//	|--------|-------|------------|---------------------------------------|
//	| 0      | pid   | u8         | caller-supplied (pid 8/9/10/11/12)    |
//	| 1      | vtype | u8         | NumberType (drives body width)        |
//	| 2-3    | plen  | u16 BE     | 4 + body width (8 or 12 total)        |
//	| 4..    | value | 4 or 8     | big-endian per §Number Types table    |
//
// Spec reference: acp2_protocol.pdf §Number Types, §Wire Sizes
func encodeNumericProp(pid uint8, nt codec.NumberType, v any) (codec.Property, error) {
	switch nt {
	case codec.NumTypeS8, codec.NumTypeS16, codec.NumTypeS32:
		n, err := asInt64(v, "numeric")
		if err != nil {
			return codec.Property{}, err
		}
		data, err := codec.EncodeNumericValue(nt, n, 0, 0)
		if err != nil {
			return codec.Property{}, err
		}
		return numericProp(pid, nt, data), nil
	case codec.NumTypeS64:
		n, err := asInt64(v, "numeric")
		if err != nil {
			return codec.Property{}, err
		}
		data, err := codec.EncodeNumericValue(nt, n, 0, 0)
		if err != nil {
			return codec.Property{}, err
		}
		return numericProp(pid, nt, data), nil
	case codec.NumTypeU8, codec.NumTypeU16, codec.NumTypeU32, codec.NumTypePreset:
		u, err := asUint64(v, "numeric")
		if err != nil {
			return codec.Property{}, err
		}
		data, err := codec.EncodeNumericValue(nt, 0, u, 0)
		if err != nil {
			return codec.Property{}, err
		}
		return numericProp(pid, nt, data), nil
	case codec.NumTypeU64:
		u, err := asUint64(v, "numeric")
		if err != nil {
			return codec.Property{}, err
		}
		data, err := codec.EncodeNumericValue(nt, 0, u, 0)
		if err != nil {
			return codec.Property{}, err
		}
		return numericProp(pid, nt, data), nil
	case codec.NumTypeFloat:
		f, err := asFloat64(v, "numeric")
		if err != nil {
			return codec.Property{}, err
		}
		data, err := codec.EncodeNumericValue(nt, 0, 0, f)
		if err != nil {
			return codec.Property{}, err
		}
		return numericProp(pid, nt, data), nil
	case codec.NumTypeIPv4:
		u, err := ipv4Uint32(v)
		if err != nil {
			return codec.Property{}, err
		}
		return numericProp(pid, nt, u32Data(u)), nil
	}
	return codec.Property{}, fmt.Errorf("encodeNumericProp: unsupported codec.NumberType %d", nt)
}

// numericProp wraps an already-encoded value in a Property with its
// vtype set to the NumberType so the consumer decodes correctly.
func numericProp(pid uint8, nt codec.NumberType, data []byte) codec.Property {
	return codec.Property{
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
