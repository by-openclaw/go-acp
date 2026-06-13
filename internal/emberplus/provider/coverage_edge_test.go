package emberplus

import (
	"testing"

	"dhs/internal/export/canonical"
)

// TestSetupBuiltinFunctions_NilTree covers the nil-tree guard via a
// direct call on a server whose tree never loaded.
func TestSetupBuiltinFunctions_NilTree(t *testing.T) {
	s := &server{} // tree == nil
	s.setupBuiltinFunctions()
	if s.salvos != nil || s.locks != nil {
		t.Error("nil-tree setup must not allocate stores")
	}
}

// TestEncodeGetDirReply_RootTemplateError covers the encodeQualifiedTemplate
// error propagation when the served tree has a template with a bad OID and
// the reply targets the root (templates join the RootElementCollection).
func TestEncodeGetDirReply_RootTemplateError(t *testing.T) {
	root := &canonical.Node{Header: canonical.Header{
		Number: 1, Identifier: "root", Path: "root", OID: "1", IsOnline: true,
		Access: canonical.AccessRead, Children: canonical.EmptyChildren(),
	}}
	srv := newServer(nil, &canonical.Export{
		Root: root,
		Templates: []*canonical.TemplateEntry{{
			OID:        "x.y", // non-numeric → encodeQualifiedTemplate errors
			Identifier: "t",
			Template: &canonical.Node{Header: canonical.Header{
				Number: 1, Identifier: "n", IsOnline: true, Children: canonical.EmptyChildren(),
			}},
		}},
	})
	if _, err := srv.encodeGetDirReply(srv.tree.rootEntry(), false); err == nil {
		t.Error("root reply with a bad template should propagate the encode error")
	}
}

// TestEncodeMatrixContents_BadLabel covers the encodeLabel error branch
// inside encodeMatrixContents (a label whose basePath is non-numeric).
func TestEncodeMatrixContents_BadLabel(t *testing.T) {
	m := &canonical.Matrix{
		Header: canonical.Header{Identifier: "m"},
		Type:   canonical.MatrixOneToN,
		Labels: []canonical.MatrixLabel{{BasePath: "a.b"}}, // bad → encodeLabel errors
	}
	if _, err := encodeMatrixContents(m); err == nil {
		t.Error("encodeMatrixContents with a bad label should error")
	}
}

// TestCollectStreams_NilChild covers the walk's nil-element guard. The
// tree index builder rejects nil children, so we construct the tree
// directly to place a nil element alongside a real stream Parameter —
// the guard at the top of collectStreams' walk is the safety net for any
// future code path that admits a nil child.
func TestCollectStreams_NilChild(t *testing.T) {
	root := &canonical.Node{Header: canonical.Header{
		Number: 1, Identifier: "root", Path: "root", OID: "1", IsOnline: true,
		Access: canonical.AccessRead,
		Children: []canonical.Element{
			nil, // exercises the `if el == nil { return }` guard
			&canonical.Parameter{
				Header:           canonical.Header{Number: 1, Identifier: "m", OID: "1.1", IsOnline: true, Children: canonical.EmptyChildren()},
				Type:             canonical.ParamInteger,
				StreamIdentifier: i64p(7),
			},
		},
	}}
	srv := &server{tree: &tree{root: root}}
	got := srv.collectStreams()
	if len(got) != 1 || got[0].id != 7 {
		t.Errorf("collectStreams with a nil child = %+v, want one stream id=7", got)
	}
}

// TestMergeSources_ExistingDup covers mergeSources' dedup-within-existing
// branch: a matrix connection whose stored Sources already contain a
// duplicate, then a Connect that merges in another source.
func TestMergeSources_ExistingDup(t *testing.T) {
	m := &canonical.Matrix{
		Type:                     canonical.MatrixNToN,
		MaximumConnectsPerTarget: i64p(10),
		MaximumTotalConnects:     i64p(10),
		Connections: []canonical.MatrixConnection{
			{Target: 0, Sources: []int64{1, 1, 2}}, // pre-existing self-duplicate
		},
	}
	srv := buildMatrixTree(t, m)
	post, err := srv.applyMatrixConnections("1.1", []canonical.MatrixConnection{
		{Target: 0, Sources: []int64{3}, Operation: canonical.ConnOpConnect},
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	// Existing self-dup collapses → {1,2,3}.
	if len(post) != 1 || len(post[0].Sources) != 3 {
		t.Errorf("mergeSources existing-dup: %+v", post)
	}
}

// TestRecallSalvo_ApplyError covers makeBuiltinRecallSalvo's
// applyMatrixConnections error propagation (builtins.go:312). A matrix
// with an empty OID but a resolvable Path resolves via lookupPath, so
// resolveMatrix returns "" as the OID; applyMatrixConnections("") then
// fails the byOID lookup and the recall surfaces the error.
func TestRecallSalvo_ApplyError(t *testing.T) {
	mtx := &canonical.Matrix{
		Header: canonical.Header{
			Number: 1, Identifier: "mat", Path: "router.mat", OID: "", // no OID
			IsOnline: true, Access: canonical.AccessReadWrite, Children: canonical.EmptyChildren(),
		},
		Type: canonical.MatrixNToN, TargetCount: 2, SourceCount: 2,
	}
	root := &canonical.Node{Header: canonical.Header{
		Number: 1, Identifier: "router", Path: "router", OID: "1", IsOnline: true,
		Access: canonical.AccessRead, Children: []canonical.Element{mtx},
	}}
	srv := newServer(nil, &canonical.Export{Root: root})
	// Store a salvo under the empty OID key — that is the key recall uses
	// once resolveMatrix returns "".
	srv.salvos.store("", 1, []canonical.MatrixConnection{
		{Target: 0, Sources: []int64{1}, Operation: canonical.ConnOpConnect},
	})

	recall := srv.makeBuiltinRecallSalvo()
	if _, err := recall([]any{"router.mat", int64(1)}); err == nil {
		t.Error("recallSalvo should surface the applyMatrixConnections error for an empty-OID matrix")
	}
}

// TestApplyMatrixConnections_OneToOneNoStrip covers the source-steal
// loop's len-equal continue branch (174): a oneToOne connect where
// another target holds a DIFFERENT source, so the strip leaves its
// sources unchanged and the loop continues without emitting.
func TestApplyMatrixConnections_OneToOneNoStrip(t *testing.T) {
	m := &canonical.Matrix{
		Type: canonical.MatrixOneToOne,
		Connections: []canonical.MatrixConnection{
			{Target: 1, Sources: []int64{2}}, // holds source 2 (different)
		},
	}
	srv := buildMatrixTree(t, m)
	// Connect source 0 to target 0 — target 1 holds source 2, untouched.
	post, err := srv.applyMatrixConnections("1.1", []canonical.MatrixConnection{
		{Target: 0, Sources: []int64{0}, Operation: canonical.ConnOpConnect},
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	// Only target 0 changed; target 1 not re-emitted (no steal).
	if len(post) != 1 || post[0].Target != 0 {
		t.Errorf("oneToOne no-strip: %+v", post)
	}
}
