package emberplus

import (
	"strings"
	"testing"

	"dhs/internal/export/canonical"
)

// TestBuiltins_ErrorOnUnresolvedMatrix pins #455: every builtin that
// accepts a matrixPath (setLock / listLocks / storeSalvo / recallSalvo /
// listSalvos / getSalvo) must return (nil, err) — Success=false on the
// wire — when resolveMatrix(matrixRef) misses, so the consumer can
// distinguish "matrix not found" from a real falsy result.
//
// Pre-#455 the same builtins silently returned a falsy default
// (`[]any{false}`, `[]any{""}`, `[]any{int64(0)}`) on miss, which the
// consumer's invoke verb prints as "invocation 1: success=true
// result: [false]" — indistinguishable from a real operation that
// returned false.
func TestBuiltins_ErrorOnUnresolvedMatrix(t *testing.T) {
	m := &canonical.Matrix{Type: canonical.MatrixOneToN}
	srv := buildMatrixTree(t, m)

	cases := []struct {
		name string
		fn   FunctionImpl
		args []any
	}{
		{"setLock", srv.makeBuiltinSetLock(), []any{"bogus.path", int64(0), true}},
		{"listLocks", srv.makeBuiltinListLocks(), []any{"bogus.path"}},
		{"storeSalvo", srv.makeBuiltinStoreSalvo(), []any{"bogus.path", int64(7), ""}},
		{"recallSalvo", srv.makeBuiltinRecallSalvo(), []any{"bogus.path", int64(7)}},
		{"listSalvos", srv.makeBuiltinListSalvos(), []any{"bogus.path"}},
		{"getSalvo", srv.makeBuiltinGetSalvo(), []any{"bogus.path", int64(7)}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := tc.fn(tc.args)
			if err == nil {
				t.Fatalf("expected non-nil error on unresolved matrix, got nil (res=%v)", res)
			}
			if res != nil {
				t.Errorf("on error the result tuple must be nil (so Success=false), got %v", res)
			}
			if !strings.Contains(err.Error(), "bogus.path") {
				t.Errorf("error %q should mention the rejected matrixRef", err.Error())
			}
			if !strings.Contains(err.Error(), tc.name) {
				t.Errorf("error %q should name the builtin (%s)", err.Error(), tc.name)
			}
		})
	}
}

// TestBuiltins_HappyPathUnchanged confirms the resolved-matrix path
// still returns (result, nil) — the fix in #455 must only impact the
// previously-silent miss branch, not the working path.
func TestBuiltins_HappyPathUnchanged(t *testing.T) {
	m := &canonical.Matrix{
		Type: canonical.MatrixNToN,
		Connections: []canonical.MatrixConnection{
			{Target: 0, Sources: []int64{1}},
		},
	}
	srv := buildMatrixTree(t, m)

	if res, err := srv.makeBuiltinSetLock()([]any{"1.1", int64(0), true}); err != nil {
		t.Errorf("setLock happy path: unexpected error %v", err)
	} else if len(res) != 1 || res[0] != false {
		t.Errorf("setLock happy path: result %v, want [false] (prev unlocked)", res)
	}
	if _, err := srv.makeBuiltinListLocks()([]any{"1.1"}); err != nil {
		t.Errorf("listLocks happy path: unexpected error %v", err)
	}
	if res, err := srv.makeBuiltinStoreSalvo()([]any{"1.1", int64(1), ""}); err != nil {
		t.Errorf("storeSalvo happy path: unexpected error %v", err)
	} else if len(res) != 1 || res[0] != true {
		t.Errorf("storeSalvo happy path: result %v, want [true]", res)
	}
	if res, err := srv.makeBuiltinListSalvos()([]any{"1.1"}); err != nil {
		t.Errorf("listSalvos happy path: unexpected error %v", err)
	} else if len(res) != 1 {
		t.Errorf("listSalvos happy path: result %v, want non-empty string tuple", res)
	}
	if _, err := srv.makeBuiltinRecallSalvo()([]any{"1.1", int64(1)}); err != nil {
		t.Errorf("recallSalvo happy path: unexpected error %v", err)
	}
	if _, err := srv.makeBuiltinGetSalvo()([]any{"1.1", int64(1)}); err != nil {
		t.Errorf("getSalvo happy path: unexpected error %v", err)
	}
}

// TestBuiltins_NonMatrixRefIsError covers a sub-case of #455: when the
// ref resolves to *something* in the tree but it isn't a Matrix
// element, resolveMatrix returns ok=false. Same error contract — the
// builtin must surface that rather than silently coerce.
func TestBuiltins_NonMatrixRefIsError(t *testing.T) {
	// buildMatrixTree's root is a Node with OID="1" — point at it
	// (not the matrix at "1.1") and confirm the builtin errors.
	m := &canonical.Matrix{Type: canonical.MatrixOneToN}
	srv := buildMatrixTree(t, m)

	_, err := srv.makeBuiltinSetLock()([]any{"1", int64(0), true})
	if err == nil {
		t.Fatal("setLock against non-matrix OID should error, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error %q should say 'not found'", err.Error())
	}
}
