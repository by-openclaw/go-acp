package errcode_test

import (
	"errors"
	"fmt"
	"testing"

	"dhs/internal/errcode"
)

// Sentinel codes used by the tests — mirror the per-layer pattern callers
// will adopt in R1b.
var (
	errTestRefused      = errcode.New(errcode.LayerTransport, "refused", errcode.ClassRuntime)
	errTestTimeout      = errcode.New(errcode.LayerTransport, "timeout", errcode.ClassRuntime)
	errTestInvalidInt   = errcode.New(errcode.LayerValidation, "invalid-integer", errcode.ClassUsage)
	errTestNotFound     = errcode.New(errcode.LayerPlugin, "object-not-found", errcode.ClassUsage)
	errTestMatrixLocked = errcode.New(errcode.LayerMatrix, "target-locked", errcode.ClassRuntime)
)

func TestCode_ErrorString(t *testing.T) {
	cases := []struct {
		code *errcode.Code
		want string
	}{
		{errTestRefused, "transport:refused"},
		{errTestTimeout, "transport:timeout"},
		{errTestInvalidInt, "validation:invalid-integer"},
		{errTestNotFound, "plugin:object-not-found"},
		{errTestMatrixLocked, "matrix:target-locked"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			if got := tc.code.Error(); got != tc.want {
				t.Errorf("Error() = %q, want %q", got, tc.want)
			}
			if got := tc.code.String(); got != tc.want {
				t.Errorf("String() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCode_WrapAndIs(t *testing.T) {
	wrapped := fmt.Errorf("%w: connect 127.0.0.1:9100: connection refused", errTestRefused)

	if !errors.Is(wrapped, errTestRefused) {
		t.Errorf("errors.Is on wrapped %v did not find errTestRefused", wrapped)
	}
	if errors.Is(wrapped, errTestTimeout) {
		t.Errorf("errors.Is on wrapped should NOT match a different code")
	}
	wantPrefix := "transport:refused:"
	if got := wrapped.Error(); got[:len(wantPrefix)] != wantPrefix {
		t.Errorf("wrapped error = %q, want prefix %q", got, wantPrefix)
	}
}

func TestFrom_FindsTypedCodeInChain(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want *errcode.Code
	}{
		{name: "nil", err: nil, want: nil},
		{name: "plain error has no code", err: errors.New("plain"), want: nil},
		{name: "raw code", err: errTestInvalidInt, want: errTestInvalidInt},
		{name: "single wrap", err: fmt.Errorf("%w: x", errTestInvalidInt), want: errTestInvalidInt},
		{name: "double wrap", err: fmt.Errorf("outer: %w", fmt.Errorf("%w: x", errTestMatrixLocked)), want: errTestMatrixLocked},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := errcode.From(tc.err)
			if got != tc.want {
				t.Errorf("From(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestExit_MapsClassToOSExitCode(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{name: "nil err → 0", err: nil, want: 0},
		{name: "untyped err → 1 (runtime fallback)", err: errors.New("untyped"), want: 1},
		{name: "runtime code → 1", err: errTestRefused, want: 1},
		{name: "matrix runtime code → 1", err: errTestMatrixLocked, want: 1},
		{name: "validation usage code → 2", err: errTestInvalidInt, want: 2},
		{name: "plugin usage code → 2", err: errTestNotFound, want: 2},
		{name: "wrapped runtime → 1", err: fmt.Errorf("%w: detail", errTestTimeout), want: 1},
		{name: "wrapped usage → 2", err: fmt.Errorf("%w: detail", errTestInvalidInt), want: 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := errcode.Exit(tc.err); got != tc.want {
				t.Errorf("Exit(%v) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}

func TestExit_AcrossEveryClass(t *testing.T) {
	// Pin the cross-OS contract: only 0, 1, or 2 — never 3+.
	codes := []*errcode.Code{errTestRefused, errTestTimeout, errTestInvalidInt, errTestNotFound, errTestMatrixLocked}
	for _, c := range codes {
		exit := errcode.Exit(c)
		if exit < 0 || exit > 2 {
			t.Errorf("Exit(%v) = %d, want one of {0, 1, 2}", c, exit)
		}
	}
}

func TestNew_PanicsOnEmptyLayer(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("New with empty Layer should panic")
		}
	}()
	_ = errcode.New("", "refused", errcode.ClassRuntime)
}

func TestNew_PanicsOnEmptyName(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("New with empty Name should panic")
		}
	}()
	_ = errcode.New(errcode.LayerTransport, "", errcode.ClassRuntime)
}

// TestContract_StableShape pins the wire-shape contract on stderr.
// Every typed code, wrapped with fmt.Errorf("%w: ...", ...), must
// produce "<layer>:<code>: <detail>" — that's what scripts grep on.
func TestContract_StableShape(t *testing.T) {
	for _, c := range []*errcode.Code{errTestRefused, errTestInvalidInt, errTestMatrixLocked} {
		wrapped := fmt.Errorf("%w: dynamic detail %d", c, 42)
		s := wrapped.Error()
		wantPrefix := string(c.Layer) + ":" + c.Name + ":"
		if len(s) < len(wantPrefix) || s[:len(wantPrefix)] != wantPrefix {
			t.Errorf("wrapped %v string = %q, want prefix %q", c, s, wantPrefix)
		}
	}
}
