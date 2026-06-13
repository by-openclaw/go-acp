package acp1

import (
	"math"
	"testing"

	"dhs/internal/acp1/codec"
	"dhs/internal/export/canonical"
)

// TestEncodeObject_NilEntry covers the nil-entry / nil-param guard.
func TestEncodeObject_NilEntry(t *testing.T) {
	if _, err := encodeObject(nil); err == nil {
		t.Error("nil entry: want error")
	}
	if _, err := encodeObject(&entry{}); err == nil {
		t.Error("nil param: want error")
	}
}

// TestEncodeEnum_TooManyItemsAndBadDefault covers the >255-items guard and
// the bad-default error arm of encodeEnum (via encodeObject).
func TestEncodeEnum_TooManyItems(t *testing.T) {
	items := make([]canonical.EnumEntry, 300)
	for i := range items {
		items[i] = canonical.EnumEntry{Key: "x", Value: int64(i)}
	}
	e := &entry{acpType: codec.TypeEnum, param: &canonical.Parameter{
		Header:  canonical.Header{Identifier: "Big"},
		Value:   int64(0),
		EnumMap: items,
	}}
	if _, err := encodeObject(e); err == nil {
		t.Fatal(">255 enum items: want error")
	}
}

func TestEncodeEnum_BadDefault(t *testing.T) {
	e := &entry{acpType: codec.TypeEnum, param: &canonical.Parameter{
		Header:  canonical.Header{Identifier: "E"},
		Value:   int64(0),
		EnumMap: []canonical.EnumEntry{{Key: "Off", Value: 0}, {Key: "On", Value: 1}},
		Default: "not-a-number", // asUint8Opt fails
	}}
	if _, err := encodeObject(e); err == nil {
		t.Fatal("bad enum default: want error")
	}
}

// TestEnumItems_NewlineSplit covers the "\n"-delimited Enumeration arm.
func TestEnumItems_NewlineSplit(t *testing.T) {
	enum := "Off\nOn\nAuto"
	got := enumItems(&canonical.Parameter{Enumeration: &enum})
	if len(got) != 3 || got[2] != "Auto" {
		t.Fatalf("newline split = %v", got)
	}
}

// TestAlarmHelpers_NilFields cover the nil-Format and nil-Description arms.
func TestAlarmHelpers_NilFields(t *testing.T) {
	pr, tag := alarmPriorityTag(&canonical.Parameter{})
	if pr != 1 || tag != 0 {
		t.Errorf("default alarm priority/tag = %d/%d, want 1/0", pr, tag)
	}
	on, off := alarmMessages(&canonical.Parameter{})
	if on != "" || off != "" {
		t.Errorf("nil description messages = %q/%q", on, off)
	}
}

// TestFrameSlotStatuses_Errors covers the per-element conversion error and
// the out-of-range guard.
func TestFrameSlotStatuses_Errors(t *testing.T) {
	// Non-numeric element → anyToInt error.
	if _, err := frameSlotStatuses(&canonical.Parameter{Value: []any{"x"}}); err == nil {
		t.Error("non-numeric slot status: want error")
	}
	// Out-of-range element.
	if _, err := frameSlotStatuses(&canonical.Parameter{Value: []any{int64(999)}}); err == nil {
		t.Error("out-of-range slot status: want error")
	}
}

// TestAsConverters_EdgeCases covers asFloat32 range, asString nil, asBool
// nil + wrong-type, and ipv4ToUint32 nil + wrong-type.
func TestAsConverters_EdgeCases(t *testing.T) {
	if _, err := asFloat32(math.MaxFloat64, "v"); err == nil {
		t.Error("asFloat32 out of range: want error")
	}
	if s, err := asString(nil, "v"); err != nil || s != "" {
		t.Errorf("asString(nil) = %q,%v", s, err)
	}
	if b, err := asBool(nil, "v"); err != nil || b {
		t.Errorf("asBool(nil) = %v,%v", b, err)
	}
	if _, err := asBool(123, "v"); err == nil {
		t.Error("asBool wrong type: want error")
	}
	if _, err := ipv4ToUint32(nil); err == nil {
		t.Error("ipv4ToUint32(nil): want error")
	}
	if _, err := ipv4ToUint32(123); err == nil {
		t.Error("ipv4ToUint32 wrong type: want error")
	}
}
