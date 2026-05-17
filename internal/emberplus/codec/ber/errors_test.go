package ber

import (
	"errors"
	"testing"

	"dhs/internal/errcode"
)

// TestBER_Sentinels_ShapeAndClass pins every BER code's wire shape +
// exit class.
func TestBER_Sentinels_ShapeAndClass(t *testing.T) {
	cases := []struct {
		code *errcode.Code
		want string
	}{
		{ErrTruncated, "ber:truncated"},
		{ErrTagTooLong, "ber:tag-too-long"},
		{ErrLengthTooLong, "ber:length-too-long"},
		{ErrInvalidReal, "ber:invalid-real"},
		{ErrOverflow, "ber:integer-overflow"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			if got := tc.code.Error(); got != tc.want {
				t.Errorf("Error() = %q, want %q", got, tc.want)
			}
			if tc.code.Layer != errcode.LayerBER {
				t.Errorf("Layer = %q, want %q", tc.code.Layer, errcode.LayerBER)
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

// TestBER_DecodeTruncated_TypedError pins that DecodeAll on insufficient
// bytes returns errors.Is(err, ErrTruncated).
func TestBER_DecodeTruncated_TypedError(t *testing.T) {
	// Single byte: tag without length or content.
	_, err := DecodeAll([]byte{0x30})
	if err == nil {
		t.Fatal("expected truncated error, got nil")
	}
	if !errors.Is(err, ErrTruncated) {
		t.Errorf("err = %v, want errors.Is(err, ErrTruncated)", err)
	}
}
