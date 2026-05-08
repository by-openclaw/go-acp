package acp2

import (
	"encoding/binary"
	"fmt"
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
		if cp, ok, err := encodeOptionalConstraint(codec.PIDDefaultValue, e.numType, e.param.Default); err != nil {
			return nil, err
		} else if ok {
			props = append(props, cp)
		}
		if cp, ok, err := encodeOptionalConstraint(codec.PIDMinValue, e.numType, e.param.Minimum); err != nil {
			return nil, err
		} else if ok {
			props = append(props, cp)
		}
		if cp, ok, err := encodeOptionalConstraint(codec.PIDMaxValue, e.numType, e.param.Maximum); err != nil {
			return nil, err
		} else if ok {
			props = append(props, cp)
		}
		if cp, ok, err := encodeOptionalConstraint(codec.PIDStepSize, e.numType, e.param.Step); err != nil {
			return nil, err
		} else if ok {
			props = append(props, cp)
		}
		if e.param.Unit != nil && *e.param.Unit != "" {
			props = append(props, propStringData0(codec.PIDUnit, *e.param.Unit))
		}
	case codec.ObjTypeEnum:
		// Enum per spec §5.1: pid 5 number_type does NOT apply (Number only),
		// and pid 9 default_value is depth-indexed ([d]) — only valid for
		// preset children which carry pid 7 preset_depth. A plain Enum is
		// not a preset child, so pid 9 is omitted. Emitting pid 9 with
		// vtype=9 but no pid 7 trips Lawo VSM's parser:
		// "Index was outside the bounds of the array" — the preset array
		// hasn't been sized.
		// pid 8 value uses vtype = 9 (preset/enum), stored as u32 index.
		val, err := encodeValueProp(codec.PIDValue, e)
		if err != nil {
			return nil, err
		}
		props = append(props, val)
		props = append(props, propOptions(enumOptions(e.param)))
	case codec.ObjTypePreset:
		// Preset child per spec §5: pid 7 preset_depth lists the N valid
		// idx values; pids 8/9/10/11 each appear N times in the reply,
		// once per idx. Number-style numeric fields (pid 5, 12, 13) are
		// emitted once. For N=1 the shape degenerates to "Number + pid 7".
		props = append(props, propInline(codec.PIDNumberType, uint8(e.numType)))
		props = append(props, propPresetDepth(e.presetDepth))
		for i := uint32(0); i < e.presetDepth; i++ {
			val, err := encodeValueProp(codec.PIDValue, e)
			if err != nil {
				return nil, err
			}
			props = append(props, val)
		}
		if cp, ok, err := encodeOptionalConstraint(codec.PIDDefaultValue, e.numType, e.param.Default); err != nil {
			return nil, err
		} else if ok {
			for i := uint32(0); i < e.presetDepth; i++ {
				props = append(props, cp)
			}
		}
		if cp, ok, err := encodeOptionalConstraint(codec.PIDMinValue, e.numType, e.param.Minimum); err != nil {
			return nil, err
		} else if ok {
			for i := uint32(0); i < e.presetDepth; i++ {
				props = append(props, cp)
			}
		}
		if cp, ok, err := encodeOptionalConstraint(codec.PIDMaxValue, e.numType, e.param.Maximum); err != nil {
			return nil, err
		} else if ok {
			for i := uint32(0); i < e.presetDepth; i++ {
				props = append(props, cp)
			}
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

// optionEntry pairs a wire index (from canonical EnumMap.Value) with
// its label. Used by propOptions to emit pid=15 with the same idx
// values pid=8 (current value) and pid=9 (default) reference. Per
// spec §5.4 the wire index is part of the option record; consumers
// match pid=8.value against pid=15[i].idx to resolve the active label.
type optionEntry struct {
	idx   uint32
	label string
}

// propOptions builds the pid=15 (options) property per spec §5.4
// row 15: fixed 72-byte stride, plen = 4 + 72 * N.
//
//	header: pid=15, data=num_option (INLINE), plen=4 + 72 * N
//	body  : N fixed-size slots, each {u32 idx, 68-byte NUL-padded name}
//
// The wire idx in each slot MUST come from the device's option-id
// numbering (real Axon assigns u32 ids per option, e.g. 675="A1",
// 690="D4"). Synthesising 0..N-1 leaves pid=8 (value) and pid=9
// (default) — both already keyed by the real wire idx — pointing at
// no option, so Cerebrum cannot resolve the active label.
//
//	| Offset          | Field   | Width  | Notes                         |
//	|-----------------|---------|--------|-------------------------------|
//	| 0               | pid     | u8     | 15 = options                  |
//	| 1               | data    | u8     | num options (N) — inline count|
//	| 2-3             | plen    | u16 BE | 4 + 72*N                      |
//	| 4 + 72*i        | idx_i   | u32 BE | wire idx from EnumMap.Value   |
//	| 8 + 72*i        | name_i  | 68     | UTF-8, zero-padded, truncates |
//
// Spec reference: acp2_protocol.docx §5.4 pid=15 options.
func propOptions(opts []optionEntry) codec.Property {
	n := len(opts)
	data := make([]byte, acp2OptionSize*n)
	for i, opt := range opts {
		off := i * acp2OptionSize
		binary.BigEndian.PutUint32(data[off:off+4], opt.idx)
		// Copy the UTF-8 name into the 68-byte slot. Truncate if
		// longer; zero-pad otherwise. No explicit NUL — the zero
		// padding serves as the terminator.
		name := opt.label
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

// encodeOptionalConstraint emits a pid=9/10/11/12 property from a
// constraint field (Default/Min/Max/Step) if present on the canonical
// Parameter. Returns (prop, false, nil) when the field is nil so the
// caller can skip emission.
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
// enumOptions returns the option records (wire-idx + label) for an
// Enum's pid=15. Pulls each EnumMap entry's Value as the wire idx
// and Key as the label so pid=15[i].idx matches what pid=8 (value)
// and pid=9 (default) reference. Falls back to positional 0..N-1
// indices ONLY when the canonical fixture lacks an EnumMap (legacy
// Enumeration string); a compliance event would be the right call
// there but is left for the canonical-loader to fire.
func enumOptions(p *canonical.Parameter) []optionEntry {
	if len(p.EnumMap) > 0 {
		out := make([]optionEntry, len(p.EnumMap))
		for i, e := range p.EnumMap {
			out[i] = optionEntry{idx: uint32(e.Value), label: e.Key}
		}
		return out
	}
	if p.Enumeration != nil && *p.Enumeration != "" {
		raw := *p.Enumeration
		var labels []string
		if strings.Contains(raw, "\n") {
			labels = strings.Split(raw, "\n")
		} else {
			labels = strings.Split(raw, ",")
		}
		out := make([]optionEntry, len(labels))
		for i, l := range labels {
			out[i] = optionEntry{idx: uint32(i), label: l}
		}
		return out
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
