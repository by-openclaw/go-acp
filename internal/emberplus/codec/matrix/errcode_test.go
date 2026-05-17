package matrix

import (
	"errors"
	"testing"

	"dhs/internal/emberplus/codec/glow"
	"dhs/internal/errcode"
)

// TestMatrix_Sentinels_ShapeAndClass pins every matrix code's wire
// shape + exit class.
func TestMatrix_Sentinels_ShapeAndClass(t *testing.T) {
	cases := []struct {
		code *errcode.Code
		want string
	}{
		{ErrTargetLocked, "matrix:target-locked"},
		{ErrCardinalityExceeded, "matrix:cardinality-exceeded"},
		{ErrMaxConnectsPerTarget, "matrix:max-connects-per-target"},
		{ErrMaxTotalConnects, "matrix:max-total-connects"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			if got := tc.code.Error(); got != tc.want {
				t.Errorf("Error() = %q, want %q", got, tc.want)
			}
			if tc.code.Layer != errcode.LayerMatrix {
				t.Errorf("Layer = %q, want %q", tc.code.Layer, errcode.LayerMatrix)
			}
			if tc.code.Class != errcode.ClassRuntime {
				t.Errorf("Class = %d, want ClassRuntime (1)", tc.code.Class)
			}
			if got := errcode.Exit(tc.code); got != 1 {
				t.Errorf("Exit() = %d, want 1", got)
			}
		})
	}
}

// TestCanConnect_LockedTarget_TypedError pins ErrTargetLocked.
func TestCanConnect_LockedTarget_TypedError(t *testing.T) {
	s := &State{
		Type: glow.MatrixTypeOneToN,
		Targets: map[int32]*TargetState{
			2: {Sources: []int32{5}, Disposition: glow.ConnDispLocked},
		},
	}
	err := s.CanConnect(2, []int32{7}, glow.ConnOpAbsolute)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrTargetLocked) {
		t.Errorf("err = %v, want errors.Is(err, ErrTargetLocked)", err)
	}
}

// TestCanConnect_OneToNCardinality_TypedError pins ErrCardinalityExceeded
// for oneToN.
func TestCanConnect_OneToNCardinality_TypedError(t *testing.T) {
	s := &State{Type: glow.MatrixTypeOneToN}
	err := s.CanConnect(0, []int32{1, 2}, glow.ConnOpAbsolute)
	if err == nil {
		t.Fatal("expected cardinality error, got nil")
	}
	if !errors.Is(err, ErrCardinalityExceeded) {
		t.Errorf("err = %v, want errors.Is(err, ErrCardinalityExceeded)", err)
	}
}

// TestCanConnect_OneToOneCardinality_TypedError mirrors the above for
// oneToOne.
func TestCanConnect_OneToOneCardinality_TypedError(t *testing.T) {
	s := &State{Type: glow.MatrixTypeOneToOne}
	err := s.CanConnect(0, []int32{1, 2}, glow.ConnOpAbsolute)
	if err == nil {
		t.Fatal("expected cardinality error, got nil")
	}
	if !errors.Is(err, ErrCardinalityExceeded) {
		t.Errorf("err = %v, want errors.Is(err, ErrCardinalityExceeded)", err)
	}
}

// TestCanConnect_NToNMaxPerTarget_TypedError pins ErrMaxConnectsPerTarget.
func TestCanConnect_NToNMaxPerTarget_TypedError(t *testing.T) {
	s := &State{
		Type:                 glow.MatrixTypeNToN,
		MaxConnectsPerTarget: 2,
	}
	err := s.CanConnect(0, []int32{1, 2, 3}, glow.ConnOpAbsolute)
	if err == nil {
		t.Fatal("expected per-target capacity error, got nil")
	}
	if !errors.Is(err, ErrMaxConnectsPerTarget) {
		t.Errorf("err = %v, want errors.Is(err, ErrMaxConnectsPerTarget)", err)
	}
}

// TestCanConnect_NToNMaxTotal_TypedError pins ErrMaxTotalConnects.
func TestCanConnect_NToNMaxTotal_TypedError(t *testing.T) {
	s := &State{
		Type:             glow.MatrixTypeNToN,
		MaxTotalConnects: 3,
		Targets: map[int32]*TargetState{
			0: {Sources: []int32{1, 2}},
			1: {Sources: []int32{3}},
			// 2 will get the request; with current 3 total, adding 1 more = 4 > max 3
		},
	}
	err := s.CanConnect(2, []int32{4}, glow.ConnOpAbsolute)
	if err == nil {
		t.Fatal("expected total-capacity error, got nil")
	}
	if !errors.Is(err, ErrMaxTotalConnects) {
		t.Errorf("err = %v, want errors.Is(err, ErrMaxTotalConnects)", err)
	}
}
