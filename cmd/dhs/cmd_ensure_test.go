package main

import (
	"encoding/json"
	"errors"
	"strings"
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

// TestPredictStored pins the client-side clamp that keeps ensure idempotent
// against ACP1's device-side clamping: an out-of-range numeric --value is
// predicted as the [min,max]-clamped value the device will actually store, so
// the second run sees no change. Non-numeric kinds and nil meta pass through.
func TestPredictStored(t *testing.T) {
	uintMeta := &consumer.Object{Kind: consumer.KindUint, Min: uint64(0), Max: uint64(32)}
	intMeta := &consumer.Object{Kind: consumer.KindInt, Min: int64(-50), Max: int64(150)}
	floatMeta := &consumer.Object{Kind: consumer.KindFloat, Min: float64(0), Max: float64(10)}
	cases := []struct {
		name    string
		meta    *consumer.Object
		kind    consumer.ValueKind
		desired string
		want    string
	}{
		{"uint above max clamps", uintMeta, consumer.KindUint, "100", "32"},
		{"uint in range passes", uintMeta, consumer.KindUint, "20", "20"},
		{"uint at max passes", uintMeta, consumer.KindUint, "32", "32"},
		{"int below min clamps", intMeta, consumer.KindInt, "-999", "-50"},
		{"int above max clamps", intMeta, consumer.KindInt, "999", "150"},
		{"int in range passes", intMeta, consumer.KindInt, "0", "0"},
		{"float above max clamps", floatMeta, consumer.KindFloat, "12.5", "10"},
		{"float in range passes", floatMeta, consumer.KindFloat, "3.5", "3.5"},
		{"nil meta passes through", nil, consumer.KindUint, "100", "100"},
		{"enum passes through", uintMeta, consumer.KindEnum, "On", "On"},
		{"whitespace trimmed", uintMeta, consumer.KindUint, "  20 ", "20"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := predictStored(tc.meta, tc.kind, tc.desired); got != tc.want {
				t.Errorf("predictStored = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestResolveEnsureOutput pins the canonical --output flag (ADR-0002) and the
// deprecated --json alias mapping to a single json/text decision, and that yaml
// / unknown formats are exit-2 validation errors.
func TestResolveEnsureOutput(t *testing.T) {
	cases := []struct {
		name      string
		output    string
		jsonAlias bool
		wantJSON  bool
		wantErr   bool
	}{
		{"text default", "text", false, false, false},
		{"text + --json alias", "text", true, true, false},
		{"output json", "json", false, true, false},
		{"output json + alias", "json", true, true, false},
		{"yaml not yet supported", "yaml", false, false, true},
		{"unknown format", "xml", false, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveEnsureOutput(tc.output, tc.jsonAlias)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("resolveEnsureOutput(%q,%v) err = nil, want validation error", tc.output, tc.jsonAlias)
				}
				var verr *consumer.ValidationError
				if !errors.As(err, &verr) {
					t.Errorf("err = %T, want *consumer.ValidationError (exit 2)", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveEnsureOutput(%q,%v) err = %v, want nil", tc.output, tc.jsonAlias, err)
			}
			if got != tc.wantJSON {
				t.Errorf("resolveEnsureOutput(%q,%v) = %v, want %v", tc.output, tc.jsonAlias, got, tc.wantJSON)
			}
		})
	}
}

// TestEnsureValueDiff pins the ADR-0007 diff[] for the value case: one
// {field:"value",from,to} entry when changed, an empty but NON-nil slice when
// not (ADR-0007 §Forbidden: diff must always be emitted, even []).
func TestEnsureValueDiff(t *testing.T) {
	t.Run("changed yields one entry", func(t *testing.T) {
		d := ensureValueDiff(true, "On", "Off")
		if len(d) != 1 || d[0] != (ensureDiff{Field: "value", From: "On", To: "Off"}) {
			t.Fatalf("diff = %+v, want [{value On Off}]", d)
		}
	})
	t.Run("unchanged yields empty non-nil slice", func(t *testing.T) {
		d := ensureValueDiff(false, "On", "On")
		if d == nil {
			t.Fatal("diff is nil; ADR-0007 requires an empty [], never null")
		}
		if len(d) != 0 {
			t.Fatalf("diff = %+v, want []", d)
		}
	})
}

// TestEnsureResultDiffAlwaysEmitted pins that ensureResult always serialises a
// diff array (never omitted, never null), per ADR-0007 §Forbidden.
func TestEnsureResultDiffAlwaysEmitted(t *testing.T) {
	no := false
	b, err := json.Marshal(ensureResult{Changed: &no, Current: "On", Diff: ensureValueDiff(false, "On", "On")})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), `"diff":[]`) {
		t.Errorf("json = %s; want it to contain \"diff\":[] (ADR-0007 always emits diff)", b)
	}
}
