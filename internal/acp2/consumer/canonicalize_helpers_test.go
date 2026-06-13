package acp2

import (
	"testing"

	"dhs/internal/acp2/codec"
	"dhs/internal/consumer"
	"dhs/internal/export/canonical"
)

// TestACP2KindToCanonicalType covers every Kind branch plus the
// objType/numType fallback for KindUnknown leaves.
func TestACP2KindToCanonicalType(t *testing.T) {
	cases := []struct {
		k       consumer.ValueKind
		objType codec.ACP2ObjType
		numType codec.NumberType
		want    string
	}{
		{consumer.KindBool, 0, 0, canonical.ParamBoolean},
		{consumer.KindInt, 0, 0, canonical.ParamInteger},
		{consumer.KindUint, 0, 0, canonical.ParamInteger},
		{consumer.KindFloat, 0, 0, canonical.ParamReal},
		{consumer.KindEnum, 0, 0, canonical.ParamEnum},
		{consumer.KindString, 0, 0, canonical.ParamString},
		{consumer.KindIPAddr, 0, 0, canonical.ParamString},
		{consumer.KindRaw, 0, 0, canonical.ParamOctets},
		// KindUnknown → objType/numType disambiguation
		{consumer.KindUnknown, codec.ObjTypeString, 0, canonical.ParamString},
		{consumer.KindUnknown, codec.ObjTypeIPv4, 0, canonical.ParamString},
		{consumer.KindUnknown, codec.ObjTypeNumber, codec.NumTypeFloat, canonical.ParamReal},
		{consumer.KindUnknown, codec.ObjTypeNumber, codec.NumTypeU32, canonical.ParamInteger},
		{consumer.KindUnknown, codec.ObjTypeEnum, 0, canonical.ParamEnum},
		{consumer.KindUnknown, codec.ObjTypePreset, 0, canonical.ParamEnum},
		{consumer.KindUnknown, codec.ObjTypeNode, 0, canonical.ParamString}, // final fallback
	}
	for _, c := range cases {
		if got := acp2KindToCanonicalType(c.k, c.objType, c.numType); got != c.want {
			t.Errorf("acp2KindToCanonicalType(%v,%v,%v) = %q, want %q",
				c.k, c.objType, c.numType, got, c.want)
		}
	}
}

// TestACP2ValueToAny covers every value-kind scalar branch including the
// enum label-vs-index split and the nil fallthrough.
func TestACP2ValueToAny(t *testing.T) {
	if got := acp2ValueToAny(consumer.Value{Kind: consumer.KindBool, Bool: true}); got != true {
		t.Errorf("bool = %v", got)
	}
	if got := acp2ValueToAny(consumer.Value{Kind: consumer.KindInt, Int: -7}); got != int64(-7) {
		t.Errorf("int = %v", got)
	}
	if got := acp2ValueToAny(consumer.Value{Kind: consumer.KindUint, Uint: 9}); got != uint64(9) {
		t.Errorf("uint = %v", got)
	}
	if got := acp2ValueToAny(consumer.Value{Kind: consumer.KindFloat, Float: 1.5}); got != 1.5 {
		t.Errorf("float = %v", got)
	}
	if got := acp2ValueToAny(consumer.Value{Kind: consumer.KindEnum, Str: "On"}); got != "On" {
		t.Errorf("enum label = %v", got)
	}
	if got := acp2ValueToAny(consumer.Value{Kind: consumer.KindEnum, Enum: 4}); got != int64(4) {
		t.Errorf("enum idx = %v", got)
	}
	if got := acp2ValueToAny(consumer.Value{Kind: consumer.KindString, Str: "SDI"}); got != "SDI" {
		t.Errorf("string = %v", got)
	}
	if got := acp2ValueToAny(consumer.Value{Kind: consumer.KindIPAddr, IPAddr: [4]byte{10, 0, 0, 1}}); got != "10.0.0.1" {
		t.Errorf("ipaddr = %v", got)
	}
	if got := acp2ValueToAny(consumer.Value{Kind: consumer.KindUnknown}); got != nil {
		t.Errorf("unknown = %v, want nil", got)
	}
}

// TestACP2Identifier covers the labelled and unlabelled-fallback branches.
func TestACP2Identifier(t *testing.T) {
	if got := acp2Identifier(consumer.Object{Label: "Gain"}); got != "Gain" {
		t.Errorf("labelled = %q", got)
	}
	if got := acp2Identifier(consumer.Object{ID: 42}); got != "#42" {
		t.Errorf("unlabelled = %q, want #42", got)
	}
}

// TestSortACP2ByNumber covers the insertion-sort reorder + already-sorted
// no-swap path.
func TestSortACP2ByNumber(t *testing.T) {
	els := []canonical.Element{
		&canonical.Parameter{Header: canonical.Header{Number: 3}},
		&canonical.Parameter{Header: canonical.Header{Number: 1}},
		&canonical.Parameter{Header: canonical.Header{Number: 2}},
	}
	sortACP2ByNumber(els)
	for i, want := range []int{1, 2, 3} {
		if got := els[i].Common().Number; got != want {
			t.Errorf("els[%d].Number = %d, want %d", i, got, want)
		}
	}
	// Idempotent on already-sorted input (exercises the break path).
	sortACP2ByNumber(els)
	if els[0].Common().Number != 1 {
		t.Errorf("re-sort disturbed order: %d", els[0].Common().Number)
	}
}
