package acp1

import (
	"testing"

	"dhs/internal/acp1/codec"
	"dhs/internal/consumer"
)

func TestDecodeValueBytes_AllTypes(t *testing.T) {
	enumObj := consumer.Object{Kind: consumer.KindEnum, EnumItems: []string{"Off", "On", "Auto"}}

	// Happy paths.
	if v, err := DecodeValueBytes(consumer.Object{Kind: consumer.KindInt}, codec.TypeInteger, []byte{0xFF, 0xFB}); err != nil || v.Int != -5 {
		t.Errorf("integer = %d,%v want -5", v.Int, err)
	}
	if v, err := DecodeValueBytes(consumer.Object{Kind: consumer.KindInt}, codec.TypeLong, []byte{0x00, 0x01, 0x00, 0x00}); err != nil || v.Int != 65536 {
		t.Errorf("long = %d,%v want 65536", v.Int, err)
	}
	if v, err := DecodeValueBytes(consumer.Object{Kind: consumer.KindUint}, codec.TypeByte, []byte{0x2A}); err != nil || v.Uint != 42 {
		t.Errorf("byte = %d,%v want 42", v.Uint, err)
	}
	if v, err := DecodeValueBytes(consumer.Object{Kind: consumer.KindFloat}, codec.TypeFloat, []byte{0x3F, 0x80, 0x00, 0x00}); err != nil || v.Float != 1.0 {
		t.Errorf("float = %v,%v want 1.0", v.Float, err)
	}
	if v, err := DecodeValueBytes(consumer.Object{Kind: consumer.KindIPAddr}, codec.TypeIPAddr, []byte{192, 168, 1, 5}); err != nil || v.IPAddr != [4]byte{192, 168, 1, 5} {
		t.Errorf("ipaddr = %v,%v", v.IPAddr, err)
	}
	if v, err := DecodeValueBytes(enumObj, codec.TypeEnum, []byte{1}); err != nil || v.Enum != 1 || v.Str != "On" {
		t.Errorf("enum = %d/%q,%v want 1/On", v.Enum, v.Str, err)
	}
	// Enum index beyond items → no Str resolved, no error.
	if v, err := DecodeValueBytes(enumObj, codec.TypeEnum, []byte{9}); err != nil || v.Str != "" {
		t.Errorf("enum oob = %q,%v", v.Str, err)
	}
	if v, err := DecodeValueBytes(consumer.Object{Kind: consumer.KindString}, codec.TypeString, []byte{'h', 'i', 0, 'x'}); err != nil || v.Str != "hi" {
		t.Errorf("string NUL = %q,%v want hi", v.Str, err)
	}
	if v, err := DecodeValueBytes(consumer.Object{Kind: consumer.KindString}, codec.TypeString, []byte{'y', 'o'}); err != nil || v.Str != "yo" {
		t.Errorf("string no-NUL = %q,%v want yo", v.Str, err)
	}
	if v, err := DecodeValueBytes(consumer.Object{Kind: consumer.KindAlarm}, codec.TypeAlarm, []byte{3}); err != nil || v.Kind != consumer.KindUint || v.Uint != 3 {
		t.Errorf("alarm = %+v,%v", v, err)
	}
	if v, err := DecodeValueBytes(consumer.Object{}, codec.TypeFrame, []byte{2, 2, 0}); err != nil || v.Kind != consumer.KindFrame || len(v.SlotStatus) != 2 {
		t.Errorf("frame = %+v,%v", v, err)
	}
	if v, err := DecodeValueBytes(consumer.Object{}, codec.TypeFile, []byte{1, 2, 3}); err != nil || v.Kind != consumer.KindRaw {
		t.Errorf("file raw = %+v,%v", v, err)
	}

	// Error paths: too-short buffers per type + unsupported type.
	short := map[codec.ObjectType][]byte{
		codec.TypeInteger: {0x00},
		codec.TypeLong:    {0x00, 0x00},
		codec.TypeByte:    {},
		codec.TypeFloat:   {0x00, 0x00},
		codec.TypeIPAddr:  {1, 2, 3},
		codec.TypeEnum:    {},
		codec.TypeAlarm:   {},
		codec.TypeFrame:   {}, // empty frame
	}
	for ty, raw := range short {
		if _, err := DecodeValueBytes(consumer.Object{}, ty, raw); err == nil {
			t.Errorf("type %d short buffer: want error", ty)
		}
	}
	// Frame declares more slots than present.
	if _, err := DecodeValueBytes(consumer.Object{}, codec.TypeFrame, []byte{5, 1}); err == nil {
		t.Error("frame under-length: want error")
	}
	// Unsupported type.
	if _, err := DecodeValueBytes(consumer.Object{}, codec.ObjectType(99), []byte{1}); err == nil {
		t.Error("unsupported type: want error")
	}
}

func TestEncodeValueBytes_AllTypes(t *testing.T) {
	cases := []struct {
		name string
		obj  consumer.Object
		ty   codec.ObjectType
		val  consumer.Value
		want int // wire length
	}{
		{"integer", consumer.Object{}, codec.TypeInteger, consumer.Value{Int: 5}, 2},
		{"long", consumer.Object{}, codec.TypeLong, consumer.Value{Int: 70000}, 4},
		{"byte", consumer.Object{}, codec.TypeByte, consumer.Value{Uint: 200}, 1},
		{"float", consumer.Object{}, codec.TypeFloat, consumer.Value{Float: 1.5}, 4},
		{"ipaddr", consumer.Object{}, codec.TypeIPAddr, consumer.Value{Str: "10.0.0.1"}, 4},
		{"enum-label", consumer.Object{EnumItems: []string{"Off", "On"}}, codec.TypeEnum, consumer.Value{Str: "On"}, 1},
		{"string", consumer.Object{MaxLen: 8}, codec.TypeString, consumer.Value{Str: "hi"}, 3},
	}
	for _, c := range cases {
		b, err := EncodeValueBytes(c.obj, c.ty, c.val)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if len(b) != c.want {
			t.Errorf("%s len = %d, want %d", c.name, len(b), c.want)
		}
	}

	// Error paths.
	errCases := []struct {
		name string
		obj  consumer.Object
		ty   codec.ObjectType
		val  consumer.Value
	}{
		{"int-range", consumer.Object{}, codec.TypeInteger, consumer.Value{Int: 99999}},
		{"long-range", consumer.Object{}, codec.TypeLong, consumer.Value{Int: 1 << 40}},
		{"byte-range", consumer.Object{}, codec.TypeByte, consumer.Value{Uint: 999}},
		{"float-bad", consumer.Object{}, codec.TypeFloat, consumer.Value{Str: "x"}},
		{"ip-bad", consumer.Object{}, codec.TypeIPAddr, consumer.Value{Str: "1.2.3"}},
		{"enum-bad", consumer.Object{EnumItems: []string{"Off", "On"}}, codec.TypeEnum, consumer.Value{Str: "Nope"}},
		{"string-toolong", consumer.Object{MaxLen: 2}, codec.TypeString, consumer.Value{Str: "toolong"}},
		{"unsupported", consumer.Object{}, codec.TypeFrame, consumer.Value{}},
	}
	for _, c := range errCases {
		if _, err := EncodeValueBytes(c.obj, c.ty, c.val); err == nil {
			t.Errorf("%s: want error", c.name)
		}
	}
}

func TestCoerceHelpers(t *testing.T) {
	// coerceInt precedence + errors.
	if n, err := coerceInt(consumer.Value{Str: "0x10"}, 0, 255); err != nil || n != 16 {
		t.Errorf("coerceInt str hex = %d,%v want 16", n, err)
	}
	if n, err := coerceInt(consumer.Value{Uint: 7}, 0, 255); err != nil || n != 7 {
		t.Errorf("coerceInt uint = %d,%v", n, err)
	}
	if n, err := coerceInt(consumer.Value{Float: 3.9}, 0, 255); err != nil || n != 3 {
		t.Errorf("coerceInt float trunc = %d,%v", n, err)
	}
	if _, err := coerceInt(consumer.Value{Str: "notnum"}, 0, 255); err == nil {
		t.Error("coerceInt bad str: want error")
	}
	if _, err := coerceInt(consumer.Value{Int: 5}, 0, 1); err == nil {
		t.Error("coerceInt out-of-range: want error")
	}

	// coerceUint branches.
	if n, err := coerceUint(consumer.Value{Int: 9}, 255); err != nil || n != 9 {
		t.Errorf("coerceUint int = %d,%v", n, err)
	}
	if n, err := coerceUint(consumer.Value{Float: 4.5}, 255); err != nil || n != 4 {
		t.Errorf("coerceUint float = %d,%v", n, err)
	}
	if _, err := coerceUint(consumer.Value{Str: "bad"}, 255); err == nil {
		t.Error("coerceUint bad str: want error")
	}

	// coerceFloat branches.
	if f, err := coerceFloat(consumer.Value{Int: 3}); err != nil || f != 3 {
		t.Errorf("coerceFloat int = %v,%v", f, err)
	}
	if f, err := coerceFloat(consumer.Value{Uint: 4}); err != nil || f != 4 {
		t.Errorf("coerceFloat uint = %v,%v", f, err)
	}
	if f, err := coerceFloat(consumer.Value{}); err != nil || f != 0 {
		t.Errorf("coerceFloat zero = %v,%v", f, err)
	}

	// coerceIP branches.
	if u, err := coerceIP(consumer.Value{Uint: 0x0A000001}); err != nil || u != 0x0A000001 {
		t.Errorf("coerceIP uint = %x,%v", u, err)
	}
	if u, err := coerceIP(consumer.Value{IPAddr: [4]byte{10, 0, 0, 2}}); err != nil || u != 0x0A000002 {
		t.Errorf("coerceIP field = %x,%v", u, err)
	}
	if _, err := coerceIP(consumer.Value{Str: "1.2.3.999"}); err == nil {
		t.Error("coerceIP bad octet: want error")
	}

	// coerceEnum branches.
	items := []string{"Off", "On", "Auto"}
	if idx, err := coerceEnum(items, consumer.Value{Str: "2"}); err != nil || idx != 2 {
		t.Errorf("coerceEnum numeric = %d,%v", idx, err)
	}
	if idx, err := coerceEnum(items, consumer.Value{Enum: 1}); err != nil || idx != 1 {
		t.Errorf("coerceEnum index = %d,%v", idx, err)
	}
	if _, err := coerceEnum(items, consumer.Value{Enum: 9}); err == nil {
		t.Error("coerceEnum index oob: want error")
	}
}
