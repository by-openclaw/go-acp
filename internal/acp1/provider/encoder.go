package acp1

import (
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"

	"acp/internal/export/canonical"
	iacp1 "acp/internal/acp1/consumer"
)

// encodeObject builds the Value bytes returned by a getObject reply
// (spec §"AxonNet methods" p.28 Method 5). Layout is:
//
//	[type_byte] [num_props] [per-type properties in order]
//
// This is the exact inverse of property.go's DecodeObject — the same
// per-type ordering, same field widths, same big-endian encoding, same
// NUL-terminated strings. A round-trip test in encoder_test.go asserts
// encodeObject followed by DecodeObject recovers every field.
//
// Errors returned when a canonical value is out of spec range for its
// ACP1 type (e.g. int16 value 40000) so the provider never emits
// malformed bytes. Never silently clamps.
func encodeObject(e *entry) ([]byte, error) {
	if e == nil || e.param == nil {
		return nil, fmt.Errorf("encodeObject: nil entry")
	}
	switch e.acpType {
	case iacp1.TypeRoot:
		return encodeRoot(e)
	case iacp1.TypeInteger:
		return encodeInteger(e)
	case iacp1.TypeIPAddr:
		return encodeIPAddr(e)
	case iacp1.TypeFloat:
		return encodeFloat(e)
	case iacp1.TypeEnum:
		return encodeEnum(e)
	case iacp1.TypeString:
		return encodeString(e)
	case iacp1.TypeFrame:
		return encodeFrame(e)
	case iacp1.TypeAlarm:
		return encodeAlarm(e)
	case iacp1.TypeFile:
		return encodeFile(e)
	case iacp1.TypeLong:
		return encodeLong(e)
	case iacp1.TypeByte:
		return encodeByte(e)
	}
	return nil, fmt.Errorf("encodeObject: unsupported ACP1 type %d", e.acpType)
}

// encodeValue builds the Value bytes returned by getValue / setValue /
// setIncValue / setDecValue / setDefValue replies. Per spec §"Objects by
// Type" p.21-27: the "value" property for each type.
//
//	Root       -> boot_mode (u8)
//	Integer    -> value (i16)
//	IPAddr     -> value (u32)
//	Float      -> value (f32)
//	Enum       -> value (u8, index into item list)
//	String     -> value (NUL-terminated)
//	Frame      -> num_slots (u8) + slot_status[N]
//	Alarm      -> active (u8, 0=idle, 1=active)
//	File       -> num_fragments (i16)
//	Long       -> value (i32)
//	Byte       -> value (u8)
func encodeValue(e *entry) ([]byte, error) {
	if e == nil || e.param == nil {
		return nil, fmt.Errorf("encodeValue: nil entry")
	}
	switch e.acpType {
	case iacp1.TypeInteger:
		v, err := asInt16(e.param.Value, "value")
		if err != nil {
			return nil, err
		}
		return writeI16(v), nil
	case iacp1.TypeIPAddr:
		v, err := ipv4ToUint32(e.param.Value)
		if err != nil {
			return nil, err
		}
		return writeU32(v), nil
	case iacp1.TypeFloat:
		v, err := asFloat32(e.param.Value, "value")
		if err != nil {
			return nil, err
		}
		return writeF32(v), nil
	case iacp1.TypeEnum:
		v, err := asUint8(e.param.Value, "value")
		if err != nil {
			return nil, err
		}
		return []byte{v}, nil
	case iacp1.TypeString:
		s, err := asString(e.param.Value, "value")
		if err != nil {
			return nil, err
		}
		return writeCStr(s), nil
	case iacp1.TypeAlarm:
		v, err := asBool(e.param.Value, "value")
		if err != nil {
			return nil, err
		}
		if v {
			return []byte{1}, nil
		}
		return []byte{0}, nil
	case iacp1.TypeLong:
		v, err := asInt32(e.param.Value, "value")
		if err != nil {
			return nil, err
		}
		return writeI32(v), nil
	case iacp1.TypeByte:
		v, err := asUint8(e.param.Value, "value")
		if err != nil {
			return nil, err
		}
		return []byte{v}, nil
	case iacp1.TypeFile:
		v, err := asInt16(e.param.Default, "num_fragments") // File's "value" is num_fragments (see spec p.26)
		if err != nil {
			return nil, err
		}
		return writeI16(v), nil
	case iacp1.TypeFrame:
		return encodeFrameValue(e)
	}
	return nil, fmt.Errorf("encodeValue: unsupported ACP1 type %d", e.acpType)
}

// encodeRoot — spec p.21. Root has 9 properties; object_type(0) and
// num_props(9) are part of the header.
func encodeRoot(e *entry) ([]byte, error) {
	// Root props: access, boot_mode, num_identity, num_control,
	// num_status, num_alarm, num_file. The canonical tree doesn't store
	// these counts on a "root" Parameter — they come from tree.slots.
	// The session layer is responsible for synthesising the entry with
	// the correct counts before calling encodeObject.
	return nil, fmt.Errorf("encodeRoot: Root objects are synthesised by session.go, not stored as tree entries")
}

func encodeInteger(e *entry) ([]byte, error) {
	p := e.param
	value, err := asInt16(p.Value, "value")
	if err != nil {
		return nil, err
	}
	def, err := asInt16Opt(p.Default, "default", 0)
	if err != nil {
		return nil, err
	}
	step, err := asInt16Opt(p.Step, "step", 1)
	if err != nil {
		return nil, err
	}
	minV, err := asInt16Opt(p.Minimum, "minimum", math.MinInt16)
	if err != nil {
		return nil, err
	}
	maxV, err := asInt16Opt(p.Maximum, "maximum", math.MaxInt16)
	if err != nil {
		return nil, err
	}
	buf := newBuf(iacp1.TypeInteger, 10)
	buf.u8(e.access)
	buf.i16(value)
	buf.i16(def)
	buf.i16(step)
	buf.i16(minV)
	buf.i16(maxV)
	buf.cstr(limitLabel(p.Identifier))
	buf.cstr(unitOf(p))
	return buf.bytes(), nil
}

func encodeLong(e *entry) ([]byte, error) {
	p := e.param
	value, err := asInt32(p.Value, "value")
	if err != nil {
		return nil, err
	}
	def, err := asInt32Opt(p.Default, "default", 0)
	if err != nil {
		return nil, err
	}
	step, err := asInt32Opt(p.Step, "step", 1)
	if err != nil {
		return nil, err
	}
	minV, err := asInt32Opt(p.Minimum, "minimum", math.MinInt32)
	if err != nil {
		return nil, err
	}
	maxV, err := asInt32Opt(p.Maximum, "maximum", math.MaxInt32)
	if err != nil {
		return nil, err
	}
	buf := newBuf(iacp1.TypeLong, 10)
	buf.u8(e.access)
	buf.i32(value)
	buf.i32(def)
	buf.i32(step)
	buf.i32(minV)
	buf.i32(maxV)
	buf.cstr(limitLabel(p.Identifier))
	buf.cstr(unitOf(p))
	return buf.bytes(), nil
}

func encodeByte(e *entry) ([]byte, error) {
	p := e.param
	value, err := asUint8(p.Value, "value")
	if err != nil {
		return nil, err
	}
	def, err := asUint8Opt(p.Default, "default", 0)
	if err != nil {
		return nil, err
	}
	step, err := asUint8Opt(p.Step, "step", 1)
	if err != nil {
		return nil, err
	}
	minV, err := asUint8Opt(p.Minimum, "minimum", 0)
	if err != nil {
		return nil, err
	}
	maxV, err := asUint8Opt(p.Maximum, "maximum", math.MaxUint8)
	if err != nil {
		return nil, err
	}
	buf := newBuf(iacp1.TypeByte, 10)
	buf.u8(e.access)
	buf.u8(value)
	buf.u8(def)
	buf.u8(step)
	buf.u8(minV)
	buf.u8(maxV)
	buf.cstr(limitLabel(p.Identifier))
	buf.cstr(unitOf(p))
	return buf.bytes(), nil
}

func encodeFloat(e *entry) ([]byte, error) {
	p := e.param
	value, err := asFloat32(p.Value, "value")
	if err != nil {
		return nil, err
	}
	def, err := asFloat32Opt(p.Default, "default", 0)
	if err != nil {
		return nil, err
	}
	step, err := asFloat32Opt(p.Step, "step", 1)
	if err != nil {
		return nil, err
	}
	minV, err := asFloat32Opt(p.Minimum, "minimum", -math.MaxFloat32)
	if err != nil {
		return nil, err
	}
	maxV, err := asFloat32Opt(p.Maximum, "maximum", math.MaxFloat32)
	if err != nil {
		return nil, err
	}
	buf := newBuf(iacp1.TypeFloat, 10)
	buf.u8(e.access)
	buf.f32(value)
	buf.f32(def)
	buf.f32(step)
	buf.f32(minV)
	buf.f32(maxV)
	buf.cstr(limitLabel(p.Identifier))
	buf.cstr(unitOf(p))
	return buf.bytes(), nil
}

func encodeIPAddr(e *entry) ([]byte, error) {
	p := e.param
	value, err := ipv4ToUint32(p.Value)
	if err != nil {
		return nil, err
	}
	def, err := ipv4ToUint32Opt(p.Default, 0)
	if err != nil {
		return nil, err
	}
	step, err := ipv4ToUint32Opt(p.Step, 0)
	if err != nil {
		return nil, err
	}
	minV, err := ipv4ToUint32Opt(p.Minimum, 0)
	if err != nil {
		return nil, err
	}
	maxV, err := ipv4ToUint32Opt(p.Maximum, math.MaxUint32)
	if err != nil {
		return nil, err
	}
	buf := newBuf(iacp1.TypeIPAddr, 10)
	buf.u8(e.access)
	buf.u32(value)
	buf.u32(def)
	buf.u32(step)
	buf.u32(minV)
	buf.u32(maxV)
	buf.cstr(limitLabel(p.Identifier))
	buf.cstr(unitOf(p))
	return buf.bytes(), nil
}

func encodeEnum(e *entry) ([]byte, error) {
	p := e.param
	value, err := asUint8(p.Value, "value")
	if err != nil {
		return nil, err
	}
	items := enumItems(p)
	if len(items) == 0 {
		return nil, fmt.Errorf("enum %q has no items (set enumMap or enumeration)", p.Identifier)
	}
	if len(items) > math.MaxUint8 {
		return nil, fmt.Errorf("enum %q has %d items, exceeds 255", p.Identifier, len(items))
	}
	def, err := asUint8Opt(p.Default, "default", 0)
	if err != nil {
		return nil, err
	}
	buf := newBuf(iacp1.TypeEnum, 8)
	buf.u8(e.access)
	buf.u8(value)
	buf.u8(uint8(len(items)))
	buf.u8(def)
	buf.cstr(limitLabel(p.Identifier))
	// Item list is a single NUL-terminated comma-delimited string —
	// spec p.23 example "Off,On,Auto\0". One final NUL is part of cstr.
	buf.cstr(strings.Join(items, ","))
	return buf.bytes(), nil
}

func encodeString(e *entry) ([]byte, error) {
	p := e.param
	value, err := asString(p.Value, "value")
	if err != nil {
		return nil, err
	}
	maxLen := stringMaxLen(p)
	buf := newBuf(iacp1.TypeString, 6)
	buf.u8(e.access)
	buf.cstr(limitString(value, int(maxLen)))
	buf.u8(maxLen)
	buf.cstr(limitLabel(p.Identifier))
	return buf.bytes(), nil
}

func encodeAlarm(e *entry) ([]byte, error) {
	p := e.param
	// priority/tag live as inline property fields of a real Axon alarm
	// object. Canonical doesn't expose them explicitly — we read them
	// from the Format hint suffix "alarm:priority=N,tag=M" when present,
	// otherwise default to priority=1 (enabled) and tag=0.
	priority, tag := alarmPriorityTag(p)
	onMsg, offMsg := alarmMessages(p)

	buf := newBuf(iacp1.TypeAlarm, 8)
	buf.u8(e.access)
	buf.u8(priority)
	buf.u8(tag)
	buf.cstr(limitLabel(p.Identifier))
	buf.cstrWithLimit(onMsg, iacp1.MaxAlarmMsg)
	buf.cstrWithLimit(offMsg, iacp1.MaxAlarmMsg)
	return buf.bytes(), nil
}

func encodeFile(e *entry) ([]byte, error) {
	p := e.param
	// Canonical carries the file name in Value; num_fragments in Default
	// (no dedicated canonical field).
	nameAny := p.Value
	name, err := asString(nameAny, "value")
	if err != nil {
		return nil, err
	}
	frags, err := asInt16Opt(p.Default, "num_fragments", 0)
	if err != nil {
		return nil, err
	}
	buf := newBuf(iacp1.TypeFile, 5)
	buf.u8(e.access)
	buf.i16(frags)
	buf.cstr(name)
	return buf.bytes(), nil
}

func encodeFrame(e *entry) ([]byte, error) {
	slots, err := frameSlotStatuses(e.param)
	if err != nil {
		return nil, err
	}
	buf := newBuf(iacp1.TypeFrame, 4)
	buf.u8(e.access)
	buf.u8(uint8(len(slots)))
	for _, s := range slots {
		buf.u8(s)
	}
	return buf.bytes(), nil
}

// encodeFrameValue serialises just the value portion for getValue —
// the [num_slots, slot_status[N]] block without the type/num_props
// prefix.
func encodeFrameValue(e *entry) ([]byte, error) {
	slots, err := frameSlotStatuses(e.param)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 1+len(slots))
	out[0] = uint8(len(slots))
	copy(out[1:], slots)
	return out, nil
}

// ----------------------------------------------------------------- helpers

// objBuf accumulates per-object bytes starting with the mandatory
// [type, num_props] header. Widths come straight from the spec tables
// p.21–27 — no padding, all multi-byte fields big-endian.
type objBuf struct {
	buf []byte
}

func newBuf(typ iacp1.ObjectType, nProps uint8) *objBuf {
	return &objBuf{buf: []byte{uint8(typ), nProps}}
}

func (b *objBuf) u8(v uint8) { b.buf = append(b.buf, v) }
func (b *objBuf) i16(v int16) {
	var tmp [2]byte
	binary.BigEndian.PutUint16(tmp[:], uint16(v))
	b.buf = append(b.buf, tmp[:]...)
}
func (b *objBuf) u32(v uint32) {
	var tmp [4]byte
	binary.BigEndian.PutUint32(tmp[:], v)
	b.buf = append(b.buf, tmp[:]...)
}
func (b *objBuf) i32(v int32) {
	var tmp [4]byte
	binary.BigEndian.PutUint32(tmp[:], uint32(v))
	b.buf = append(b.buf, tmp[:]...)
}
func (b *objBuf) f32(v float32) {
	var tmp [4]byte
	binary.BigEndian.PutUint32(tmp[:], math.Float32bits(v))
	b.buf = append(b.buf, tmp[:]...)
}
func (b *objBuf) cstr(s string) {
	b.buf = append(b.buf, []byte(s)...)
	b.buf = append(b.buf, 0)
}
func (b *objBuf) cstrWithLimit(s string, limit int) {
	if len(s) > limit {
		s = s[:limit]
	}
	b.cstr(s)
}
func (b *objBuf) bytes() []byte { return b.buf }

func writeI16(v int16) []byte {
	out := make([]byte, 2)
	binary.BigEndian.PutUint16(out, uint16(v))
	return out
}

func writeI32(v int32) []byte {
	out := make([]byte, 4)
	binary.BigEndian.PutUint32(out, uint32(v))
	return out
}

func writeU32(v uint32) []byte {
	out := make([]byte, 4)
	binary.BigEndian.PutUint32(out, v)
	return out
}

func writeF32(v float32) []byte {
	out := make([]byte, 4)
	binary.BigEndian.PutUint32(out, math.Float32bits(v))
	return out
}

func writeCStr(s string) []byte {
	out := make([]byte, len(s)+1)
	copy(out, s)
	return out
}

// limitLabel truncates identifiers to the spec-p.20 max (MaxLabelLen
// excluding the NUL terminator). A real Axon device won't emit longer.
func limitLabel(s string) string {
	if len(s) > iacp1.MaxLabelLen {
		return s[:iacp1.MaxLabelLen]
	}
	return s
}

func limitString(s string, maxLen int) string {
	if maxLen > 0 && len(s) > maxLen {
		return s[:maxLen]
	}
	return s
}

func unitOf(p *canonical.Parameter) string {
	if p.Unit == nil {
		return ""
	}
	u := *p.Unit
	if len(u) > iacp1.MaxUnitLen {
		u = u[:iacp1.MaxUnitLen]
	}
	return u
}

// stringMaxLen pulls the max-length from Parameter.Format "maxLen=N"
// (mirrors canonicalize.go's convention). Defaults to 0 when absent;
// the caller then treats 0 as "device does not declare a limit".
func stringMaxLen(p *canonical.Parameter) uint8 {
	if p.Format == nil {
		return 0
	}
	for _, kv := range strings.Split(*p.Format, ",") {
		kv = strings.TrimSpace(kv)
		if strings.HasPrefix(kv, "maxLen=") {
			n, err := strconv.Atoi(strings.TrimPrefix(kv, "maxLen="))
			if err == nil && n > 0 && n <= math.MaxUint8 {
				return uint8(n)
			}
		}
	}
	return 0
}

func enumItems(p *canonical.Parameter) []string {
	if len(p.EnumMap) > 0 {
		out := make([]string, len(p.EnumMap))
		for i, e := range p.EnumMap {
			out[i] = e.Key
		}
		return out
	}
	if p.Enumeration != nil && *p.Enumeration != "" {
		// Consumer canonicalize.go joins items with "\n"; accept both.
		raw := *p.Enumeration
		if strings.Contains(raw, "\n") {
			return strings.Split(raw, "\n")
		}
		return strings.Split(raw, ",")
	}
	return nil
}

// alarmPriorityTag reads Format hint "alarm:priority=N,tag=M" if
// present; otherwise (priority=1, tag=0). Priority 0 = disabled per
// spec p.25 so we default to 1 (enabled). Tag is an Axon-assigned id
// with no schema-level default.
func alarmPriorityTag(p *canonical.Parameter) (uint8, uint8) {
	priority, tag := uint8(1), uint8(0)
	if p.Format == nil {
		return priority, tag
	}
	for _, kv := range strings.Split(*p.Format, ",") {
		kv = strings.TrimSpace(kv)
		switch {
		case strings.HasPrefix(kv, "priority="):
			if n, err := strconv.Atoi(strings.TrimPrefix(kv, "priority=")); err == nil && n >= 0 && n <= math.MaxUint8 {
				priority = uint8(n)
			}
		case strings.HasPrefix(kv, "tag="):
			if n, err := strconv.Atoi(strings.TrimPrefix(kv, "tag=")); err == nil && n >= 0 && n <= math.MaxUint8 {
				tag = uint8(n)
			}
		}
	}
	return priority, tag
}

// alarmMessages pulls the "on:/off:" messages from Parameter.Description
// — mirroring canonicalize.go's alarm output formatting.
func alarmMessages(p *canonical.Parameter) (string, string) {
	if p.Description == nil {
		return "", ""
	}
	onMsg, offMsg := "", ""
	for _, part := range strings.Split(*p.Description, " / ") {
		part = strings.TrimSpace(part)
		switch {
		case strings.HasPrefix(part, "on:"):
			onMsg = strings.TrimSpace(strings.TrimPrefix(part, "on:"))
		case strings.HasPrefix(part, "off:"):
			offMsg = strings.TrimSpace(strings.TrimPrefix(part, "off:"))
		}
	}
	return onMsg, offMsg
}

// frameSlotStatuses pulls the slot status bytes from the Frame
// Parameter's Value. Canonical carries this as []any of int64 (decoded
// from JSON array of numbers).
func frameSlotStatuses(p *canonical.Parameter) ([]uint8, error) {
	arr, ok := p.Value.([]any)
	if !ok {
		return nil, fmt.Errorf("frame %q: value must be a JSON array of slot status bytes", p.Identifier)
	}
	out := make([]uint8, 0, len(arr))
	for i, el := range arr {
		n, err := anyToInt(el, "slot_status["+strconv.Itoa(i)+"]")
		if err != nil {
			return nil, err
		}
		if n < 0 || n > math.MaxUint8 {
			return nil, fmt.Errorf("frame %q: slot_status[%d] out of range: %d", p.Identifier, i, n)
		}
		out = append(out, uint8(n))
	}
	return out, nil
}

// ------------------------------------------------------------ any -> typed

// anyToInt accepts int / int64 / float64 / json.Number-like types and
// returns int64 — JSON unmarshals numbers as float64 by default so we
// have to accept that too.
func anyToInt(v any, field string) (int64, error) {
	switch x := v.(type) {
	case nil:
		return 0, fmt.Errorf("%s: missing", field)
	case int:
		return int64(x), nil
	case int64:
		return x, nil
	case uint8:
		return int64(x), nil
	case uint16:
		return int64(x), nil
	case uint32:
		return int64(x), nil
	case int16:
		return int64(x), nil
	case int32:
		return int64(x), nil
	case float32:
		if float32(int64(x)) != x {
			return 0, fmt.Errorf("%s: %v has fractional part, want integer", field, x)
		}
		return int64(x), nil
	case float64:
		if float64(int64(x)) != x {
			return 0, fmt.Errorf("%s: %v has fractional part, want integer", field, x)
		}
		return int64(x), nil
	case bool:
		if x {
			return 1, nil
		}
		return 0, nil
	case string:
		n, err := strconv.ParseInt(x, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("%s: %q is not an integer", field, x)
		}
		return n, nil
	}
	return 0, fmt.Errorf("%s: unexpected type %T", field, v)
}

func anyToFloat(v any, field string) (float64, error) {
	switch x := v.(type) {
	case nil:
		return 0, fmt.Errorf("%s: missing", field)
	case float32:
		return float64(x), nil
	case float64:
		return x, nil
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
	n, err := anyToInt(v, field)
	if err != nil {
		return 0, err
	}
	return float64(n), nil
}

func asInt16(v any, field string) (int16, error) {
	n, err := anyToInt(v, field)
	if err != nil {
		return 0, err
	}
	if n < math.MinInt16 || n > math.MaxInt16 {
		return 0, fmt.Errorf("%s: %d out of int16 range", field, n)
	}
	return int16(n), nil
}

func asInt16Opt(v any, field string, defaultVal int16) (int16, error) {
	if v == nil {
		return defaultVal, nil
	}
	return asInt16(v, field)
}

func asInt32(v any, field string) (int32, error) {
	n, err := anyToInt(v, field)
	if err != nil {
		return 0, err
	}
	if n < math.MinInt32 || n > math.MaxInt32 {
		return 0, fmt.Errorf("%s: %d out of int32 range", field, n)
	}
	return int32(n), nil
}

func asInt32Opt(v any, field string, defaultVal int32) (int32, error) {
	if v == nil {
		return defaultVal, nil
	}
	return asInt32(v, field)
}

func asUint8(v any, field string) (uint8, error) {
	n, err := anyToInt(v, field)
	if err != nil {
		return 0, err
	}
	if n < 0 || n > math.MaxUint8 {
		return 0, fmt.Errorf("%s: %d out of uint8 range", field, n)
	}
	return uint8(n), nil
}

func asUint8Opt(v any, field string, defaultVal uint8) (uint8, error) {
	if v == nil {
		return defaultVal, nil
	}
	return asUint8(v, field)
}

func asFloat32(v any, field string) (float32, error) {
	f, err := anyToFloat(v, field)
	if err != nil {
		return 0, err
	}
	if f > math.MaxFloat32 || f < -math.MaxFloat32 {
		return 0, fmt.Errorf("%s: %g out of float32 range", field, f)
	}
	return float32(f), nil
}

func asFloat32Opt(v any, field string, defaultVal float32) (float32, error) {
	if v == nil {
		return defaultVal, nil
	}
	return asFloat32(v, field)
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

func asBool(v any, field string) (bool, error) {
	if v == nil {
		return false, nil
	}
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("%s: expected boolean, got %T", field, v)
	}
	return b, nil
}

// ipv4ToUint32 parses canonical IP storage ("a.b.c.d" dotted-quad
// string) into a uint32 network value per spec p.22. Matches the
// consumer's canonicalize.go output ("1.2.3.4").
func ipv4ToUint32(v any) (uint32, error) {
	if v == nil {
		return 0, fmt.Errorf("ipv4: missing value")
	}
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

func ipv4ToUint32Opt(v any, defaultVal uint32) (uint32, error) {
	if v == nil {
		return defaultVal, nil
	}
	return ipv4ToUint32(v)
}
