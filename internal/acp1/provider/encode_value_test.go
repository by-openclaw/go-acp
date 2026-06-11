package acp1

import (
	"testing"

	"dhs/internal/acp1/codec"
	"dhs/internal/export/canonical"
)

// TestEncodeValue_AllTypes drives the getValue payload encoder across every
// ACP1 type (the encodeObject round-trip tests cover the full-property path;
// this covers the value-only path + frameSlotStatuses + asBool).
func TestEncodeValue_AllTypes(t *testing.T) {
	cases := []struct {
		name string
		e    *entry
		want int
	}{
		{"integer", &entry{acpType: codec.TypeInteger, param: param("i", canonical.ParamInteger, withValue(int64(5)))}, 2},
		{"long", &entry{acpType: codec.TypeLong, param: param("l", canonical.ParamInteger, withFormat("int32"), withValue(int64(100000)))}, 4},
		{"byte", &entry{acpType: codec.TypeByte, param: param("b", canonical.ParamInteger, withFormat("uint8"), withValue(int64(200)))}, 1},
		{"float", &entry{acpType: codec.TypeFloat, param: param("f", canonical.ParamReal, withValue(float64(1.5)))}, 4},
		{"ipaddr", &entry{acpType: codec.TypeIPAddr, param: param("ip", canonical.ParamString, withFormat("ipv4"), withValue("10.0.0.1"))}, 4},
		{"enum", &entry{acpType: codec.TypeEnum, param: param("e", canonical.ParamEnum, withValue(int64(1)))}, 1},
		{"string", &entry{acpType: codec.TypeString, param: param("s", canonical.ParamString, withValue("hi"))}, 3},
		{"alarm-true", &entry{acpType: codec.TypeAlarm, param: param("a", canonical.ParamBoolean, withFormat("alarm"), withValue(true))}, 1},
		{"alarm-false", &entry{acpType: codec.TypeAlarm, param: param("a", canonical.ParamBoolean, withFormat("alarm"), withValue(false))}, 1},
		{"file", &entry{acpType: codec.TypeFile, param: param("file", canonical.ParamString, withFormat("file"), withDefault(int64(3)))}, 2},
		{"frame", &entry{acpType: codec.TypeFrame, param: param("frame", canonical.ParamOctets, withFormat("frame"), withValue([]any{int64(2), int64(0), int64(2)}))}, 4},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b, err := encodeValue(c.e)
			if err != nil {
				t.Fatalf("encodeValue(%s): %v", c.name, err)
			}
			if len(b) != c.want {
				t.Errorf("%s wire len = %d, want %d", c.name, len(b), c.want)
			}
		})
	}

	// nil entry → error.
	if _, err := encodeValue(nil); err == nil {
		t.Error("encodeValue(nil) should error")
	}
	if _, err := encodeValue(&entry{acpType: codec.TypeInteger}); err == nil {
		t.Error("encodeValue with nil param should error")
	}
	// unsupported type → error.
	if _, err := encodeValue(&entry{acpType: codec.TypeRoot, param: &canonical.Parameter{}}); err == nil {
		t.Error("encodeValue(Root) should be unsupported")
	}
}

func TestMethodSupported_FullMatrix(t *testing.T) {
	types := []codec.ObjectType{
		codec.TypeRoot, codec.TypeInteger, codec.TypeLong, codec.TypeFloat,
		codec.TypeByte, codec.TypeIPAddr, codec.TypeEnum, codec.TypeString,
		codec.TypeAlarm, codec.TypeFile, codec.TypeFrame, codec.ObjectType(99),
	}
	methods := []codec.Method{
		codec.MethodGetValue, codec.MethodSetValue, codec.MethodSetIncValue,
		codec.MethodSetDecValue, codec.MethodSetDefValue, codec.MethodGetObject,
		codec.Method(0xEE),
	}
	// Spot-check a few invariants while sweeping every cell for coverage.
	for _, ty := range types {
		for _, m := range methods {
			_ = methodSupported(ty, m)
		}
	}
	if methodSupported(codec.TypeString, codec.MethodSetIncValue) {
		t.Error("string must not support inc")
	}
	if !methodSupported(codec.TypeByte, codec.MethodSetDefValue) {
		t.Error("byte must support setDef")
	}
	if methodSupported(codec.ObjectType(99), codec.MethodGetValue) {
		t.Error("unknown type supports nothing")
	}
}

func TestSnapFloat64(t *testing.T) {
	if got := snapFloat64(2.3, 0, 1); got != 2 {
		t.Errorf("snap 2.3 step1 = %v, want 2", got)
	}
	if got := snapFloat64(2.7, 0, 0.5); got != 2.5 {
		t.Errorf("snap 2.7 step0.5 = %v, want 2.5", got)
	}
	if got := snapFloat64(2.3, 0, 0); got != 2.3 {
		t.Errorf("snap with step 0 should pass through, got %v", got)
	}
}
