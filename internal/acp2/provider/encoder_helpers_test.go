package acp2

import (
	"math"
	"testing"

	"dhs/internal/acp2/codec"
	"dhs/internal/export/canonical"
)

// encoder_helpers_test.go pins the any→typed coercion helpers and the
// numeric/enum property builders that the higher-level encoder tests
// only graze. These are pure functions, so the tests are table-driven
// and assert both the happy conversions and the documented rejects.

func TestAsInt64_Variants(t *testing.T) {
	ok := []struct {
		in   any
		want int64
	}{
		{int(5), 5},
		{int64(-7), -7},
		{int32(3), 3},
		{int16(-2), -2},
		{uint(8), 8},
		{uint32(9), 9},
		{uint16(10), 10},
		{uint8(11), 11},
		{float32(4), 4},
		{float64(-6), -6},
		{"123", 123},
	}
	for _, tc := range ok {
		got, err := asInt64(tc.in, "f")
		if err != nil || got != tc.want {
			t.Errorf("asInt64(%v=%T)=%d,%v want %d", tc.in, tc.in, got, err, tc.want)
		}
	}
	bad := []any{nil, float32(1.5), float64(2.5), "nope", []byte{1}}
	for _, b := range bad {
		if _, err := asInt64(b, "f"); err == nil {
			t.Errorf("asInt64(%v=%T) expected error", b, b)
		}
	}
}

func TestAsUint64_Variants(t *testing.T) {
	if v, err := asUint64(uint64(1<<40), "f"); err != nil || v != 1<<40 {
		t.Errorf("asUint64(big uint64)=%d,%v", v, err)
	}
	if v, err := asUint64(uint(42), "f"); err != nil || v != 42 {
		t.Errorf("asUint64(uint)=%d,%v", v, err)
	}
	if v, err := asUint64(int64(5), "f"); err != nil || v != 5 {
		t.Errorf("asUint64(int64)=%d,%v", v, err)
	}
	if _, err := asUint64(int64(-1), "f"); err == nil {
		t.Error("asUint64(-1) expected error")
	}
	if _, err := asUint64("bad", "f"); err == nil {
		t.Error("asUint64(bad) expected error")
	}
}

func TestAsUint32_RangeGuard(t *testing.T) {
	if v, err := asUint32(uint64(0xFFFFFFFF), "f"); err != nil || v != 0xFFFFFFFF {
		t.Errorf("asUint32(max)=%d,%v", v, err)
	}
	if _, err := asUint32(uint64(0x1_0000_0000), "f"); err == nil {
		t.Error("asUint32 over-range expected error")
	}
	if _, err := asUint32(int64(-1), "f"); err == nil {
		t.Error("asUint32(-1) expected error")
	}
}

func TestAsFloat64_Variants(t *testing.T) {
	ok := []struct {
		in   any
		want float64
	}{
		{float64(1.25), 1.25},
		{float32(2.5), 2.5},
		{int(3), 3},
		{int64(-4), -4},
		{"6.5", 6.5},
		{uint32(7), 7}, // falls through to asInt64 path
	}
	for _, tc := range ok {
		got, err := asFloat64(tc.in, "f")
		if err != nil || math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("asFloat64(%v=%T)=%v,%v want %v", tc.in, tc.in, got, err, tc.want)
		}
	}
	bad := []any{nil, "nope", []byte{1}}
	for _, b := range bad {
		if _, err := asFloat64(b, "f"); err == nil {
			t.Errorf("asFloat64(%v=%T) expected error", b, b)
		}
	}
}

func TestAsString_Variants(t *testing.T) {
	if s, err := asString(nil, "f"); err != nil || s != "" {
		t.Errorf("asString(nil)=%q,%v", s, err)
	}
	if s, err := asString("hi", "f"); err != nil || s != "hi" {
		t.Errorf("asString(hi)=%q,%v", s, err)
	}
	if _, err := asString(123, "f"); err == nil {
		t.Error("asString(int) expected error")
	}
}

func TestIPv4Uint32(t *testing.T) {
	v, err := ipv4Uint32("192.168.0.1")
	if err != nil || v != (192<<24|168<<16|0<<8|1) {
		t.Errorf("ipv4Uint32=%d,%v", v, err)
	}
	bad := []any{123, "1.2.3", "256.0.0.1", "a.b.c.d", "1.2.3.4.5"}
	for _, b := range bad {
		if _, err := ipv4Uint32(b); err == nil {
			t.Errorf("ipv4Uint32(%v) expected error", b)
		}
	}
}

func TestNumericPropOrZero(t *testing.T) {
	// nil → zero body, 4 bytes for 32-bit types.
	p, err := numericPropOrZero(codec.PIDMinValue, codec.NumTypeS32, nil)
	if err != nil || len(p.Data) != 4 {
		t.Fatalf("zero s32 len=%d err=%v", len(p.Data), err)
	}
	// nil → 8-byte body for 64-bit types.
	p, err = numericPropOrZero(codec.PIDMinValue, codec.NumTypeU64, nil)
	if err != nil || len(p.Data) != 8 {
		t.Fatalf("zero u64 len=%d err=%v", len(p.Data), err)
	}
	// non-nil → delegates to encodeNumericProp.
	p, err = numericPropOrZero(codec.PIDMaxValue, codec.NumTypeS32, int64(5))
	if err != nil {
		t.Fatalf("value: %v", err)
	}
	iv, _, _, _ := codec.DecodeNumericValue(codec.NumTypeS32, p.Data)
	if iv != 5 {
		t.Errorf("value iv=%d want 5", iv)
	}
}

func TestEncodeNumericProp_AllTypes(t *testing.T) {
	cases := []struct {
		nt codec.NumberType
		v  any
	}{
		{codec.NumTypeS8, int64(-1)},
		{codec.NumTypeS16, int64(-300)},
		{codec.NumTypeS32, int64(-70000)},
		{codec.NumTypeS64, int64(-5_000_000_000)},
		{codec.NumTypeU8, uint64(200)},
		{codec.NumTypeU16, uint64(40000)},
		{codec.NumTypeU32, uint64(3_000_000_000)},
		{codec.NumTypeU64, uint64(10_000_000_000)},
		{codec.NumTypeFloat, float64(3.14)},
		{codec.NumTypePreset, uint64(7)},
		{codec.NumTypeIPv4, "10.1.2.3"},
	}
	for _, tc := range cases {
		p, err := encodeNumericProp(codec.PIDValue, tc.nt, tc.v)
		if err != nil {
			t.Errorf("encodeNumericProp(%d,%v): %v", tc.nt, tc.v, err)
			continue
		}
		if p.VType != uint8(tc.nt) {
			t.Errorf("nt=%d vtype=%d", tc.nt, p.VType)
		}
	}
	// unsupported number type → error
	if _, err := encodeNumericProp(codec.PIDValue, codec.NumberType(99), 0); err == nil {
		t.Error("encodeNumericProp(bad nt) expected error")
	}
	// wrong value type for each numeric class → coercion error path
	// (asInt64 / asUint64 / asFloat64 failure return inside encodeNumericProp).
	badValueCases := []codec.NumberType{
		codec.NumTypeS32,   // asInt64 fail (signed 32-bit case)
		codec.NumTypeS64,   // asInt64 fail (signed 64-bit case)
		codec.NumTypeU32,   // asUint64 fail (unsigned/preset case)
		codec.NumTypeU64,   // asUint64 fail (u64 case)
		codec.NumTypeFloat, // asFloat64 fail (float case)
	}
	for _, nt := range badValueCases {
		if _, err := encodeNumericProp(codec.PIDValue, nt, "not-num"); err == nil {
			t.Errorf("encodeNumericProp(%d,bad) expected error", nt)
		}
	}
	// invalid dotted-quad for an ipv4 slot → ipv4Uint32 error path.
	if _, err := encodeNumericProp(codec.PIDValue, codec.NumTypeIPv4, "not-an-ip"); err == nil {
		t.Error("encodeNumericProp(ipv4,bad) expected error")
	}
}

func TestEncodeOptionalConstraint(t *testing.T) {
	// nil → emitted=false, no error.
	_, ok, err := encodeOptionalConstraint(codec.PIDMinValue, codec.NumTypeS32, nil)
	if ok || err != nil {
		t.Fatalf("nil constraint ok=%v err=%v", ok, err)
	}
	// present → emitted=true.
	p, ok, err := encodeOptionalConstraint(codec.PIDMaxValue, codec.NumTypeS32, int64(9))
	if !ok || err != nil {
		t.Fatalf("present constraint ok=%v err=%v", ok, err)
	}
	if p.PID != codec.PIDMaxValue {
		t.Errorf("pid=%d", p.PID)
	}
	// bad value → error propagated.
	if _, _, err := encodeOptionalConstraint(codec.PIDMaxValue, codec.NumTypeS32, "bad"); err == nil {
		t.Error("bad constraint expected error")
	}
}

func TestEnumOptions_FromEnumMapAndLegacy(t *testing.T) {
	// EnumMap path: sparse wire ids preserved.
	p := &canonical.Parameter{EnumMap: []canonical.EnumEntry{
		{Key: "Auto", Value: 786}, {Key: "Full", Value: 791}}}
	opts := enumOptions(p)
	if len(opts) != 2 || opts[0].idx != 786 || opts[1].idx != 791 {
		t.Fatalf("enumMap opts=%+v", opts)
	}
	// Legacy comma string → positional 0..N-1.
	comma := "Off,On,Auto"
	opts = enumOptions(&canonical.Parameter{Enumeration: &comma})
	if len(opts) != 3 || opts[2].idx != 2 || opts[2].label != "Auto" {
		t.Fatalf("comma opts=%+v", opts)
	}
	// Legacy newline string.
	nl := "A\nB"
	opts = enumOptions(&canonical.Parameter{Enumeration: &nl})
	if len(opts) != 2 || opts[1].label != "B" {
		t.Fatalf("newline opts=%+v", opts)
	}
	// No enum data → nil.
	if enumOptions(&canonical.Parameter{}) != nil {
		t.Error("empty enumOptions should be nil")
	}
}

// TestStringMaxLen covers the hint → Maximum → 256 fallback chain.
func TestStringMaxLen(t *testing.T) {
	if got := stringMaxLen(nil); got != 256 {
		t.Errorf("nil param=%d want 256", got)
	}
	hint := "maxLen=12"
	if got := stringMaxLen(&canonical.Parameter{Format: &hint}); got != 12 {
		t.Errorf("hint=%d want 12", got)
	}
	if got := stringMaxLen(&canonical.Parameter{Maximum: int64(48)}); got != 48 {
		t.Errorf("maximum=%d want 48", got)
	}
	if got := stringMaxLen(&canonical.Parameter{}); got != 256 {
		t.Errorf("default=%d want 256", got)
	}
}

// TestStringDefaultProp covers the empty-vs-set default branches.
func TestStringDefaultProp(t *testing.T) {
	empty := stringDefaultProp(nil)
	if empty.PID != codec.PIDDefaultValue {
		t.Errorf("empty pid=%d", empty.PID)
	}
	set := stringDefaultProp("hello")
	if set.PID != codec.PIDDefaultValue {
		t.Errorf("set pid=%d", set.PID)
	}
	// non-string default is ignored (stays empty path) without panic.
	_ = stringDefaultProp(123)
}

// TestIPv4DefaultProp covers nil + valid + invalid default handling.
func TestIPv4DefaultProp(t *testing.T) {
	if p := ipv4DefaultProp(nil); len(p.Data) != 4 {
		t.Errorf("nil default len=%d", len(p.Data))
	}
	if p := ipv4DefaultProp("1.2.3.4"); p.Data[0] != 1 || p.Data[3] != 4 {
		t.Errorf("valid default=%v", p.Data)
	}
	// invalid default → zero body, no panic.
	if p := ipv4DefaultProp("garbage"); len(p.Data) != 4 {
		t.Errorf("invalid default len=%d", len(p.Data))
	}
}

// TestEnumDefaultProp covers the Default-present and Value-fallback paths.
func TestEnumDefaultProp(t *testing.T) {
	// Default present.
	e := &entry{param: &canonical.Parameter{Default: int64(5), Value: int64(3)}}
	p, err := enumDefaultProp(e)
	if err != nil {
		t.Fatalf("default present: %v", err)
	}
	if p.PID != codec.PIDDefaultValue {
		t.Errorf("pid=%d", p.PID)
	}
	// Default nil → fall back to Value.
	e = &entry{param: &canonical.Parameter{Value: int64(9)}}
	if _, err := enumDefaultProp(e); err != nil {
		t.Fatalf("value fallback: %v", err)
	}
	// Both unusable → error.
	e = &entry{param: &canonical.Parameter{Value: "not-a-number"}}
	if _, err := enumDefaultProp(e); err == nil {
		t.Error("bad enum default expected error")
	}
}
