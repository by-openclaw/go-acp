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
// must carry for one entry, in ascending pid order, per spec Â§5.1
// "Property fields per object type" + Â§5.4 "Property header per field".
//
// Authoritative Â§5.1 matrix (decoded from acp2_protocol.docx XML cell
// shading: âœ“ shaded = required, opÂ¹ = optional, â€” = does not apply):
//
//	| pid | name              | Acc  | Dyn | Node | Pres | Enum | Num  | IPv4 | Str  |
//	|-----|-------------------|------|-----|------|------|------|------|------|------|
//	|  1  | object type       | R    |  -  |  âœ“   |  âœ“   |  âœ“   |  âœ“   |  âœ“   |  âœ“   |
//	|  2  | label             | R    |  -  |  âœ“   |  âœ“   |  âœ“   |  âœ“   |  âœ“   |  âœ“   |
//	|  3  | access            | R    | yes |  â€”   |  âœ“   |  âœ“   |  âœ“   |  âœ“   |  âœ“   |
//	|  4  | event delay       | RW   | yes |  â€”   |  âœ“   |  âœ“   |  âœ“   |  âœ“   |  âœ“   |
//	|  5  | number type       | R    |  -  |  â€”   |  â€”   |  â€”   |  âœ“   |  â€”   |  â€”   |
//	|  6  | string max length | R    |  -  |  â€”   |  â€”   |  â€”   |  â€”   |  â€”   |  âœ“   |
//	|  7  | preset depth      | R    |  -  |  â€”   |  â€”   | opÂ¹  | opÂ¹  | opÂ¹  | opÂ¹  |
//	|  8  | value             | R/RW | yes |  â€”   |  âœ“   |  âœ“   |  âœ“   |  âœ“   |  âœ“   |
//	|  9  | default value     | R    |  -  |  â€”   |  âœ“   |  âœ“   |  âœ“   |  âœ“   |  âœ“   |
//	| 10  | min value         | R    | yes |  â€”   |  â€”   |  â€”   |  âœ“   |  â€”   |  â€”   |
//	| 11  | max value         | R    | yes |  â€”   |  â€”   |  â€”   |  âœ“   |  â€”   |  â€”   |
//	| 12  | step size         | R    |  -  |  â€”   |  â€”   |  â€”   | opÂ¹  |  â€”   |  â€”   |
//	| 13  | unit              | R    |  -  |  â€”   |  â€”   |  â€”   | opÂ¹  |  â€”   |  â€”   |
//	| 14  | children          | R    |  -  |  âœ“   |  â€”   |  â€”   |  â€”   |  â€”   |  â€”   |
//	| 15  | options           | R    |  -  |  â€”   |  âœ“   |  âœ“   |  â€”   |  â€”   |  â€”   |
//	| 16-19 event tag/prio/state/msg | â€” | opÂ¹  | opÂ¹  | opÂ¹  | opÂ¹  | opÂ¹  |
//	| 20  | preset parent     | R    |  -  |  â€”   |  â€”   | opÂ¹  | opÂ¹  | opÂ¹  | opÂ¹  |
//
// Verified byte-for-byte against real Neuron firmware (10.41.40.4)
// GetObject replies captured 2026-05-09:
//
//	Node    obj 15364 MANAGEMENT PORT : {1, 2, 14}                          dlen=108
//	Enum    obj 17671 IO Board        : {1, 2, 3, 4, 8, 9, 15}              dlen= 84
//	Number  obj 15211 Fan Speed       : {1, 2, 3, 4, 5, 8, 9, 10, 11, 12, 13} dlen=96
//	IPv4    obj 15828 Neighbor IP     : {1, 2, 3, 4, 8, 9}                   dlen= 60
//	String  obj  2    Card Name       : {1, 2, 3, 4, 6, 8, 9}                dlen= 72
//
// Capture file: bin/neuron-fresh/out-real-walk-slot0.jsonl. Any deviation
// from the matrix above is a bug in this function.
func buildProperties(e *entry) ([]codec.Property, error) {
	props := make([]codec.Property, 0, 12)

	// pid 1 object type â€” universal
	props = append(props, propInline(codec.PIDObjectType, uint8(e.objType)))
	// pid 2 label â€” universal
	props = append(props, propStringData0(codec.PIDLabel, e.label))

	// pid 3 access + pid 4 event delay â€” every parameter type, NOT Node
	if e.objType != codec.ObjTypeNode {
		props = append(props, propInline(codec.PIDAccess, e.access))
		props = append(props, propEventDelay(eventDelayHint(e.param)))
	}

	switch e.objType {
	case codec.ObjTypeNode:
		// pid 14 children â€” Node only
		props = append(props, propChildren(e.children))

	case codec.ObjTypeNumber:
		// pid 5 number type â€” Number only
		props = append(props, propInline(codec.PIDNumberType, uint8(e.numType)))
		// pid 8 value
		val, err := encodeValueProp(codec.PIDValue, e)
		if err != nil {
			return nil, err
		}
		props = append(props, val)
		// pid 9 default_value â€” required, default 0 if canonical lacks.
		dv, err := numericPropOrZero(codec.PIDDefaultValue, e.numType, e.param.Default)
		if err != nil {
			return nil, err
		}
		props = append(props, dv)
		// pid 10 min â€” required
		mn, err := numericPropOrZero(codec.PIDMinValue, e.numType, e.param.Minimum)
		if err != nil {
			return nil, err
		}
		props = append(props, mn)
		// pid 11 max â€” required
		mx, err := numericPropOrZero(codec.PIDMaxValue, e.numType, e.param.Maximum)
		if err != nil {
			return nil, err
		}
		props = append(props, mx)
		// pid 12 step size â€” optional
		if cp, ok, err := encodeOptionalConstraint(codec.PIDStepSize, e.numType, e.param.Step); err != nil {
			return nil, err
		} else if ok {
			props = append(props, cp)
		}
		// pid 13 unit â€” optional
		if e.param.Unit != nil && *e.param.Unit != "" {
			props = append(props, propStringData0(codec.PIDUnit, *e.param.Unit))
		}

	case codec.ObjTypeEnum:
		// pid 8 value (vtype = preset/enum, u32 wire idx)
		val, err := encodeValueProp(codec.PIDValue, e)
		if err != nil {
			return nil, err
		}
		props = append(props, val)
		// pid 9 default_value â€” required, vtype=preset/enum, u32 wire idx.
		// Falls back to current value when canonical lacks Default â€” same
		// shape the device emits when no separate default exists.
		dv, err := enumDefaultProp(e)
		if err != nil {
			return nil, err
		}
		props = append(props, dv)
		// pid 15 options
		props = append(props, propOptions(enumOptions(e.param)))

	case codec.ObjTypePreset:
		// Preset per spec Â§5.1: pid 5 number_type does NOT apply
		// (Number only). pid 7 preset_depth + pids 8/9 emitted once per
		// depth idx. Spec Â§5.5 "Preset depth".
		props = append(props, propPresetDepth(e.presetDepth, e.presetIdxList))
		for i := uint32(0); i < e.presetDepth; i++ {
			val, err := encodeValueProp(codec.PIDValue, e)
			if err != nil {
				return nil, err
			}
			props = append(props, val)
		}
		for i := uint32(0); i < e.presetDepth; i++ {
			dv, err := enumDefaultProp(e)
			if err != nil {
				return nil, err
			}
			props = append(props, dv)
		}
		// pid 15 options
		props = append(props, propOptions(enumOptions(e.param)))

	case codec.ObjTypeIPv4:
		// pid 8 value
		val, err := encodeValueProp(codec.PIDValue, e)
		if err != nil {
			return nil, err
		}
		props = append(props, val)
		// pid 9 default_value â€” required, vtype=ipv4, 4 octets.
		props = append(props, ipv4DefaultProp(e.param.Default))

	case codec.ObjTypeString:
		// pid 6 string_max_length â€” required (always emit).
		// Default to maxLenHint, falling back to canonical Maximum, then 256
		// (spec Â§1.2 "A string type should support strings up to a 256 bytes").
		props = append(props, propU16Pad(codec.PIDStringMaxLength, stringMaxLen(e.param)))
		// pid 8 value
		val, err := encodeValueProp(codec.PIDValue, e)
		if err != nil {
			return nil, err
		}
		props = append(props, val)
		// pid 9 default_value â€” required, vtype=string, NUL-terminated.
		props = append(props, stringDefaultProp(e.param.Default))
	}

	return props, nil
}

// propStringData0 builds a string property with data byte = 0 per spec
// Â§5.4 (pid 2 label, pid 13 unit). Body is a NUL-terminated UTF-8 string;
// plen = 4 + len(string+NUL). EncodeProperty adds 4-byte alignment pad.
//
//	| Offset | Field | Width    | Notes                                   |
//	|--------|-------|----------|-----------------------------------------|
//	| 0      | pid   | u8       | caller-supplied (pid 2 label / 13 unit) |
//	| 1      | data  | u8       | 0 per spec Â§5.4                         |
//	| 2-3    | plen  | u16 BE   | 4 + len(s) + 1                          |
//	| 4..    | utf8  | len(s)   | UTF-8 string body                       |
//	| 4+len  | NUL   | 1        | 0x00 terminator                         |
//
// Spec reference: acp2_protocol.pdf Â§5.4 (pid 2 label, pid 13 unit)
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
// spec Â§5.4 table: "data: obj type | access | number type â€” plen: 4").
// There is no body.
//
//	| Offset | Field | Width  | Notes                                     |
//	|--------|-------|--------|-------------------------------------------|
//	| 0      | pid   | u8     | 1=object_type, 3=access, 5=number_type    |
//	| 1      | data  | u8     | the value itself (inline, no body)        |
//	| 2-3    | plen  | u16 BE | 4 (header only)                           |
//
// Spec reference: acp2_protocol.pdf Â§5.4 (inline-data properties)
func propInline(pid uint8, val uint8) codec.Property {
	return codec.Property{
		PID:   pid,
		VType: val, // the "data" byte carries the value itself
		PLen:  4,   // header only; no body
		Data:  nil,
	}
}

// propU16Pad builds pid=6 string_max_length per spec Â§5.4 table
// ("data: 0 â€” plen: 6 â€” body: u16 value + u16 pad"). The 2-byte body is
// the u16 length; EncodeProperty will tack on the u16 padding to reach
// the next 4-byte boundary.
//
//	| Offset | Field | Width  | Notes                                     |
//	|--------|-------|--------|-------------------------------------------|
//	| 0      | pid   | u8     | 6 = string_max_length                     |
//	| 1      | data  | u8     | 0 per spec Â§5.4                           |
//	| 2-3    | plen  | u16 BE | 6 (excludes the 2-byte alignment pad)     |
//	| 4-5    | value | u16 BE | max length                                |
//	| 6-7    | pad   | 2      | zero bytes added by EncodeProperty        |
//
// Spec reference: acp2_protocol.pdf Â§5.4 pid=6 string_max_length
func propU16Pad(pid uint8, v uint16) codec.Property {
	body := make([]byte, 2)
	binary.BigEndian.PutUint16(body, v)
	return codec.Property{
		PID:   pid,
		VType: 0,
		PLen:  uint16(4 + 2), // plen excludes padding per spec Â§5.3
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
// Spec reference: acp2_protocol.pdf Â§5.4 pid=14 children
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
// values from canonical Format. When idxList is empty, falls back to
// positional 0..depth-1 (legacy single-element preset shape).
//
//	| Offset    | Field   | Width    | Notes                       |
//	|-----------|---------|----------|-----------------------------|
//	| 0         | pid     | u8       | 7 = preset_depth            |
//	| 1         | data    | u8       | 0                           |
//	| 2-3       | plen    | u16 BE   | 4 + 4*N                     |
//	| 4 + 4*i   | idx_i   | u32 BE   | valid preset idx value      |
//
// Spec reference: acp2_protocol.pdf §5 Preset depth.
// Sparse-idx support per #340/#311.
func propPresetDepth(depth uint32, idxList []uint32) codec.Property {
	useList := len(idxList) == int(depth)
	data := make([]byte, 4*depth)
	for i := uint32(0); i < depth; i++ {
		v := i
		if useList {
			v = idxList[i]
		}
		binary.BigEndian.PutUint32(data[i*4:], v)
	}
	return codec.Property{
		PID:   codec.PIDPresetDepth,
		VType: 0,
		PLen:  uint16(4 + len(data)),
		Data:  data,
	}
}

// optionEntry pairs the wire idx (from canonical EnumMap.Value) with
// its label so propOptions can emit the same idx values pid 8 (current
// value) and pid 9 (default) reference. Real Axon firmware uses sparse
// u32 ids per option (e.g. 786 "Automatic", 791 "Full Speed"); without
// idx-aware emission, pid 8/9 would point at no option and consumers
// fail to resolve the active label.
type optionEntry struct {
	idx   uint32
	label string
}

// propOptions builds the pid=15 (options) property using the
// variable-length per-option layout that every shipping ACP2
// controller and real Axon Neuron firmware implements:
//
//	header: pid=15, data=num_option (INLINE), plen = 4 + sum(record sizes)
//	body  : N records, each {u32 idx, NUL-terminated name, 0-3 byte align}
//
// Spec deviation note: spec Â§5.4 row 15 calls for fixed 72-byte stride
// per option (`plen = 4 + 72*N`). No production controller emits or
// accepts that layout â€” real Neuron's pid 15 wire (verified
// 2026-05-09 against 10.41.40.4: 2-option enum has plen=24 not
// 4+144=148) is variable-length, and Cerebrum + VSM Studio decode the
// variable-length form. Following the wire per
// `feedback_no_workaround` "every shipping controller contradicts the
// spec" exception. Compliance event documents the deviation.
//
//	| Offset      | Field   | Width  | Notes                                |
//	|-------------|---------|--------|--------------------------------------|
//	| 0           | pid     | u8     | 15 = options                         |
//	| 1           | data    | u8     | num options (N) â€” inline count       |
//	| 2-3         | plen    | u16 BE | 4 + sum(record sizes)                |
//	| record i: idx       | u32 BE | option wire idx                      |
//	| record i: name+\0   | varies | UTF-8, NUL-terminated                |
//	| record i: align     | 0-3    | zero pad to next 4-byte boundary     |
func propOptions(opts []optionEntry) codec.Property {
	var data []byte
	for _, opt := range opts {
		var idx [4]byte
		binary.BigEndian.PutUint32(idx[:], opt.idx)
		data = append(data, idx[:]...)
		data = append(data, []byte(opt.label)...)
		data = append(data, 0)
		if pad := (4 - (len(data) % 4)) % 4; pad > 0 {
			data = append(data, make([]byte, pad)...)
		}
	}
	return codec.Property{
		PID:   codec.PIDOptions,
		VType: uint8(len(opts)), // spec Â§5.4: "data: num option" â€” inline count
		PLen:  uint16(4 + len(data)),
		Data:  data,
	}
}

// propEventDelay builds pid=4 (event_delay) per spec Â§5.4 row 4:
//
//	| Offset | Field | Width  | Notes                                  |
//	|--------|-------|--------|----------------------------------------|
//	| 0      | pid   | u8     | 4 = event_delay                        |
//	| 1      | data  | u8     | 0 per spec Â§5.4                        |
//	| 2-3    | plen  | u16 BE | 8 (header 4 + body 4)                  |
//	| 4-7    | rate  | u32 BE | announce delay (ms)                    |
//
// Required on every parameter type (Preset, Enum, Number, IPv4,
// String); absent on Node.
func propEventDelay(rateMs uint32) codec.Property {
	body := make([]byte, 4)
	binary.BigEndian.PutUint32(body, rateMs)
	return codec.Property{
		PID:   codec.PIDEventDelay, // pid 4; spec name "event delay" â€” constant rename to PIDEventDelay tracked in #317
		VType: 0,
		PLen:  8,
		Data:  body,
	}
}

// eventDelayHint extracts the announce-rate hint (ms) from the
// canonical Parameter. Currently no canonical field carries this, so
// we default to 0 â€” same value real Neuron emits when no rate is
// configured (verified obj 17671, obj 21127, obj 15828).
func eventDelayHint(p *canonical.Parameter) uint32 {
	if p == nil {
		return 0
	}
	return 0
}

// numericPropOrZero emits pid=9/10/11 with the canonical's typed
// value when present; otherwise emits the property with a zero body
// of the correct vtype width. Spec Â§5.1 marks pid 9/10/11 as required
// on Number (and pid 9 on every parameter), so silently dropping the
// property when canonical lacks the field is a violation.
func numericPropOrZero(pid uint8, nt codec.NumberType, v any) (codec.Property, error) {
	if v != nil {
		return encodeNumericProp(pid, nt, v)
	}
	switch nt {
	case codec.NumTypeS64, codec.NumTypeU64:
		return numericProp(pid, nt, make([]byte, 8)), nil
	default:
		return numericProp(pid, nt, make([]byte, 4)), nil
	}
}

// enumDefaultProp emits pid=9 (default value) for an Enum / Preset
// using vtype=preset/enum (NumberType 9) and a 4-byte u32 BE body.
// When canonical Default is present, use it; else fall back to the
// current value (matches real-Neuron behaviour for objects with no
// distinct stored default â€” e.g. obj 21127 Fan Control: pid 8 = pid 9
// = 786).
func enumDefaultProp(e *entry) (codec.Property, error) {
	src := e.param.Default
	if src == nil {
		src = e.param.Value
	}
	v, err := asUint32(src, "default")
	if err != nil {
		return codec.Property{}, err
	}
	return numericProp(codec.PIDDefaultValue, codec.NumTypePreset, u32Data(v)), nil
}

// ipv4DefaultProp emits pid=9 for IPv4: vtype=ipv4 (NumberType 10),
// 4-byte body. Default to 0.0.0.0 when canonical lacks Default
// (matches real Neuron obj 15828 Neighbor IP).
func ipv4DefaultProp(def any) codec.Property {
	body := make([]byte, 4)
	if def != nil {
		if v, err := ipv4Uint32(def); err == nil {
			binary.BigEndian.PutUint32(body, v)
		}
	}
	return numericProp(codec.PIDDefaultValue, codec.NumTypeIPv4, body)
}

// stringMaxLen returns the configured max length for a String
// parameter, falling back through canonical hint -> Maximum -> 256
// (spec Â§1.2 "A string type should support strings up to a 256 bytes,
// configurable in menu").
func stringMaxLen(p *canonical.Parameter) uint16 {
	if p == nil {
		return 256
	}
	if ml := maxLenHint(p); ml > 0 {
		return uint16(ml)
	}
	if p.Maximum != nil {
		if u, err := asUint32(p.Maximum, "max"); err == nil && u > 0 && u <= 0xFFFF {
			return uint16(u)
		}
	}
	return 256
}

// stringDefaultProp emits pid=9 for String: vtype=string (NumberType
// 11), body = NUL-terminated UTF-8. Empty default = single NUL byte
// (matches real Neuron obj 2 Card Name: plen=5 body=`00`).
func stringDefaultProp(def any) codec.Property {
	s := ""
	if def != nil {
		if str, ok := def.(string); ok {
			s = str
		}
	}
	return codec.MakeStringProperty(codec.PIDDefaultValue, s)
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
// Spec reference: acp2_protocol.pdf Â§5.2.x (per-type value)
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
		// Enum value uses vtype=9 (preset/enum) per spec Â§5.2.2, stored as u32.
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
//	| 4..    | value | 4 or 8     | big-endian per Â§Number Types table    |
//
// Spec reference: acp2_protocol.pdf Â§Number Types, Â§Wire Sizes
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

// enumOptions returns option records (wire-idx + label) from a
// canonical Parameter. EnumMap entries carry their wire idx in
// Value (e.g. real Axon's sparse u32 ids: 786 "Automatic", 791
// "Full Speed"); pid 8 (current value) and pid 9 (default) reference
// the wire idx, so propOptions must emit the exact same idx values
// in pid 15 records or consumers fail to resolve labels.
//
// Falls back to positional 0..N-1 only for legacy fixtures that
// carry only a comma/newline-separated Enumeration string with no
// EnumMap.
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
