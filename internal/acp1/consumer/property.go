package acp1

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// DecodedObject is the typed representation of a getObject reply for one
// AxonNet object. Fields not applicable to the object's type stay zero.
//
// The decoder is spec-driven: it reads the object-type byte and num-props
// byte first, then parses the remaining bytes according to the per-type
// tables in spec v1.4 §"Objects by Type" pages 21–27.
//
// All numeric fields are decoded from big-endian wire bytes per spec p. 20:
// "All integer values, long values and float values are transmitted with
// the MSB first."
//
// All strings are NUL-terminated ASCII per spec p. 20. The NUL byte is
// consumed by the decoder and stripped from the returned Go string.
type DecodedObject struct {
	Type       ObjectType
	NumProps   uint8
	Access     uint8
	Label      string // common to most objects
	Unit       string // Integer, IPAddr, Float, Long, Byte

	// Numeric value fields. Which one is populated depends on Type.
	// Only one of {IntVal, UintVal, FloatVal, ByteVal} carries the value.
	IntVal     int64   // Integer (int16), Long (int32)
	UintVal    uint64  // IPAddr (uint32)
	FloatVal   float64 // Float (float32)
	ByteVal    uint8   // Byte, Enum current index

	// Numeric constraints. Same representation rules as *Val above.
	DefInt     int64
	DefUint    uint64
	DefFloat   float64
	DefByte    uint8

	StepInt    int64
	StepUint   uint64
	StepFloat  float64
	StepByte   uint8

	MinInt     int64
	MinUint    uint64
	MinFloat   float64
	MinByte    uint8

	MaxInt     int64
	MaxUint    uint64
	MaxFloat   float64
	MaxByte    uint8

	// Root
	BootMode       uint8
	NumIdentity    uint8
	NumControl     uint8
	NumStatus      uint8
	NumAlarm       uint8
	NumFile        uint8

	// Enum
	NumItems  uint8
	EnumItems []string

	// String
	StrValue string
	MaxLen   uint8

	// Alarm
	Priority    uint8
	Tag         uint8
	EventOnMsg  string
	EventOffMsg string

	// Frame (frame-status only lives on the rack controller)
	NumSlots   uint8
	SlotStatus []uint8

	// File
	NumFragments int16
	FileName     string
}

// ErrBadProperty is returned when the decoder runs off the end of the
// input buffer. Wraps the concrete offset and requested length for logs.
var ErrBadProperty = errors.New("acp1: property decode out of bounds")

// DecodeObject parses the Value bytes returned by a getObject reply into
// a typed DecodedObject. The first byte is always the object-type byte
// (spec p. 19 §"Property Object Type"). The second byte is always the
// num-properties byte. After that the layout is type-specific.
//
// Defensive: every read is bounds-checked. A truncated or malformed reply
// returns ErrBadProperty with an offset/length context.
//
// Common prefix (all object types):
//
//	| Byte | Field       | Notes                                          |
//	|------|-------------|------------------------------------------------|
//	|  0   | object_type |  0=Root, 1=Integer, 2=IPAddr, 3=Float, 4=Enum  |
//	|      |             |  5=String, 6=Frame, 7=Alarm, 8=File, 9=Long,   |
//	|      |             |  10=Byte, 11=Reserved                          |
//	|  1   | num_props   | number of properties following                 |
//	| 2..  | (type-specific fields — see per-type decoders) |
//
// Spec reference: AXON-ACP_v1_4.pdf §"Property Object Type" p. 19 and
// §"Objects by Type" pp. 21–27.
func DecodeObject(value []byte) (*DecodedObject, error) {
	r := &reader{buf: value}

	typeByte, err := r.u8()
	if err != nil {
		return nil, fmt.Errorf("%w: read type", ErrBadProperty)
	}
	numProps, err := r.u8()
	if err != nil {
		return nil, fmt.Errorf("%w: read num_properties", ErrBadProperty)
	}
	obj := &DecodedObject{
		Type:     ObjectType(typeByte),
		NumProps: numProps,
	}

	switch obj.Type {
	case TypeRoot:
		return decodeRoot(obj, r)
	case TypeInteger:
		return decodeInteger(obj, r)
	case TypeIPAddr:
		return decodeIPAddr(obj, r)
	case TypeFloat:
		return decodeFloat(obj, r)
	case TypeEnum:
		return decodeEnum(obj, r)
	case TypeString:
		return decodeString(obj, r)
	case TypeFrame:
		return decodeFrame(obj, r)
	case TypeAlarm:
		return decodeAlarm(obj, r)
	case TypeFile:
		return decodeFile(obj, r)
	case TypeLong:
		return decodeLong(obj, r)
	case TypeByte:
		return decodeByte(obj, r)
	case TypeReserved:
		return nil, fmt.Errorf("acp1: reserved object type 11")
	default:
		return nil, fmt.Errorf("acp1: unknown object type %d", obj.Type)
	}
}

// decodeRoot parses the Root Object (type=0). Spec p. 21:
//
//	pid 0  object_type   = 0     (consumed before dispatch)
//	pid 1  num_props     = 9     (consumed before dispatch)
//	pid 2  access        byte
//	pid 3  boot_mode     byte    (the "value" property of Root)
//	pid 4  num_identity  byte
//	pid 5  num_control   byte
//	pid 6  num_status    byte
//	pid 7  num_alarm     byte
//	pid 8  num_file      byte
//
// Wire layout after the common prefix (7 bytes):
//
//	| Byte | Field         | Width | Notes                                |
//	|------|---------------|-------|--------------------------------------|
//	|  0   | access        |   1   | bit0=r, bit1=w, bit2=setDef          |
//	|  1   | boot_mode     |   1   | Root's "value" property              |
//	|  2   | num_identity  |   1   | count of Identity objects            |
//	|  3   | num_control   |   1   | count of Control objects             |
//	|  4   | num_status    |   1   | count of Status objects              |
//	|  5   | num_alarm     |   1   | count of Alarm objects               |
//	|  6   | num_file      |   1   | count of File objects                |
//
// Spec reference: AXON-ACP_v1_4.pdf §"Objects by Type" p. 21.
func decodeRoot(o *DecodedObject, r *reader) (*DecodedObject, error) {
	var err error
	if o.Access, err = r.u8(); err != nil {
		return nil, wrap(err, "root access")
	}
	if o.BootMode, err = r.u8(); err != nil {
		return nil, wrap(err, "root boot_mode")
	}
	if o.NumIdentity, err = r.u8(); err != nil {
		return nil, wrap(err, "root num_identity")
	}
	if o.NumControl, err = r.u8(); err != nil {
		return nil, wrap(err, "root num_control")
	}
	if o.NumStatus, err = r.u8(); err != nil {
		return nil, wrap(err, "root num_status")
	}
	if o.NumAlarm, err = r.u8(); err != nil {
		return nil, wrap(err, "root num_alarm")
	}
	if o.NumFile, err = r.u8(); err != nil {
		return nil, wrap(err, "root num_file")
	}
	return o, nil
}

// decodeInteger parses the Integer Object (type=1, 10 props). Spec p. 22:
//
//	access, value(int16), default(int16), step(int16), min(int16),
//	max(int16), label(string), unit(string)
//
// Wire layout after the common prefix:
//
//	| Byte   | Field   | Width | Notes                                   |
//	|--------|---------|-------|-----------------------------------------|
//	|  0     | access  |   1   | bit0=r, bit1=w, bit2=setDef             |
//	|  1..2  | value   |   2   | int16 big-endian                        |
//	|  3..4  | default |   2   | int16 big-endian                        |
//	|  5..6  | step    |   2   | int16 big-endian                        |
//	|  7..8  | min     |   2   | int16 big-endian                        |
//	|  9..10 | max     |   2   | int16 big-endian                        |
//	| 11..   | label   |  ≤17  | NUL-terminated ASCII (max 16 + \0)      |
//	|  ...   | unit    |  ≤5   | NUL-terminated ASCII (max 4 + \0); opt. |
//
// Spec reference: AXON-ACP_v1_4.pdf §"Objects by Type" p. 22.
func decodeInteger(o *DecodedObject, r *reader) (*DecodedObject, error) {
	var err error
	if o.Access, err = r.u8(); err != nil {
		return nil, wrap(err, "integer access")
	}
	if o.IntVal, err = r.i16(); err != nil {
		return nil, wrap(err, "integer value")
	}
	if o.DefInt, err = r.i16(); err != nil {
		return nil, wrap(err, "integer default")
	}
	if o.StepInt, err = r.i16(); err != nil {
		return nil, wrap(err, "integer step")
	}
	if o.MinInt, err = r.i16(); err != nil {
		return nil, wrap(err, "integer min")
	}
	if o.MaxInt, err = r.i16(); err != nil {
		return nil, wrap(err, "integer max")
	}
	if o.Label, err = r.cstr(); err != nil {
		return nil, wrap(err, "integer label")
	}
	if o.Unit, err = r.cstr(); err != nil {
		// Some firmware omits the unit string on the wire. Tolerate EOF
		// here — a missing unit is not a decode failure.
		if !errors.Is(err, errEOF) {
			return nil, wrap(err, "integer unit")
		}
	}
	return o, nil
}

// decodeIPAddr — Spec p. 22. Layout identical to Integer but all numeric
// properties are uint32 (big-endian, MSB first).
//
// Wire layout after the common prefix:
//
//	| Byte    | Field   | Width | Notes                                  |
//	|---------|---------|-------|----------------------------------------|
//	|  0      | access  |   1   | bit0=r, bit1=w, bit2=setDef            |
//	|  1..4   | value   |   4   | uint32 big-endian (4 octets a.b.c.d)   |
//	|  5..8   | default |   4   | uint32 big-endian                      |
//	|  9..12  | step    |   4   | uint32 big-endian                      |
//	| 13..16  | min     |   4   | uint32 big-endian                      |
//	| 17..20  | max     |   4   | uint32 big-endian                      |
//	| 21..    | label   |  ≤17  | NUL-terminated ASCII                   |
//	|  ...    | unit    |  ≤5   | NUL-terminated ASCII; optional         |
//
// Spec reference: AXON-ACP_v1_4.pdf §"Objects by Type" p. 22.
func decodeIPAddr(o *DecodedObject, r *reader) (*DecodedObject, error) {
	var err error
	if o.Access, err = r.u8(); err != nil {
		return nil, wrap(err, "ipaddr access")
	}
	if o.UintVal, err = r.u32(); err != nil {
		return nil, wrap(err, "ipaddr value")
	}
	if o.DefUint, err = r.u32(); err != nil {
		return nil, wrap(err, "ipaddr default")
	}
	if o.StepUint, err = r.u32(); err != nil {
		return nil, wrap(err, "ipaddr step")
	}
	if o.MinUint, err = r.u32(); err != nil {
		return nil, wrap(err, "ipaddr min")
	}
	if o.MaxUint, err = r.u32(); err != nil {
		return nil, wrap(err, "ipaddr max")
	}
	if o.Label, err = r.cstr(); err != nil {
		return nil, wrap(err, "ipaddr label")
	}
	if o.Unit, err = r.cstr(); err != nil && !errors.Is(err, errEOF) {
		return nil, wrap(err, "ipaddr unit")
	}
	return o, nil
}

// decodeFloat — Spec p. 23. IEEE-754 single-precision, MSB first.
//
// Wire layout after the common prefix:
//
//	| Byte    | Field   | Width | Notes                                  |
//	|---------|---------|-------|----------------------------------------|
//	|  0      | access  |   1   | bit0=r, bit1=w, bit2=setDef            |
//	|  1..4   | value   |   4   | IEEE-754 float32 big-endian            |
//	|  5..8   | default |   4   | IEEE-754 float32 big-endian            |
//	|  9..12  | step    |   4   | IEEE-754 float32 big-endian            |
//	| 13..16  | min     |   4   | IEEE-754 float32 big-endian            |
//	| 17..20  | max     |   4   | IEEE-754 float32 big-endian            |
//	| 21..    | label   |  ≤17  | NUL-terminated ASCII                   |
//	|  ...    | unit    |  ≤5   | NUL-terminated ASCII; optional         |
//
// Spec reference: AXON-ACP_v1_4.pdf §"Objects by Type" p. 23.
func decodeFloat(o *DecodedObject, r *reader) (*DecodedObject, error) {
	var err error
	if o.Access, err = r.u8(); err != nil {
		return nil, wrap(err, "float access")
	}
	if o.FloatVal, err = r.f32(); err != nil {
		return nil, wrap(err, "float value")
	}
	if o.DefFloat, err = r.f32(); err != nil {
		return nil, wrap(err, "float default")
	}
	if o.StepFloat, err = r.f32(); err != nil {
		return nil, wrap(err, "float step")
	}
	if o.MinFloat, err = r.f32(); err != nil {
		return nil, wrap(err, "float min")
	}
	if o.MaxFloat, err = r.f32(); err != nil {
		return nil, wrap(err, "float max")
	}
	if o.Label, err = r.cstr(); err != nil {
		return nil, wrap(err, "float label")
	}
	if o.Unit, err = r.cstr(); err != nil && !errors.Is(err, errEOF) {
		return nil, wrap(err, "float unit")
	}
	return o, nil
}

// decodeEnum — Spec p. 23. Value is a 1-byte index into a comma-delimited
// NUL-terminated item list. The item list is the last field and may be
// empty. Label precedes the item list.
//
// Wire layout after the common prefix:
//
//	| Byte | Field      | Width | Notes                                     |
//	|------|------------|-------|-------------------------------------------|
//	|  0   | access     |   1   | bit0=r, bit1=w, bit2=setDef               |
//	|  1   | value      |   1   | u8 index into item_list                   |
//	|  2   | num_items  |   1   | number of items declared                  |
//	|  3   | default    |   1   | u8 index                                  |
//	|  4.. | label      |  ≤17  | NUL-terminated ASCII                      |
//	|  ... | item_list  |   ?   | comma-delimited NUL-terminated, e.g.      |
//	|      |            |       | "Off,On,Auto\0"; may be empty             |
//
// Spec reference: AXON-ACP_v1_4.pdf §"Objects by Type" p. 23.
func decodeEnum(o *DecodedObject, r *reader) (*DecodedObject, error) {
	var err error
	if o.Access, err = r.u8(); err != nil {
		return nil, wrap(err, "enum access")
	}
	if o.ByteVal, err = r.u8(); err != nil {
		return nil, wrap(err, "enum value")
	}
	if o.NumItems, err = r.u8(); err != nil {
		return nil, wrap(err, "enum num_items")
	}
	if o.DefByte, err = r.u8(); err != nil {
		return nil, wrap(err, "enum default")
	}
	if o.Label, err = r.cstr(); err != nil {
		return nil, wrap(err, "enum label")
	}
	// Item list is a single NUL-terminated string of comma-delimited items.
	// Spec p. 23 example: "Off,On,Auto\0".
	raw, err := r.cstr()
	if err != nil && !errors.Is(err, errEOF) {
		return nil, wrap(err, "enum item_list")
	}
	o.EnumItems = splitEnumItems(raw, int(o.NumItems))
	return o, nil
}

// splitEnumItems splits the comma-delimited enum string and pads or
// truncates to the declared NumItems count so callers never have to
// bounds-check when indexing by ByteVal.
func splitEnumItems(raw string, n int) []string {
	if raw == "" && n == 0 {
		return nil
	}
	out := make([]string, 0, n)
	start := 0
	for i := 0; i < len(raw); i++ {
		if raw[i] == ',' {
			out = append(out, raw[start:i])
			start = i + 1
		}
	}
	if start <= len(raw) {
		out = append(out, raw[start:])
	}
	// Normalise to exactly n items: pad with "" or truncate.
	if n > 0 {
		if len(out) < n {
			pad := make([]string, n-len(out))
			out = append(out, pad...)
		} else if len(out) > n {
			out = out[:n]
		}
	}
	return out
}

// decodeString — Spec p. 24. Value(string[MaxLen]) + MaxLen(byte) + Label.
// Order on the wire: Value, then MaxLen, then Label. The spec puts MaxLen
// AFTER the value, not before — verified against the C# reference parser.
//
// Wire layout after the common prefix:
//
//	| Byte | Field    | Width | Notes                                       |
//	|------|----------|-------|---------------------------------------------|
//	|  0   | access   |   1   | bit0=r, bit1=w, bit2=setDef                 |
//	|  1.. | value    |   ?   | NUL-terminated ASCII, bounded by max_len+\0 |
//	|  ... | max_len  |   1   | u8; declared buffer size (bytes)            |
//	|  ... | label    |  ≤17  | NUL-terminated ASCII; optional on wire      |
//
// Spec reference: AXON-ACP_v1_4.pdf §"Objects by Type" p. 24.
func decodeString(o *DecodedObject, r *reader) (*DecodedObject, error) {
	var err error
	if o.Access, err = r.u8(); err != nil {
		return nil, wrap(err, "string access")
	}
	if o.StrValue, err = r.cstr(); err != nil {
		return nil, wrap(err, "string value")
	}
	if o.MaxLen, err = r.u8(); err != nil {
		return nil, wrap(err, "string max_len")
	}
	if o.Label, err = r.cstr(); err != nil && !errors.Is(err, errEOF) {
		return nil, wrap(err, "string label")
	}
	return o, nil
}

// decodeFrame — Spec p. 24. Frame Status Object (type=6, 4 props).
// Access byte then NumOfSlots + SlotStatus array packed into one field.
//
// Wire layout after the common prefix:
//
//	| Byte      | Field         | Width | Notes                           |
//	|-----------|---------------|-------|---------------------------------|
//	|  0        | access        |   1   | read-only (bit0 set, 1)         |
//	|  1        | num_slots     |   1   | u8; number of cards in frame    |
//	|  2..n+1   | slot_status[] |   n   | one u8 per slot:                |
//	|           |               |       |  0=no-card, 1=powerup,          |
//	|           |               |       |  2=present, 3=error,            |
//	|           |               |       |  4=removed, 5=boot              |
//
// Spec reference: AXON-ACP_v1_4.pdf §"Objects by Type" p. 24.
func decodeFrame(o *DecodedObject, r *reader) (*DecodedObject, error) {
	var err error
	if o.Access, err = r.u8(); err != nil {
		return nil, wrap(err, "frame access")
	}
	if o.NumSlots, err = r.u8(); err != nil {
		return nil, wrap(err, "frame num_slots")
	}
	o.SlotStatus = make([]uint8, o.NumSlots)
	for i := 0; i < int(o.NumSlots); i++ {
		b, err := r.u8()
		if err != nil {
			return nil, wrap(err, "frame slot_status")
		}
		o.SlotStatus[i] = b
	}
	return o, nil
}

// decodeAlarm — Spec p. 25. 8 properties (since rev 1.2 added Event Off):
//
//	access, priority, tag, label, event_on_msg, event_off_msg
//
// Wire layout after the common prefix:
//
//	| Byte | Field         | Width | Notes                                |
//	|------|---------------|-------|--------------------------------------|
//	|  0   | access        |   1   | bit0=r, bit1=w, bit2=setDef          |
//	|  1   | priority      |   1   | u8; 0 = alarm disabled               |
//	|  2   | tag           |   1   | u8; device-assigned identifier       |
//	|  3.. | label         |  ≤17  | NUL-terminated ASCII (max 16 + \0)   |
//	|  ... | event_on_msg  |  ≤33  | NUL-terminated ASCII (max 32 + \0)   |
//	|  ... | event_off_msg |  ≤33  | NUL-terminated ASCII; optional       |
//
// Spec reference: AXON-ACP_v1_4.pdf §"Objects by Type" p. 25.
func decodeAlarm(o *DecodedObject, r *reader) (*DecodedObject, error) {
	var err error
	if o.Access, err = r.u8(); err != nil {
		return nil, wrap(err, "alarm access")
	}
	if o.Priority, err = r.u8(); err != nil {
		return nil, wrap(err, "alarm priority")
	}
	if o.Tag, err = r.u8(); err != nil {
		return nil, wrap(err, "alarm tag")
	}
	if o.Label, err = r.cstr(); err != nil {
		return nil, wrap(err, "alarm label")
	}
	if o.EventOnMsg, err = r.cstr(); err != nil && !errors.Is(err, errEOF) {
		return nil, wrap(err, "alarm event_on")
	}
	if o.EventOffMsg, err = r.cstr(); err != nil && !errors.Is(err, errEOF) {
		return nil, wrap(err, "alarm event_off")
	}
	return o, nil
}

// decodeFile — Spec p. 26. File Object (type=8, 5 props). The Fragment
// property is not returned by getObject per the spec note on p. 26.
//
// Wire layout after the common prefix:
//
//	| Byte   | Field         | Width | Notes                              |
//	|--------|---------------|-------|------------------------------------|
//	|  0     | access        |   1   | bit0=r, bit1=w, bit2=setDef        |
//	|  1..2  | num_fragments |   2   | int16 big-endian                   |
//	|  3..   | file_name     |  ≤17  | NUL-terminated ASCII (max 16 + \0) |
//
// Note: Fragment property is engineer-mode only and never returned by
// getObject on real Synapse firmware.
//
// Spec reference: AXON-ACP_v1_4.pdf §"Objects by Type" p. 26.
func decodeFile(o *DecodedObject, r *reader) (*DecodedObject, error) {
	var err error
	if o.Access, err = r.u8(); err != nil {
		return nil, wrap(err, "file access")
	}
	var frags int64
	if frags, err = r.i16(); err != nil {
		return nil, wrap(err, "file num_fragments")
	}
	o.NumFragments = int16(frags)
	if o.FileName, err = r.cstr(); err != nil && !errors.Is(err, errEOF) {
		return nil, wrap(err, "file name")
	}
	return o, nil
}

// decodeLong — Spec p. 26. Layout identical to Integer but int32 values.
//
// Wire layout after the common prefix:
//
//	| Byte    | Field   | Width | Notes                                  |
//	|---------|---------|-------|----------------------------------------|
//	|  0      | access  |   1   | bit0=r, bit1=w, bit2=setDef            |
//	|  1..4   | value   |   4   | int32 big-endian                       |
//	|  5..8   | default |   4   | int32 big-endian                       |
//	|  9..12  | step    |   4   | int32 big-endian                       |
//	| 13..16  | min     |   4   | int32 big-endian                       |
//	| 17..20  | max     |   4   | int32 big-endian                       |
//	| 21..    | label   |  ≤17  | NUL-terminated ASCII                   |
//	|  ...    | unit    |  ≤5   | NUL-terminated ASCII; optional         |
//
// Spec reference: AXON-ACP_v1_4.pdf §"Objects by Type" p. 26.
func decodeLong(o *DecodedObject, r *reader) (*DecodedObject, error) {
	var err error
	if o.Access, err = r.u8(); err != nil {
		return nil, wrap(err, "long access")
	}
	if o.IntVal, err = r.i32(); err != nil {
		return nil, wrap(err, "long value")
	}
	if o.DefInt, err = r.i32(); err != nil {
		return nil, wrap(err, "long default")
	}
	if o.StepInt, err = r.i32(); err != nil {
		return nil, wrap(err, "long step")
	}
	if o.MinInt, err = r.i32(); err != nil {
		return nil, wrap(err, "long min")
	}
	if o.MaxInt, err = r.i32(); err != nil {
		return nil, wrap(err, "long max")
	}
	if o.Label, err = r.cstr(); err != nil {
		return nil, wrap(err, "long label")
	}
	if o.Unit, err = r.cstr(); err != nil && !errors.Is(err, errEOF) {
		return nil, wrap(err, "long unit")
	}
	return o, nil
}

// decodeByte — Spec p. 27. Layout identical to Integer but u8 values.
//
// Wire layout after the common prefix:
//
//	| Byte | Field   | Width | Notes                                     |
//	|------|---------|-------|-------------------------------------------|
//	|  0   | access  |   1   | bit0=r, bit1=w, bit2=setDef               |
//	|  1   | value   |   1   | u8                                        |
//	|  2   | default |   1   | u8                                        |
//	|  3   | step    |   1   | u8                                        |
//	|  4   | min     |   1   | u8                                        |
//	|  5   | max     |   1   | u8                                        |
//	|  6.. | label   |  ≤17  | NUL-terminated ASCII                      |
//	|  ... | unit    |  ≤5   | NUL-terminated ASCII; optional            |
//
// Spec reference: AXON-ACP_v1_4.pdf §"Objects by Type" p. 27.
func decodeByte(o *DecodedObject, r *reader) (*DecodedObject, error) {
	var err error
	if o.Access, err = r.u8(); err != nil {
		return nil, wrap(err, "byte access")
	}
	if o.ByteVal, err = r.u8(); err != nil {
		return nil, wrap(err, "byte value")
	}
	if o.DefByte, err = r.u8(); err != nil {
		return nil, wrap(err, "byte default")
	}
	if o.StepByte, err = r.u8(); err != nil {
		return nil, wrap(err, "byte step")
	}
	if o.MinByte, err = r.u8(); err != nil {
		return nil, wrap(err, "byte min")
	}
	if o.MaxByte, err = r.u8(); err != nil {
		return nil, wrap(err, "byte max")
	}
	if o.Label, err = r.cstr(); err != nil {
		return nil, wrap(err, "byte label")
	}
	if o.Unit, err = r.cstr(); err != nil && !errors.Is(err, errEOF) {
		return nil, wrap(err, "byte unit")
	}
	return o, nil
}

// IsSubGroupMarker reports whether the decoded object is a section header
// used by Axon firmware to group Control/Status objects in the UI.
//
// Two conventions observed in the wild:
//
//  1. Enum type with exactly one item equal to " " (a single space).
//     Documented in the C# reference driver (ACPCard.NO_SUB_GROUP).
//  2. String type whose label starts with a space character. Observed on
//     modern firmware (e.g. on the 10.6.239.113 emulator) where section
//     headers like "  DOWN CONV", "  INSERTER", " NETWORK" appear as
//     read-only String objects with leading-whitespace labels.
//
// Neither convention is in the v1.4 spec. The walker recognises both so
// the CLI/UI tree view renders sections correctly regardless of which
// firmware generation produced them.
func (o *DecodedObject) IsSubGroupMarker() bool {
	if o.Type == TypeEnum && len(o.EnumItems) == 1 && o.EnumItems[0] == " " {
		return true
	}
	if o.Type == TypeString && len(o.Label) > 0 && (o.Label[0] == ' ' || o.Label[0] == '\t') {
		return true
	}
	return false
}

// ---------------------------------------------------------------- reader

// errEOF is the sentinel the reader returns when the buffer is exhausted.
// It is wrapped (not equal) with errors.Is so callers can special-case
// "optional trailing field missing" without importing io.EOF.
var errEOF = errors.New("acp1: property decode EOF")

type reader struct {
	buf []byte
	pos int
}

func (r *reader) remaining() int { return len(r.buf) - r.pos }

func (r *reader) need(n int) error {
	if r.remaining() < n {
		return fmt.Errorf("%w (pos=%d want=%d left=%d)", errEOF, r.pos, n, r.remaining())
	}
	return nil
}

func (r *reader) u8() (uint8, error) {
	if err := r.need(1); err != nil {
		return 0, err
	}
	v := r.buf[r.pos]
	r.pos++
	return v, nil
}

func (r *reader) i16() (int64, error) {
	if err := r.need(2); err != nil {
		return 0, err
	}
	v := int16(binary.BigEndian.Uint16(r.buf[r.pos:]))
	r.pos += 2
	return int64(v), nil
}

func (r *reader) u32() (uint64, error) {
	if err := r.need(4); err != nil {
		return 0, err
	}
	v := binary.BigEndian.Uint32(r.buf[r.pos:])
	r.pos += 4
	return uint64(v), nil
}

func (r *reader) i32() (int64, error) {
	if err := r.need(4); err != nil {
		return 0, err
	}
	v := int32(binary.BigEndian.Uint32(r.buf[r.pos:]))
	r.pos += 4
	return int64(v), nil
}

func (r *reader) f32() (float64, error) {
	if err := r.need(4); err != nil {
		return 0, err
	}
	bits := binary.BigEndian.Uint32(r.buf[r.pos:])
	r.pos += 4
	return float64(math.Float32frombits(bits)), nil
}

// cstr reads a NUL-terminated string. Consumes the NUL. Returns errEOF if
// no NUL is found before end-of-buffer.
func (r *reader) cstr() (string, error) {
	if r.remaining() == 0 {
		return "", errEOF
	}
	start := r.pos
	for r.pos < len(r.buf) {
		if r.buf[r.pos] == 0 {
			s := string(r.buf[start:r.pos])
			r.pos++ // skip the NUL
			return s, nil
		}
		r.pos++
	}
	return "", fmt.Errorf("%w: unterminated string starting at pos=%d", errEOF, start)
}

func wrap(err error, ctx string) error {
	return fmt.Errorf("%s: %w", ctx, err)
}
