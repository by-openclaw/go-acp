package acp1

import (
	"testing"

	"dhs/internal/acp1/codec"
	"dhs/internal/consumer"
)

func TestAnyAsInt64_AllKinds(t *testing.T) {
	ok := []any{
		int(1), int8(2), int16(3), int32(4), int64(5),
		uint(6), uint8(7), uint16(8), uint32(9), uint64(10),
		float32(11), float64(12), "13",
	}
	for i, v := range ok {
		if n, got := anyAsInt64(v); !got || n != int64(i+1) {
			t.Errorf("anyAsInt64(%v=%T) = %d,%v want %d", v, v, n, got, i+1)
		}
	}
	for _, v := range []any{nil, "xx", true, []byte{1}} {
		if _, got := anyAsInt64(v); got {
			t.Errorf("anyAsInt64(%v) should fail", v)
		}
	}
}

func TestReadIntBound_AllKinds(t *testing.T) {
	cases := []struct {
		in   any
		want int64
		ok   bool
	}{
		{int(5), 5, true}, {int64(6), 6, true}, {float64(7), 7, true},
		{"8", 8, true}, {"bad", 0, false}, {true, 0, false}, {nil, 0, false},
	}
	for _, c := range cases {
		got, ok := readIntBound(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("readIntBound(%v) = %d,%v want %d,%v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestAcpTypeFromObject_AllKinds(t *testing.T) {
	cases := []struct {
		o    consumer.Object
		want codec.ObjectType
	}{
		{consumer.Object{Kind: consumer.KindUint}, codec.TypeByte},
		{consumer.Object{Kind: consumer.KindInt, Min: int64(0), Max: int64(10)}, codec.TypeInteger},
		{consumer.Object{Kind: consumer.KindInt, Max: int64(100000)}, codec.TypeLong},
		{consumer.Object{Kind: consumer.KindFloat}, codec.TypeFloat},
		{consumer.Object{Kind: consumer.KindEnum}, codec.TypeEnum},
		{consumer.Object{Kind: consumer.KindIPAddr}, codec.TypeIPAddr},
		{consumer.Object{Kind: consumer.KindString}, codec.TypeString},
		{consumer.Object{Kind: consumer.KindAlarm}, codec.TypeAlarm},
		{consumer.Object{Kind: consumer.KindBool}, codec.TypeAlarm},
		{consumer.Object{Kind: consumer.KindFrame}, codec.TypeFrame},
		// Meta hint overrides Kind inference, in each numeric form.
		{consumer.Object{Kind: consumer.KindInt, Meta: map[string]any{"acp1_type": float64(codec.TypeFloat)}}, codec.TypeFloat},
		{consumer.Object{Kind: consumer.KindInt, Meta: map[string]any{"acp1_type": int(codec.TypeLong)}}, codec.TypeLong},
		{consumer.Object{Kind: consumer.KindInt, Meta: map[string]any{"acp1_type": int64(codec.TypeByte)}}, codec.TypeByte},
		{consumer.Object{Kind: consumer.KindInt, Meta: map[string]any{"acp1_type": codec.TypeEnum}}, codec.TypeEnum},
	}
	for _, c := range cases {
		if got := acpTypeFromObject(c.o); got != c.want {
			t.Errorf("acpTypeFromObject(%+v) = %d, want %d", c.o, got, c.want)
		}
	}
}

func TestCanonicalTypeFor_AllTypes(t *testing.T) {
	// Every ACP1 type must map to a non-empty canonical type string,
	// including the File branch and the default fallthrough.
	for _, ty := range []codec.ObjectType{
		codec.TypeInteger, codec.TypeLong, codec.TypeByte, codec.TypeFloat,
		codec.TypeIPAddr, codec.TypeString, codec.TypeFile, codec.TypeEnum,
		codec.TypeAlarm, codec.TypeFrame, codec.ObjectType(99),
	} {
		if canonicalTypeFor(ty) == "" {
			t.Errorf("canonicalTypeFor(%d) returned empty", ty)
		}
	}
}

// TestValueAny_KindMismatch covers both the matching and the
// fall-through (default zero-value) arms of valueAny for each ACP1 type.
func TestValueAny_KindMismatch(t *testing.T) {
	// Matching value kinds.
	match := []struct {
		o  consumer.Object
		ty codec.ObjectType
	}{
		{consumer.Object{Value: consumer.Value{Kind: consumer.KindInt, Int: 5}}, codec.TypeInteger},
		{consumer.Object{Value: consumer.Value{Kind: consumer.KindUint, Uint: 5}}, codec.TypeInteger},
		{consumer.Object{Value: consumer.Value{Kind: consumer.KindUint, Uint: 9}}, codec.TypeByte},
		{consumer.Object{Value: consumer.Value{Kind: consumer.KindInt, Int: 9}}, codec.TypeByte},
		{consumer.Object{Value: consumer.Value{Kind: consumer.KindFloat, Float: 1.5}}, codec.TypeFloat},
		{consumer.Object{Value: consumer.Value{Kind: consumer.KindEnum, Enum: 1}}, codec.TypeEnum},
		{consumer.Object{Value: consumer.Value{Kind: consumer.KindIPAddr, IPAddr: [4]byte{1, 2, 3, 4}}}, codec.TypeIPAddr},
		{consumer.Object{Value: consumer.Value{Kind: consumer.KindString, Str: "x"}}, codec.TypeString},
		{consumer.Object{Value: consumer.Value{Kind: consumer.KindBool, Bool: true}}, codec.TypeAlarm},
	}
	for _, c := range match {
		if valueAny(c.o, c.ty) == nil {
			t.Errorf("valueAny match (type %d) returned nil", c.ty)
		}
	}
	// Mismatched kind → defensive zero value (still non-nil for these types).
	mism := []codec.ObjectType{
		codec.TypeInteger, codec.TypeByte, codec.TypeFloat, codec.TypeEnum,
		codec.TypeIPAddr, codec.TypeString, codec.TypeAlarm,
	}
	empty := consumer.Object{} // Value.Kind == KindInvalid
	for _, ty := range mism {
		if valueAny(empty, ty) == nil {
			t.Errorf("valueAny mismatch (type %d) returned nil zero-value", ty)
		}
	}
	// Unsupported type → nil.
	if valueAny(empty, codec.TypeFrame) != nil {
		t.Error("valueAny(Frame) should be nil")
	}
}

func TestParsePath_Errors(t *testing.T) {
	bad := []string{
		"1.2.3",       // wrong component count
		"2.1.2.0",     // first != 1
		"1.x.2.0",     // bad slot
		"1.0.2.0",     // slot 1-based < 1
		"1.1.9.0",     // group > 6
		"1.1.2.x",     // bad id
		"1.1.2.999",   // id > 255
	}
	for _, p := range bad {
		if _, err := parsePath(p); err == nil {
			t.Errorf("parsePath(%q): expected error", p)
		}
	}
	// Valid baseline.
	if k, err := parsePath("1.2.2.0"); err != nil || k.slot != 1 || k.group != codec.GroupControl || k.id != 0 {
		t.Errorf("parsePath(1.2.2.0) = %+v, %v", k, err)
	}
}
