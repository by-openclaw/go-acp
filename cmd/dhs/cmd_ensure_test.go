package main

import (
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
