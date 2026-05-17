package emberplus

import (
	"errors"
	"strings"
	"testing"

	"dhs/internal/consumer"
)

// TestCoerceStringToTyped_RejectsInvalidInput pins #445: unparseable
// --value strings must surface as consumer.ValidationError, not be
// silently coerced to the Go zero value of the typed field. Each row
// asserts that the parse failure (a) returns a non-nil error, (b)
// types as *consumer.ValidationError so exitCode() yields 2, and (c)
// the rejected string appears in Reason for operator debuggability.
func TestCoerceStringToTyped_RejectsInvalidInput(t *testing.T) {
	cases := []struct {
		name string
		kind consumer.ValueKind
		str  string
	}{
		{"int with decimal", consumer.KindInt, "-25.5"},
		{"int with letters", consumer.KindInt, "abc"},
		{"int empty-after-sign", consumer.KindInt, "-"},
		{"uint with negative", consumer.KindUint, "-1"},
		{"uint with letters", consumer.KindUint, "xyz"},
		{"float with letters", consumer.KindFloat, "3.14abc"},
		{"float garbage", consumer.KindFloat, "not-a-float"},
		{"enum with letters", consumer.KindEnum, "Low"},
		{"enum overflow uint8", consumer.KindEnum, "999"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			val := &consumer.Value{Kind: tc.kind, Str: tc.str}
			err := coerceStringToTyped(val)
			if err == nil {
				t.Fatalf("kind=%v str=%q: expected error, got nil (val=%+v)", tc.kind, tc.str, val)
			}
			var verr *consumer.ValidationError
			if !errors.As(err, &verr) {
				t.Errorf("kind=%v str=%q: expected *consumer.ValidationError, got %T (%v)", tc.kind, tc.str, err, err)
			}
			if verr != nil && !strings.Contains(verr.Reason, tc.str) {
				t.Errorf("kind=%v: Reason %q should mention rejected input %q", tc.kind, verr.Reason, tc.str)
			}
			if val.Int != 0 || val.Uint != 0 || val.Float != 0 || val.Enum != 0 {
				t.Errorf("kind=%v str=%q: typed fields should be zero on error, got %+v", tc.kind, tc.str, val)
			}
		})
	}
}

// TestCoerceStringToTyped_AcceptsValidInput pins the happy paths so a
// future tightening of the parse rules doesn't accidentally reject
// values that are actually valid for their kind.
func TestCoerceStringToTyped_AcceptsValidInput(t *testing.T) {
	cases := []struct {
		name string
		kind consumer.ValueKind
		str  string
		want any
	}{
		{"int negative", consumer.KindInt, "-2", int64(-2)},
		{"int zero", consumer.KindInt, "0", int64(0)},
		{"int positive", consumer.KindInt, "42", int64(42)},
		{"uint zero", consumer.KindUint, "0", uint64(0)},
		{"uint positive", consumer.KindUint, "65535", uint64(65535)},
		{"float negative", consumer.KindFloat, "-25.5", float64(-25.5)},
		{"float positive", consumer.KindFloat, "3.14", float64(3.14)},
		{"float integer-form", consumer.KindFloat, "100", float64(100)},
		{"enum low", consumer.KindEnum, "0", uint8(0)},
		{"enum high", consumer.KindEnum, "255", uint8(255)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			val := &consumer.Value{Kind: tc.kind, Str: tc.str}
			if err := coerceStringToTyped(val); err != nil {
				t.Fatalf("unexpected error for %q kind=%v: %v", tc.str, tc.kind, err)
			}
			switch want := tc.want.(type) {
			case int64:
				if val.Int != want {
					t.Errorf("Int = %d, want %d", val.Int, want)
				}
			case uint64:
				if val.Uint != want {
					t.Errorf("Uint = %d, want %d", val.Uint, want)
				}
			case float64:
				if val.Float != want {
					t.Errorf("Float = %g, want %g", val.Float, want)
				}
			case uint8:
				if val.Enum != want {
					t.Errorf("Enum = %d, want %d", val.Enum, want)
				}
			}
			if val.Str != "" {
				t.Errorf("Str should be cleared after successful coerce, got %q", val.Str)
			}
		})
	}
}

// TestCoerceStringToTyped_BoolStaysPermissive locks the bool branch's
// deliberately-loose contract documented in coerceStringToTyped's
// doc-comment — operators routinely pass "off"/"no" expecting false,
// not an error.
func TestCoerceStringToTyped_BoolStaysPermissive(t *testing.T) {
	cases := []struct {
		str  string
		want bool
	}{
		{"true", true},
		{"1", true},
		{"yes", true},
		{"false", false},
		{"no", false},
		{"off", false},
		{"anything-else", false},
	}
	for _, tc := range cases {
		val := &consumer.Value{Kind: consumer.KindBool, Str: tc.str}
		if err := coerceStringToTyped(val); err != nil {
			t.Errorf("bool %q: unexpected error %v (bool should be permissive)", tc.str, err)
		}
		if val.Bool != tc.want {
			t.Errorf("bool %q: got %v, want %v", tc.str, val.Bool, tc.want)
		}
	}
}

// TestCoerceStringToTyped_StringAndEmptyPassthrough confirms the
// guard clause at the top of coerceStringToTyped: KindString and
// empty-string inputs short-circuit cleanly (no parse, no error).
func TestCoerceStringToTyped_StringAndEmptyPassthrough(t *testing.T) {
	val1 := &consumer.Value{Kind: consumer.KindString, Str: "hello"}
	if err := coerceStringToTyped(val1); err != nil {
		t.Errorf("KindString passthrough: unexpected error %v", err)
	}
	if val1.Str != "hello" {
		t.Errorf("KindString: Str modified to %q, want preserved %q", val1.Str, "hello")
	}

	val2 := &consumer.Value{Kind: consumer.KindInt, Str: ""}
	if err := coerceStringToTyped(val2); err != nil {
		t.Errorf("empty-string: unexpected error %v", err)
	}
}
