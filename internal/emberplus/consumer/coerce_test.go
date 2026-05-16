package emberplus

import (
	"errors"
	"testing"

	"dhs/internal/protocol"
)

// TestCoerceStringToTyped_RejectsInvalidInput pins #445: unparseable
// --value strings must surface as protocol.ValidationError, not be
// silently coerced to the Go zero value of the typed field. Each row
// asserts that the parse failure (a) returns a non-nil error, (b)
// types as *protocol.ValidationError so exitCode() yields 2, and (c)
// the rejected string appears in Reason for operator debuggability.
func TestCoerceStringToTyped_RejectsInvalidInput(t *testing.T) {
	cases := []struct {
		name string
		kind protocol.ValueKind
		str  string
	}{
		{"int with decimal", protocol.KindInt, "-25.5"},
		{"int with letters", protocol.KindInt, "abc"},
		{"int empty-after-sign", protocol.KindInt, "-"},
		{"uint with negative", protocol.KindUint, "-1"},
		{"uint with letters", protocol.KindUint, "xyz"},
		{"float with letters", protocol.KindFloat, "3.14abc"},
		{"float garbage", protocol.KindFloat, "not-a-float"},
		{"enum with letters", protocol.KindEnum, "Low"},
		{"enum overflow uint8", protocol.KindEnum, "999"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			val := &protocol.Value{Kind: tc.kind, Str: tc.str}
			err := coerceStringToTyped(val)
			if err == nil {
				t.Fatalf("kind=%v str=%q: expected error, got nil (val=%+v)", tc.kind, tc.str, val)
			}
			var verr *protocol.ValidationError
			if !errors.As(err, &verr) {
				t.Errorf("kind=%v str=%q: expected *protocol.ValidationError, got %T (%v)", tc.kind, tc.str, err, err)
			}
			if verr != nil && !contains(verr.Reason, tc.str) {
				t.Errorf("kind=%v: Reason %q should mention rejected input %q", tc.kind, verr.Reason, tc.str)
			}
			// Typed field MUST stay at zero — no silent coerce.
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
		kind protocol.ValueKind
		str  string
		want any
	}{
		{"int negative", protocol.KindInt, "-2", int64(-2)},
		{"int zero", protocol.KindInt, "0", int64(0)},
		{"int positive", protocol.KindInt, "42", int64(42)},
		{"uint zero", protocol.KindUint, "0", uint64(0)},
		{"uint positive", protocol.KindUint, "65535", uint64(65535)},
		{"float negative", protocol.KindFloat, "-25.5", float64(-25.5)},
		{"float positive", protocol.KindFloat, "3.14", float64(3.14)},
		{"float integer-form", protocol.KindFloat, "100", float64(100)},
		{"enum low", protocol.KindEnum, "0", uint8(0)},
		{"enum high", protocol.KindEnum, "255", uint8(255)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			val := &protocol.Value{Kind: tc.kind, Str: tc.str}
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
		val := &protocol.Value{Kind: protocol.KindBool, Str: tc.str}
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
	val1 := &protocol.Value{Kind: protocol.KindString, Str: "hello"}
	if err := coerceStringToTyped(val1); err != nil {
		t.Errorf("KindString passthrough: unexpected error %v", err)
	}
	if val1.Str != "hello" {
		t.Errorf("KindString: Str modified to %q, want preserved %q", val1.Str, "hello")
	}

	val2 := &protocol.Value{Kind: protocol.KindInt, Str: ""}
	if err := coerceStringToTyped(val2); err != nil {
		t.Errorf("empty-string: unexpected error %v", err)
	}
}

// contains is a tiny local helper to keep the assertion line readable
// without importing strings just for this. Equivalent to
// strings.Contains.
func contains(haystack, needle string) bool {
	if needle == "" {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
