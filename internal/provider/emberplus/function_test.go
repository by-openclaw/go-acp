package emberplus

import (
	"testing"

	"acp/internal/export/canonical"
	"acp/internal/protocol/emberplus/glow"
)

// TestRoundTrip_Function checks a Function with 2 integer args → 1 result
// round-trips shape + tuple descriptions through the consumer decoder.
func TestRoundTrip_Function(t *testing.T) {
	f := &canonical.Function{
		Header: canonical.Header{
			Number: 1, Identifier: "sum", Path: "r.sum", OID: "1.1",
			IsOnline: true, Access: canonical.AccessRead,
			Children: canonical.EmptyChildren(),
		},
		Arguments: []canonical.TupleItem{
			{Name: "a", Type: canonical.ParamInteger},
			{Name: "b", Type: canonical.ParamInteger},
		},
		Result: []canonical.TupleItem{
			{Name: "sum", Type: canonical.ParamInteger},
		},
	}
	root := &canonical.Node{
		Header: canonical.Header{
			Number: 1, Identifier: "r", Path: "r", OID: "1",
			IsOnline: true, Access: canonical.AccessRead,
			Children: []canonical.Element{f},
		},
	}
	srv := newServer(nil, &canonical.Export{Root: root})

	reply, err := srv.encodeGetDirReply(srv.tree.rootEntry(), false)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	els, err := glow.DecodeRoot(reply)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(els) != 1 || els[0].Function == nil {
		t.Fatalf("want 1 Function element, got %+v", els)
	}
	got := els[0].Function
	if got.Identifier != "sum" {
		t.Errorf("identifier=%q want sum", got.Identifier)
	}
	if len(got.Arguments) != 2 {
		t.Fatalf("arguments=%d want 2", len(got.Arguments))
	}
	if got.Arguments[0].Name != "a" || got.Arguments[0].Type != glow.ParamTypeInteger {
		t.Errorf("arg[0]=%+v", got.Arguments[0])
	}
	if len(got.Result) != 1 || got.Result[0].Name != "sum" {
		t.Errorf("result=%+v", got.Result)
	}
}

// TestInvocationResult checks that Root→InvocationResult decodes back with
// the right id + tuple values (int, bool, string).
func TestInvocationResult(t *testing.T) {
	srv := newServer(nil, nil) // no tree — only encoder exercised
	payload := srv.encodeInvocationResult(42, true, []any{int64(7), true, "ok"})

	els, err := glow.DecodeRoot(payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(els) != 1 || els[0].InvocationResult == nil {
		t.Fatalf("want 1 InvocationResult, got %+v", els)
	}
	r := els[0].InvocationResult
	if r.InvocationID != 42 {
		t.Errorf("invocationID=%d want 42", r.InvocationID)
	}
	if !r.Success {
		t.Error("success=false, want true (default when omitted)")
	}
	if len(r.Result) != 3 {
		t.Fatalf("result len=%d want 3", len(r.Result))
	}
	if v, _ := r.Result[0].(int64); v != 7 {
		t.Errorf("result[0]=%v want 7", r.Result[0])
	}
	if v, _ := r.Result[1].(bool); !v {
		t.Errorf("result[1]=%v want true", r.Result[1])
	}
	if v, _ := r.Result[2].(string); v != "ok" {
		t.Errorf("result[2]=%v want ok", r.Result[2])
	}
}

// TestBuiltinSum exercises the auto-registered builtin.
func TestBuiltinSum(t *testing.T) {
	got, err := builtinSum([]any{int64(3), int64(4)})
	if err != nil {
		t.Fatalf("sum: %v", err)
	}
	if len(got) != 1 || got[0] != int64(7) {
		t.Errorf("sum got %v want [7]", got)
	}

	if _, err := builtinSum([]any{int64(1)}); err == nil {
		t.Error("want error on missing arg")
	}
}

// TestSalvoStoreRecall checks the salvo store preserves a deep copy so
// later mutations to the matrix's live connections don't leak back.
func TestSalvoStoreRecall(t *testing.T) {
	s := newSalvoStore()
	live := []canonical.MatrixConnection{{Target: 0, Sources: []int64{5}}}
	s.store("1.1", 1, live)
	// Mutate the live slice — saved copy must be independent.
	live[0].Sources[0] = 99

	got, ok := s.recall("1.1", 1)
	if !ok {
		t.Fatal("recall missed")
	}
	if got[0].Sources[0] != 5 {
		t.Errorf("saved source=%d want 5 (deep-copy broken)", got[0].Sources[0])
	}
}

// TestResolveMatrix_AcceptsOIDAndPath asserts the salvo functions accept
// either the numeric OID or the dotted identifier path for a Matrix,
// and reject refs that point at non-matrix elements.
func TestResolveMatrix_AcceptsOIDAndPath(t *testing.T) {
	m := &canonical.Matrix{Type: canonical.MatrixOneToN}
	srv := buildMatrixTree(t, m)

	cases := []struct {
		name, ref string
		wantOK    bool
	}{
		{"by OID", "1.1", true},
		{"by dotted path", "router.mat", true},
		{"non-matrix OID", "1", false},
		{"non-existent OID", "9.9.9", false},
		{"empty", "", false},
	}
	for _, c := range cases {
		oid, got, ok := srv.resolveMatrix(c.ref)
		if ok != c.wantOK {
			t.Errorf("%s: ok=%v want %v (ref=%q)", c.name, ok, c.wantOK, c.ref)
			continue
		}
		if c.wantOK && (oid != "1.1" || got != m) {
			t.Errorf("%s: resolved to oid=%q m=%v want 1.1 / the Matrix", c.name, oid, got)
		}
	}
}
