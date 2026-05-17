package emberplus

import (
	"errors"
	"strings"
	"testing"

	"dhs/internal/consumer"
	"dhs/internal/emberplus/codec/glow"
)

// TestResolveEnumLabel_HappyPath pins the R16 #483 acceptance: a
// label-shaped --value resolves to the integer index via the
// Parameter's enumMap (Ember+ Contents [15]).
func TestResolveEnumLabel_HappyPath(t *testing.T) {
	param := &glow.Parameter{
		EnumMap: map[int64]string{0: "Off", 1: "Low", 2: "Medium", 3: "High"},
	}
	val := &consumer.Value{Kind: consumer.KindEnum, Str: "Low"}
	if err := resolveEnumLabelToIndex(val, param); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val.Str != "1" {
		t.Errorf("Str: got %q, want %q", val.Str, "1")
	}
}

// TestResolveEnumLabel_NumericPassthrough confirms a numeric string
// stays untouched — coerceStringToTyped handles the actual parse.
func TestResolveEnumLabel_NumericPassthrough(t *testing.T) {
	param := &glow.Parameter{
		EnumMap: map[int64]string{0: "Off", 1: "Low"},
	}
	val := &consumer.Value{Kind: consumer.KindEnum, Str: "1"}
	if err := resolveEnumLabelToIndex(val, param); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val.Str != "1" {
		t.Errorf("numeric Str mutated: got %q, want %q", val.Str, "1")
	}
}

// TestResolveEnumLabel_NotInMap pins ErrInvalidEnumLabel: a label not
// present in enumMap fires the typed error and the stderr lists the
// valid labels alphabetised so the operator can pick.
func TestResolveEnumLabel_NotInMap(t *testing.T) {
	param := &glow.Parameter{
		EnumMap: map[int64]string{0: "Off", 1: "Low", 2: "High"},
	}
	val := &consumer.Value{Kind: consumer.KindEnum, Str: "Bogus"}
	err := resolveEnumLabelToIndex(val, param)
	if err == nil {
		t.Fatal("expected ErrInvalidEnumLabel, got nil")
	}
	if !errors.Is(err, consumer.ErrInvalidEnumLabel) {
		t.Errorf("got %v, want ErrInvalidEnumLabel", err)
	}
	if !strings.Contains(err.Error(), "High") || !strings.Contains(err.Error(), "Low") || !strings.Contains(err.Error(), "Off") {
		t.Errorf("error message must list valid labels: %v", err)
	}
}

// TestResolveEnumLabel_NoEnumMap fires ErrEnumNotSupported when the
// Parameter is KindEnum but the wire never carried an enumMap.
func TestResolveEnumLabel_NoEnumMap(t *testing.T) {
	param := &glow.Parameter{}
	val := &consumer.Value{Kind: consumer.KindEnum, Str: "Low"}
	err := resolveEnumLabelToIndex(val, param)
	if !errors.Is(err, consumer.ErrEnumNotSupported) {
		t.Errorf("got %v, want ErrEnumNotSupported", err)
	}
}

// TestResolveEnumLabel_NonEnumKind: non-enum kinds short-circuit.
func TestResolveEnumLabel_NonEnumKind(t *testing.T) {
	param := &glow.Parameter{}
	val := &consumer.Value{Kind: consumer.KindString, Str: "hello"}
	if err := resolveEnumLabelToIndex(val, param); err != nil {
		t.Errorf("non-enum kind must short-circuit: %v", err)
	}
}

// TestConstraints_RangeLow pins ErrOutOfRangeLow.
func TestConstraints_RangeLow(t *testing.T) {
	param := &glow.Parameter{Minimum: int64(-100), Maximum: int64(100)}
	val := &consumer.Value{Kind: consumer.KindInt, Int: -150}
	err := applyParameterConstraints(val, param, consumer.ValueRequest{})
	if !errors.Is(err, consumer.ErrOutOfRangeLow) {
		t.Errorf("got %v, want ErrOutOfRangeLow", err)
	}
}

// TestConstraints_RangeHigh pins ErrOutOfRangeHigh.
func TestConstraints_RangeHigh(t *testing.T) {
	param := &glow.Parameter{Minimum: int64(-100), Maximum: int64(100)}
	val := &consumer.Value{Kind: consumer.KindInt, Int: 250}
	err := applyParameterConstraints(val, param, consumer.ValueRequest{})
	if !errors.Is(err, consumer.ErrOutOfRangeHigh) {
		t.Errorf("got %v, want ErrOutOfRangeHigh", err)
	}
}

// TestConstraints_InRange happy path — value within [min, max] and on
// the step grid passes unchanged.
func TestConstraints_InRange(t *testing.T) {
	param := &glow.Parameter{Minimum: int64(-100), Maximum: int64(100), Step: int64(5)}
	val := &consumer.Value{Kind: consumer.KindInt, Int: 25}
	if err := applyParameterConstraints(val, param, consumer.ValueRequest{}); err != nil {
		t.Errorf("in-range on-step: %v", err)
	}
	if val.Int != 25 {
		t.Errorf("value mutated: %d", val.Int)
	}
}

// TestConstraints_StepMisaligned pins strict-mode rejection: off-step
// without --round fires ErrStepMisaligned and the error message names
// the nearest legal value so the operator can correct.
func TestConstraints_StepMisaligned(t *testing.T) {
	param := &glow.Parameter{Minimum: float64(0), Maximum: float64(10), Step: float64(0.5)}
	val := &consumer.Value{Kind: consumer.KindFloat, Float: 1.3}
	err := applyParameterConstraints(val, param, consumer.ValueRequest{})
	if !errors.Is(err, consumer.ErrStepMisaligned) {
		t.Fatalf("got %v, want ErrStepMisaligned", err)
	}
	if !strings.Contains(err.Error(), "1.5") {
		t.Errorf("error must include nearest legal value (1.5): %v", err)
	}
}

// TestConstraints_StepSnap pins --round behavior: off-step value snaps
// in place to the nearest legal grid point.
func TestConstraints_StepSnap(t *testing.T) {
	param := &glow.Parameter{Minimum: float64(0), Maximum: float64(10), Step: float64(0.5)}
	val := &consumer.Value{Kind: consumer.KindFloat, Float: 1.3}
	err := applyParameterConstraints(val, param, consumer.ValueRequest{Round: true})
	if err != nil {
		t.Fatalf("--round: unexpected error %v", err)
	}
	if val.Float != 1.5 {
		t.Errorf("snap: got %v, want 1.5", val.Float)
	}
}

// TestConstraints_RoundNotApplicable pins ErrRoundNotApplicable: passing
// --round on a non-numeric Parameter is a user error.
func TestConstraints_RoundNotApplicable(t *testing.T) {
	param := &glow.Parameter{}
	val := &consumer.Value{Kind: consumer.KindString, Str: "hello"}
	err := applyParameterConstraints(val, param, consumer.ValueRequest{Round: true})
	if !errors.Is(err, consumer.ErrRoundNotApplicable) {
		t.Errorf("got %v, want ErrRoundNotApplicable", err)
	}
}

// TestConstraints_NoMinMaxNoStep — Parameter without any constraint
// fields applied: every numeric value passes through unchanged.
func TestConstraints_NoConstraints(t *testing.T) {
	param := &glow.Parameter{}
	val := &consumer.Value{Kind: consumer.KindInt, Int: 9999}
	if err := applyParameterConstraints(val, param, consumer.ValueRequest{}); err != nil {
		t.Errorf("unconstrained value: %v", err)
	}
}

// TestConstraints_StepAnchorAtMinimum — step grid anchors at the
// minimum (matches libember-cpp + Lawo VSM convention). With min=2
// step=3, legal values are 2, 5, 8, 11... 4 snaps to 5; 7 snaps to 8.
func TestConstraints_StepAnchorAtMinimum(t *testing.T) {
	param := &glow.Parameter{Minimum: int64(2), Maximum: int64(20), Step: int64(3)}
	val := &consumer.Value{Kind: consumer.KindInt, Int: 4}
	err := applyParameterConstraints(val, param, consumer.ValueRequest{Round: true})
	if err != nil {
		t.Fatalf("snap: %v", err)
	}
	if val.Int != 5 {
		t.Errorf("anchored snap: got %d, want 5 (min=2 step=3 → grid 2,5,8,...)", val.Int)
	}
}
