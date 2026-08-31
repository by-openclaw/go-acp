package ms05

// Constraint VALUE checking — the enforcement half of the MS-05-02
// constraints model (Constraints.html). Declaring a constraint in a
// descriptor without rejecting violating writes would publish a
// contract the device does not honour; the AMWA suite attacks exactly
// that (IS-14-01 test_27 offers out-of-range values through
// bulkProperties and expects error notices).
//
// The hierarchy (runtime overrides property overrides datatype) is
// resolved by the CALLER — this file only answers "does this value
// satisfy this one constraint instance".

import (
	"fmt"
	"math"
	"regexp"
)

// numericStep tolerance: values arrive as JSON float64; a step check
// on 0.5 must not reject 5.5 over representation error.
const stepEpsilon = 1e-9

// CheckConstraintValue validates one decoded JSON value against one
// constraint instance — any of the four concrete variants (property /
// parameter × number / string), by value or pointer. A nil value is
// accepted (nullability is a separate descriptor rule), as is a nil
// or base-only constraint (a bare default value constrains nothing).
func CheckConstraintValue(v any, c any) error {
	if v == nil || c == nil {
		return nil
	}
	switch t := c.(type) {
	case *NcParameterConstraintsNumber:
		return checkNumber(v, t.Minimum, t.Maximum, t.Step)
	case NcParameterConstraintsNumber:
		return checkNumber(v, t.Minimum, t.Maximum, t.Step)
	case *NcPropertyConstraintsNumber:
		return checkNumber(v, t.Minimum, t.Maximum, t.Step)
	case NcPropertyConstraintsNumber:
		return checkNumber(v, t.Minimum, t.Maximum, t.Step)
	case *NcParameterConstraintsString:
		return checkString(v, t.MaxCharacters, t.Pattern)
	case NcParameterConstraintsString:
		return checkString(v, t.MaxCharacters, t.Pattern)
	case *NcPropertyConstraintsString:
		return checkString(v, t.MaxCharacters, t.Pattern)
	case NcPropertyConstraintsString:
		return checkString(v, t.MaxCharacters, t.Pattern)
	}
	return nil
}

// asFloat coerces the polymorphic min/max/step members (authored as
// int or float literals) to float64; ok=false when absent (null).
func asFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	}
	return 0, false
}

func checkNumber(v, min, max, step any) error {
	n, ok := asFloat(v)
	if !ok {
		return fmt.Errorf("value is not numeric")
	}
	if lo, ok := asFloat(min); ok && n < lo {
		return fmt.Errorf("value %v is below the constraint minimum %v", n, lo)
	}
	if hi, ok := asFloat(max); ok && n > hi {
		return fmt.Errorf("value %v is above the constraint maximum %v", n, hi)
	}
	if st, ok := asFloat(step); ok && st > 0 {
		// Steps count from the minimum when one is declared, from
		// zero otherwise (MS-05-02 Constraints.html).
		base := 0.0
		if lo, ok := asFloat(min); ok {
			base = lo
		}
		ratio := (n - base) / st
		if math.Abs(ratio-math.Round(ratio)) > stepEpsilon {
			return fmt.Errorf("value %v does not align to the constraint step %v", n, st)
		}
	}
	return nil
}

func checkString(v any, maxChars *uint32, pattern *string) error {
	s, ok := v.(string)
	if !ok {
		return fmt.Errorf("value is not a string")
	}
	if maxChars != nil && uint32(len([]rune(s))) > *maxChars {
		return fmt.Errorf("value is %d characters, above the constraint maximum %d", len([]rune(s)), *maxChars)
	}
	if pattern != nil && *pattern != "" {
		re, err := regexp.Compile(*pattern)
		if err != nil {
			return fmt.Errorf("constraint pattern %q does not compile: %v", *pattern, err)
		}
		if !re.MatchString(s) {
			return fmt.Errorf("value %q does not match the constraint pattern %q", s, *pattern)
		}
	}
	return nil
}

// ConstraintPropertyID extracts the propertyId member of a
// property-level constraint instance — how callers match an entry of
// runtimePropertyConstraints to the property being written.
func ConstraintPropertyID(c any) (NcPropertyId, bool) {
	switch t := c.(type) {
	case *NcPropertyConstraintsNumber:
		return t.PropertyId, true
	case NcPropertyConstraintsNumber:
		return t.PropertyId, true
	case *NcPropertyConstraintsString:
		return t.PropertyId, true
	case NcPropertyConstraintsString:
		return t.PropertyId, true
	case *NcPropertyConstraints:
		return t.PropertyId, true
	case NcPropertyConstraints:
		return t.PropertyId, true
	}
	return NcPropertyId{}, false
}
