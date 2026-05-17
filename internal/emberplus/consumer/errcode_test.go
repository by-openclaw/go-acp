package emberplus

import (
	"errors"
	"fmt"
	"testing"

	"dhs/internal/errcode"
)

// TestEmberplus_Sentinels_ShapeAndClass pins every emberplus:* code's
// wire shape + exit class.
func TestEmberplus_Sentinels_ShapeAndClass(t *testing.T) {
	cases := []struct {
		code *errcode.Code
		want string
	}{
		{ErrInvocationFailed, "emberplus:invocation-failed"},
		{ErrInvocationFailedWithDescription, "emberplus:invocation-failed-with-description"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			if got := tc.code.Error(); got != tc.want {
				t.Errorf("Error() = %q, want %q", got, tc.want)
			}
			if tc.code.Layer != errcode.LayerEmberplus {
				t.Errorf("Layer = %q, want %q", tc.code.Layer, errcode.LayerEmberplus)
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

// TestInvocationFailed_ErrorsIsDispatch pins that a wrap mirroring the
// shape InvokeFunction emits on Success=false (`%w: invocation %d on %q`)
// chains correctly for callers using errors.Is.
func TestInvocationFailed_ErrorsIsDispatch(t *testing.T) {
	err := fmt.Errorf("%w: invocation %d on %q", ErrInvocationFailed, 42, "router.functions.setLock")
	if !errors.Is(err, ErrInvocationFailed) {
		t.Errorf("err = %v, want errors.Is(err, ErrInvocationFailed)", err)
	}
	if got := errcode.Exit(err); got != 1 {
		t.Errorf("Exit() = %d, want 1", got)
	}
	wantPrefix := "emberplus:invocation-failed:"
	if got := err.Error(); len(got) < len(wantPrefix) || got[:len(wantPrefix)] != wantPrefix {
		t.Errorf("err string = %q, want prefix %q", got, wantPrefix)
	}
}
