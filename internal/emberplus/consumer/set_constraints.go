package emberplus

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"dhs/internal/consumer"
	"dhs/internal/emberplus/codec/glow"
)

// resolveEnumLabelToIndex turns a label-shaped --value (`"Low"`) into the
// integer index it represents in the Parameter's enumMap. Called before
// coerceStringToTyped so the downstream parser sees a numeric string.
//
// Behavior:
//   - val.Kind != KindEnum                          → no-op
//   - val.Str empty / already numeric               → no-op
//   - enumMap empty                                 → ErrEnumNotSupported
//   - val.Str matches an enumMap label              → val.Str = "<int>"
//   - val.Str does not match any label              → ErrInvalidEnumLabel
//     (stderr lists valid labels alphabetised)
//
// Refs R16 #483.
func resolveEnumLabelToIndex(val *consumer.Value, param *glow.Parameter) error {
	if val.Kind != consumer.KindEnum {
		return nil
	}
	if val.Str == "" {
		return nil
	}
	if _, err := strconv.ParseUint(val.Str, 10, 64); err == nil {
		// Numeric already; existing pipeline handles it.
		return nil
	}
	if param == nil || len(param.EnumMap) == 0 {
		return fmt.Errorf("%w: --value %q is a label but Parameter has no enumMap",
			consumer.ErrEnumNotSupported, val.Str)
	}
	for idx, lbl := range param.EnumMap {
		if lbl == val.Str {
			val.Str = strconv.FormatInt(idx, 10)
			return nil
		}
	}
	labels := make([]string, 0, len(param.EnumMap))
	for _, lbl := range param.EnumMap {
		labels = append(labels, lbl)
	}
	sort.Strings(labels)
	return fmt.Errorf("%w: --value %q not in enumMap (valid labels: %s)",
		consumer.ErrInvalidEnumLabel, val.Str, strings.Join(labels, ", "))
}

// applyParameterConstraints checks a coerced typed Value against the
// Parameter's declared minimum / maximum / step (Ember+ Contents [3]
// [4] [11]). Numeric Parameters only — KindString / KindBool / KindEnum
// flow straight through (enum range is implicit in the enumMap lookup;
// out-of-range integer enums are a separate concern: provider-side).
//
// When req.Round is true and the value is off-step, the function snaps
// to the nearest legal step in place and returns nil. When req.Round is
// false, off-step returns ErrStepMisaligned.
//
// req.Round on a non-numeric Parameter returns ErrRoundNotApplicable.
//
// Refs R16 #483.
func applyParameterConstraints(val *consumer.Value, param *glow.Parameter, req consumer.ValueRequest) error {
	if param == nil {
		return nil
	}
	switch val.Kind {
	case consumer.KindInt, consumer.KindUint, consumer.KindFloat:
		// Numeric — apply min / max / step below.
	default:
		if req.Round {
			return fmt.Errorf("%w: --round is only meaningful for numeric Parameters (this one is %s)",
				consumer.ErrRoundNotApplicable, val.Kind)
		}
		return nil
	}

	want := asFloat(val)
	if min, ok := asNumber(param.Minimum); ok && want < min {
		return fmt.Errorf("%w: --value %v below minimum %v",
			consumer.ErrOutOfRangeLow, want, min)
	}
	if max, ok := asNumber(param.Maximum); ok && want > max {
		return fmt.Errorf("%w: --value %v above maximum %v",
			consumer.ErrOutOfRangeHigh, want, max)
	}
	step, hasStep := asNumber(param.Step)
	if !hasStep || step <= 0 {
		return nil
	}
	// Anchor the step grid at minimum when present (matches Lawo VSM
	// + libember-cpp behavior); fall back to 0 otherwise.
	anchor := 0.0
	if min, ok := asNumber(param.Minimum); ok {
		anchor = min
	}
	offset := want - anchor
	steps := math.Round(offset / step)
	snapped := anchor + steps*step
	if math.Abs(snapped-want) < step*1e-9 {
		// Already on-grid (within float epsilon).
		return nil
	}
	if !req.Round {
		return fmt.Errorf("%w: --value %v not aligned to step %v (nearest legal value: %v). Pass --round to snap",
			consumer.ErrStepMisaligned, want, step, snapped)
	}
	writeFloat(val, snapped)
	return nil
}

// asNumber returns the float64 view of a Parameter.{Minimum,Maximum,
// Step} field. The Glow decoder leaves these as `any` (int64 or
// float64); both are flattened to float64 for comparison. nil / other
// types → not present.
func asNumber(v any) (float64, bool) {
	switch x := v.(type) {
	case nil:
		return 0, false
	case int64:
		return float64(x), true
	case int32:
		return float64(x), true
	case int:
		return float64(x), true
	case float64:
		return x, true
	case float32:
		return float64(x), true
	}
	return 0, false
}

// asFloat returns the Value's numeric body as float64. Caller is
// responsible for confirming Kind is numeric.
func asFloat(val *consumer.Value) float64 {
	switch val.Kind {
	case consumer.KindInt:
		return float64(val.Int)
	case consumer.KindUint:
		return float64(val.Uint)
	case consumer.KindFloat:
		return val.Float
	}
	return 0
}

// writeFloat back-projects a snapped float64 into the right typed body
// for the Value's Kind. Used by --round to commit the snap result.
func writeFloat(val *consumer.Value, f float64) {
	switch val.Kind {
	case consumer.KindInt:
		val.Int = int64(math.Round(f))
	case consumer.KindUint:
		if f < 0 {
			f = 0
		}
		val.Uint = uint64(math.Round(f))
	case consumer.KindFloat:
		val.Float = f
	}
}
