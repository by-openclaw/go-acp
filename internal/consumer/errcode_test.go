package consumer

import (
	"errors"
	"testing"

	"dhs/internal/errcode"
)

// TestConsumerSentinels_ShapeAndClass pins every R1g-migrated sentinel's
// wire shape, layer, class, and CLI exit value. These are the existing
// internal/consumer/ ErrXxx variables now rewired through errcode.
func TestConsumerSentinels_ShapeAndClass(t *testing.T) {
	cases := []struct {
		code      *errcode.Code
		wantStr   string
		wantLayer errcode.Layer
		wantClass errcode.Class
		wantExit  int
	}{
		{ErrNotImplemented, "plugin:not-implemented", errcode.LayerPlugin, errcode.ClassUsage, 2},
		{ErrNotConnected, "plugin:not-connected", errcode.LayerPlugin, errcode.ClassUsage, 2},
		{ErrUnknownLabel, "plugin:unknown-label", errcode.LayerPlugin, errcode.ClassUsage, 2},
		{ErrObjectNotFound, "plugin:object-not-found", errcode.LayerPlugin, errcode.ClassUsage, 2},
		{ErrIdentityUnresolved, "plugin:identity-unresolved", errcode.LayerPlugin, errcode.ClassUsage, 2},
		{ErrValidationFailed, "validation:failed", errcode.LayerValidation, errcode.ClassUsage, 2},
		{ErrWriteTimeout, "session:write-timeout", errcode.LayerSession, errcode.ClassRuntime, 1},
		{ErrWriteCoerced, "session:write-coerced", errcode.LayerSession, errcode.ClassRuntime, 1},
		{ErrWriteRejected, "session:write-rejected", errcode.LayerSession, errcode.ClassRuntime, 1},
	}
	for _, tc := range cases {
		t.Run(tc.wantStr, func(t *testing.T) {
			if got := tc.code.Error(); got != tc.wantStr {
				t.Errorf("Error() = %q, want %q", got, tc.wantStr)
			}
			if tc.code.Layer != tc.wantLayer {
				t.Errorf("Layer = %q, want %q", tc.code.Layer, tc.wantLayer)
			}
			if tc.code.Class != tc.wantClass {
				t.Errorf("Class = %d, want %d", tc.code.Class, tc.wantClass)
			}
			if got := errcode.Exit(tc.code); got != tc.wantExit {
				t.Errorf("Exit() = %d, want %d", got, tc.wantExit)
			}
		})
	}
}

// TestConsumerSentinels_ErrorsIs pins the chain dispatch that callers rely
// on — a wrapped error must surface the underlying sentinel via errors.Is.
func TestConsumerSentinels_ErrorsIs(t *testing.T) {
	wrapped := errors.Join(ErrObjectNotFound, errors.New("router.bogus.path"))
	if !errors.Is(wrapped, ErrObjectNotFound) {
		t.Errorf("errors.Is wrapped did not find ErrObjectNotFound")
	}
	if errors.Is(wrapped, ErrNotConnected) {
		t.Errorf("errors.Is wrapped should NOT match a different sentinel")
	}
}

// TestValidationError_StringShape pins that the legacy ValidationError
// struct still stringifies as "validation: <field>: <reason>" — the
// existing contract used across codec packages stays intact.
func TestValidationError_StringShape(t *testing.T) {
	ve := &ValidationError{Field: "value", Reason: "invalid integer \"abc\""}
	want := "validation: value: invalid integer \"abc\""
	if got := ve.Error(); got != want {
		t.Errorf("ValidationError.Error() = %q, want %q", got, want)
	}
}
