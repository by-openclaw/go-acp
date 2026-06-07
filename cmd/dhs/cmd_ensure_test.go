package main

import (
	"errors"
	"testing"

	"dhs/internal/consumer"
)

// TestCanonicalValueStr pins the comparable-string rendering per ValueKind —
// the form `ensure` compares a user-supplied --value against. No unit suffix,
// no formatting flourish (unlike formatValue).
func TestCanonicalValueStr(t *testing.T) {
	cases := []struct {
		name string
		v    consumer.Value
		want string
	}{
		{"enum", consumer.Value{Kind: consumer.KindEnum, Str: "On"}, "On"},
		{"string", consumer.Value{Kind: consumer.KindString, Str: "Broadcasts"}, "Broadcasts"},
		{"bool true", consumer.Value{Kind: consumer.KindBool, Bool: true}, "true"},
		{"bool false", consumer.Value{Kind: consumer.KindBool, Bool: false}, "false"},
		{"int negative", consumer.Value{Kind: consumer.KindInt, Int: -3}, "-3"},
		{"uint", consumer.Value{Kind: consumer.KindUint, Uint: 10}, "10"},
		{"float", consumer.Value{Kind: consumer.KindFloat, Float: 3.5}, "3.5"},
		{"ipaddr", consumer.Value{Kind: consumer.KindIPAddr, IPAddr: [4]byte{192, 168, 1, 5}}, "192.168.1.5"},
		{"raw hex", consumer.Value{Kind: consumer.KindRaw, Raw: []byte{0x01, 0xff}}, "01ff"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := canonicalValueStr(tc.v); got != tc.want {
				t.Errorf("canonicalValueStr = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestValuesEqual pins the idempotency decision: equal means ensure performs no
// write. Includes whitespace trimming and numeric tolerance (10 == 10.0).
func TestValuesEqual(t *testing.T) {
	cases := []struct {
		name    string
		cur     consumer.Value
		desired string
		want    bool
	}{
		{"enum match", consumer.Value{Kind: consumer.KindEnum, Str: "On"}, "On", true},
		{"enum mismatch", consumer.Value{Kind: consumer.KindEnum, Str: "On"}, "Off", false},
		{"enum trims whitespace", consumer.Value{Kind: consumer.KindEnum, Str: "On"}, "  On ", true},
		{"string match", consumer.Value{Kind: consumer.KindString, Str: "hi"}, "hi", true},
		{"int exact", consumer.Value{Kind: consumer.KindInt, Int: 10}, "10", true},
		{"int tolerance 10 vs 10.0", consumer.Value{Kind: consumer.KindInt, Int: 10}, "10.0", true},
		{"int mismatch", consumer.Value{Kind: consumer.KindInt, Int: 10}, "11", false},
		{"float tolerance 3 vs 3.0", consumer.Value{Kind: consumer.KindFloat, Float: 3}, "3.0", true},
		{"float mismatch", consumer.Value{Kind: consumer.KindFloat, Float: 3}, "4", false},
		{"ipaddr match", consumer.Value{Kind: consumer.KindIPAddr, IPAddr: [4]byte{0, 0, 0, 0}}, "0.0.0.0", true},
		{"ipaddr mismatch", consumer.Value{Kind: consumer.KindIPAddr, IPAddr: [4]byte{0, 0, 0, 0}}, "10.0.0.1", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := valuesEqual(tc.cur, tc.desired); got != tc.want {
				t.Errorf("valuesEqual = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestCoerceDesired pins the --value typing that feeds the client-side
// validator: parseable input yields a typed Value of the object's kind (so
// range/type checks see a number, not a bare string); unparseable input is a
// *consumer.ValidationError (exit 2 per error-codes.md), never a runtime error.
// Str is always retained so the SetValue encoder still sees the original text.
func TestCoerceDesired(t *testing.T) {
	t.Run("parseable", func(t *testing.T) {
		cases := []struct {
			name string
			kind consumer.ValueKind
			in   string
			chk  func(consumer.Value) bool
		}{
			{"int", consumer.KindInt, "-3", func(v consumer.Value) bool { return v.Int == -3 }},
			{"uint", consumer.KindUint, "10", func(v consumer.Value) bool { return v.Uint == 10 }},
			{"float", consumer.KindFloat, "3.5", func(v consumer.Value) bool { return v.Float == 3.5 }},
			{"bool", consumer.KindBool, "true", func(v consumer.Value) bool { return v.Bool }},
			{"enum keeps str", consumer.KindEnum, "On", func(v consumer.Value) bool { return v.Str == "On" }},
			{"string keeps str", consumer.KindString, "hi", func(v consumer.Value) bool { return v.Str == "hi" }},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				v, err := coerceDesired(tc.kind, tc.in)
				if err != nil {
					t.Fatalf("coerceDesired(%v, %q) err = %v, want nil", tc.kind, tc.in, err)
				}
				if v.Kind != tc.kind {
					t.Errorf("Kind = %v, want %v", v.Kind, tc.kind)
				}
				if v.Str != tc.in {
					t.Errorf("Str = %q, want %q (original text must be retained)", v.Str, tc.in)
				}
				if !tc.chk(v) {
					t.Errorf("typed field not populated for %v from %q: %+v", tc.kind, tc.in, v)
				}
			})
		}
	})

	t.Run("unparseable is exit-2 validation error", func(t *testing.T) {
		cases := []struct {
			name string
			kind consumer.ValueKind
			in   string
		}{
			{"int", consumer.KindInt, "abc"},
			{"uint", consumer.KindUint, "-1"},
			{"float", consumer.KindFloat, "x"},
			{"bool", consumer.KindBool, "maybe"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				_, err := coerceDesired(tc.kind, tc.in)
				if err == nil {
					t.Fatalf("coerceDesired(%v, %q) err = nil, want validation error", tc.kind, tc.in)
				}
				var verr *consumer.ValidationError
				if !errors.As(err, &verr) {
					t.Errorf("err = %T, want *consumer.ValidationError (exit 2)", err)
				}
			})
		}
	})
}
