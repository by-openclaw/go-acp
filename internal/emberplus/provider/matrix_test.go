package emberplus

import (
	"testing"

	"dhs/internal/export/canonical"
	"dhs/internal/emberplus/codec/glow"
)

// buildMatrixTree constructs a minimal tree: router(Node 1) → mat(Matrix 1.1)
// with labels basePath + per-cell parametersLocation wired but the child
// Nodes not populated (the Matrix element alone is what we exercise).
func buildMatrixTree(t *testing.T, m *canonical.Matrix) *server {
	t.Helper()
	m.Header = canonical.Header{
		Number: 1, Identifier: "mat", Path: "router.mat", OID: "1.1",
		IsOnline: true, Access: canonical.AccessReadWrite,
		Children: canonical.EmptyChildren(),
	}
	m.TargetCount = 4
	m.SourceCount = 4
	if m.Type == "" {
		m.Type = canonical.MatrixOneToN
	}
	root := &canonical.Node{
		Header: canonical.Header{
			Number: 1, Identifier: "router", Path: "router", OID: "1",
			IsOnline: true, Access: canonical.AccessRead,
			Children: []canonical.Element{m},
		},
	}
	srv := newServer(nil, &canonical.Export{Root: root})
	if srv.tree == nil {
		t.Fatal("tree failed to build")
	}
	return srv
}

// TestRoundTrip_Matrix_OneToN exercises a 4×4 oneToN matrix with one
// label level + connections. The consumer decoder must recover every
// field on the wire without error.
func TestRoundTrip_Matrix_OneToN(t *testing.T) {
	desc := "Primary"
	m := &canonical.Matrix{
		Type: canonical.MatrixOneToN,
		Mode: canonical.ModeLinear,
		Labels: []canonical.MatrixLabel{
			{BasePath: "1.2", Description: &desc},
		},
		Connections: []canonical.MatrixConnection{
			{Target: 0, Sources: []int64{0}, Operation: canonical.ConnOpAbsolute, Disposition: canonical.ConnDispTally},
			{Target: 1, Sources: []int64{3}, Operation: canonical.ConnOpAbsolute, Disposition: canonical.ConnDispTally},
		},
	}
	srv := buildMatrixTree(t, m)

	reply, err := srv.encodeGetDirReply(srv.tree.rootEntry(), false)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	els, err := glow.DecodeRoot(reply)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(els) != 1 || els[0].Matrix == nil {
		t.Fatalf("want 1 Matrix element, got %+v", els)
	}
	got := els[0].Matrix
	if got.Identifier != "mat" {
		t.Errorf("identifier=%q want mat", got.Identifier)
	}
	if got.MatrixType != glow.MatrixTypeOneToN {
		t.Errorf("type=%d want oneToN(0)", got.MatrixType)
	}
	if got.TargetCount != 4 || got.SourceCount != 4 {
		t.Errorf("counts=%d/%d want 4/4", got.TargetCount, got.SourceCount)
	}
	if len(got.Labels) != 1 || got.Labels[0].Description != "Primary" {
		t.Errorf("labels=%+v", got.Labels)
	}
	if len(got.Connections) != 2 {
		t.Fatalf("connections=%d want 2", len(got.Connections))
	}
	if got.Connections[1].Target != 1 || len(got.Connections[1].Sources) != 1 || got.Connections[1].Sources[0] != 3 {
		t.Errorf("conn[1]=%+v want target=1 sources=[3]", got.Connections[1])
	}
}

// TestRoundTrip_Matrix_NToN exercises nToN with parametersLocation + gain
// number + max caps — the richest common shape.
func TestRoundTrip_Matrix_NToN(t *testing.T) {
	pl := "1.3.2"
	gain := int64(1)
	maxT := int64(8)
	maxPT := int64(2)
	m := &canonical.Matrix{
		Type:                     canonical.MatrixNToN,
		Mode:                     canonical.ModeLinear,
		ParametersLocation:       &pl,
		GainParameterNumber:      &gain,
		MaximumTotalConnects:     &maxT,
		MaximumConnectsPerTarget: &maxPT,
	}
	srv := buildMatrixTree(t, m)
	reply, err := srv.encodeGetDirReply(srv.tree.rootEntry(), false)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	els, err := glow.DecodeRoot(reply)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	got := els[0].Matrix
	if got.MatrixType != glow.MatrixTypeNToN {
		t.Errorf("type=%d want nToN(2)", got.MatrixType)
	}
	if got.MaxTotalConnects != 8 || got.MaxConnectsPerTarget != 2 {
		t.Errorf("caps=%d/%d want 8/2", got.MaxTotalConnects, got.MaxConnectsPerTarget)
	}
	if got.GainParameterNumber != 1 {
		t.Errorf("gainParameterNumber=%d want 1", got.GainParameterNumber)
	}
	if pl, ok := got.ParametersLocation.([]int32); !ok || len(pl) != 3 {
		t.Errorf("parametersLocation=%v want []int32{1,3,2}", got.ParametersLocation)
	}
}

// TestMatrixLocked_RejectsConnect proves the full spec p.89 lock
// enforcement path end-to-end:
//
//  1. A canonical seed with disposition="locked" on target 2 must
//     populate the runtime lockStore at boot (so the rejection can
//     fire without an external setLock invocation).
//
//  2. An incoming Connect on the locked target must NOT mutate the
//     matrix state — m.Connections[target=2].Sources stays unchanged.
//
//  3. The provider's response to the locked Connect must echo back
//     the current sources with disposition=locked, signalling the
//     consumer that the request was seen and rejected.
func TestMatrixLocked_RejectsConnect(t *testing.T) {
	m := &canonical.Matrix{
		Type: canonical.MatrixNToN,
		Mode: canonical.ModeLinear,
		Connections: []canonical.MatrixConnection{
			{Target: 0, Sources: []int64{0}, Operation: canonical.ConnOpAbsolute, Disposition: canonical.ConnDispTally},
			{Target: 1, Sources: []int64{1}, Operation: canonical.ConnOpAbsolute, Disposition: canonical.ConnDispTally},
			{Target: 2, Sources: []int64{2}, Operation: canonical.ConnOpAbsolute, Disposition: canonical.ConnDispLocked, Locked: true},
		},
	}
	srv := buildMatrixTree(t, m)
	if srv.locks == nil {
		t.Fatal("lockStore not initialised on server")
	}
	if !srv.locks.isLocked("1.1", 2) {
		t.Fatal("target 2 should be locked from canonical seed but lockStore reports unlocked — boot-time pre-seed missing")
	}

	// Consumer tries to reroute the locked target to a new source.
	post, err := srv.applyMatrixConnections("1.1", []canonical.MatrixConnection{
		{Target: 2, Sources: []int64{99}, Operation: canonical.ConnOpAbsolute},
	})
	if err != nil {
		t.Fatalf("applyMatrixConnections: %v", err)
	}

	// Echo must carry disposition=locked + original sources unchanged.
	if len(post) != 1 {
		t.Fatalf("want 1 echo connection, got %d: %+v", len(post), post)
	}
	if post[0].Target != 2 {
		t.Errorf("echo target = %d, want 2", post[0].Target)
	}
	if post[0].Disposition != canonical.ConnDispLocked {
		t.Errorf("echo disposition = %q, want %q (spec p.89 reject)", post[0].Disposition, canonical.ConnDispLocked)
	}
	if len(post[0].Sources) != 1 || post[0].Sources[0] != 2 {
		t.Errorf("echo sources = %v, want [2] (state unchanged on locked target)", post[0].Sources)
	}

	// Mutation check: the underlying tree must still show target 2 → source 2.
	updated := srv.tree.byOID["1.1"].el.(*canonical.Matrix)
	for _, c := range updated.Connections {
		if c.Target == 2 {
			if len(c.Sources) != 1 || c.Sources[0] != 2 {
				t.Errorf("tree mutated: target 2 sources = %v, want [2] (lock should have blocked the change)", c.Sources)
			}
			return
		}
	}
	t.Fatal("target 2 connection vanished from tree after locked Connect")
}

// TestEncodeMatrix_DTD230Fields asserts the Matrix encoder emits the
// two MatrixContents fields introduced by DTD 2.30 (spec p.88):
//
//	[11] schemaIdentifiers — newline-separated string
//	[12] templateReference — RELATIVE-OID
func TestEncodeMatrix_DTD230Fields(t *testing.T) {
	schema := "com.lawo.signalMatrix/1"
	tmpl := "9.3"
	m := &canonical.Matrix{
		Type:              canonical.MatrixOneToN,
		Mode:              canonical.ModeLinear,
		SchemaIdentifiers: &schema,
		TemplateReference: &tmpl,
	}
	srv := buildMatrixTree(t, m)

	reply, err := srv.encodeGetDirReply(srv.tree.rootEntry(), false)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	els, err := glow.DecodeRoot(reply)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	got := els[0].Matrix
	if got.SchemaIdentifiers != schema {
		t.Errorf("schemaIdentifiers = %q, want %q", got.SchemaIdentifiers, schema)
	}
	if len(got.TemplateReference) != 2 || got.TemplateReference[0] != 9 || got.TemplateReference[1] != 3 {
		t.Errorf("templateReference = %v, want [9 3]", got.TemplateReference)
	}
}

// TestApplyConnection_Absolute replaces a target's sources.
func TestApplyConnection_Absolute(t *testing.T) {
	m := &canonical.Matrix{
		Type: canonical.MatrixNToN,
		Connections: []canonical.MatrixConnection{
			{Target: 0, Sources: []int64{0, 1}},
		},
	}
	srv := buildMatrixTree(t, m)
	post, err := srv.applyMatrixConnections("1.1", []canonical.MatrixConnection{
		{Target: 0, Sources: []int64{2, 3}, Operation: canonical.ConnOpAbsolute},
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(post) != 1 || len(post[0].Sources) != 2 || post[0].Sources[0] != 2 {
		t.Errorf("post=%+v want target 0 sources [2 3]", post)
	}
	if post[0].Disposition != canonical.ConnDispTally {
		t.Errorf("disposition=%q want tally", post[0].Disposition)
	}
}

// TestApplyConnection_OneToOne_Exclusivity asserts that reassigning a
// source to a new target releases it from the previous target — the
// bijection that defines oneToOne. Each apply returns a tally for the
// loser + the winner.
func TestApplyConnection_OneToOne_Exclusivity(t *testing.T) {
	m := &canonical.Matrix{
		Type: canonical.MatrixOneToOne,
		Connections: []canonical.MatrixConnection{
			{Target: 0, Sources: []int64{3}}, // t-0 holds s-3
			{Target: 1, Sources: []int64{2}}, // t-1 holds s-2
		},
	}
	srv := buildMatrixTree(t, m)
	// Client sends "t-1 → s-3" — must steal s-3 from t-0.
	post, err := srv.applyMatrixConnections("1.1", []canonical.MatrixConnection{
		{Target: 1, Sources: []int64{3}, Operation: canonical.ConnOpAbsolute},
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	byT := map[int64][]int64{}
	for _, c := range post {
		byT[c.Target] = c.Sources
	}
	if got := byT[1]; len(got) != 1 || got[0] != 3 {
		t.Errorf("t-1 post=%v want [3]", got)
	}
	if _, seen := byT[0]; !seen {
		t.Fatal("t-0 loser not emitted — consumer won't redraw")
	}
	if got := byT[0]; len(got) != 0 {
		t.Errorf("t-0 post=%v want [] (source stolen)", got)
	}
}

// TestApplyConnection_OneToN_SingleSource asserts target cardinality is
// clamped to 1 even when the client sends extra sources.
func TestApplyConnection_OneToN_SingleSource(t *testing.T) {
	m := &canonical.Matrix{Type: canonical.MatrixOneToN}
	srv := buildMatrixTree(t, m)
	post, err := srv.applyMatrixConnections("1.1", []canonical.MatrixConnection{
		{Target: 0, Sources: []int64{0, 1, 2}, Operation: canonical.ConnOpAbsolute},
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(post[0].Sources) != 1 {
		t.Errorf("oneToN target kept %d sources, want 1", len(post[0].Sources))
	}
}

// TestApplyConnection_OneToN_ConnectOp_Replaces: EmberViewer (and most
// Ember+ clients) send Operation=Connect on user clicks. For a oneToN
// matrix, Connect MUST replace the target's single source — not union.
// Without the coercion, mergeSources([0], [2]) = [0, 2] and the target
// ends up with 2 sources, violating the oneToN invariant.
func TestApplyConnection_OneToN_ConnectOp_Replaces(t *testing.T) {
	m := &canonical.Matrix{
		Type: canonical.MatrixOneToN,
		Connections: []canonical.MatrixConnection{
			{Target: 0, Sources: []int64{0}}, // t-0 currently ← s-0
		},
	}
	srv := buildMatrixTree(t, m)
	post, err := srv.applyMatrixConnections("1.1", []canonical.MatrixConnection{
		{Target: 0, Sources: []int64{2}, Operation: canonical.ConnOpConnect},
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(post[0].Sources) != 1 {
		t.Fatalf("oneToN + Connect must keep exactly 1 source, got %v", post[0].Sources)
	}
	if post[0].Sources[0] != 2 {
		t.Errorf("oneToN + Connect must replace, got src=%d want 2", post[0].Sources[0])
	}
}

// TestApplyConnection_OneToOne_ConnectOp_ReplacesAndSteals: on a user
// click, Operation=Connect against a oneToOne matrix must replace the
// target's source AND strip the newly-bound source from any other
// target that held it (source-exclusivity / loser tally).
func TestApplyConnection_OneToOne_ConnectOp_ReplacesAndSteals(t *testing.T) {
	m := &canonical.Matrix{
		Type: canonical.MatrixOneToOne,
		Connections: []canonical.MatrixConnection{
			{Target: 0, Sources: []int64{0}}, // t-0 ← s-0
			{Target: 1, Sources: []int64{1}}, // t-1 ← s-1
		},
	}
	srv := buildMatrixTree(t, m)
	// User routes s-0 onto t-1 via EmberViewer → Operation=Connect.
	post, err := srv.applyMatrixConnections("1.1", []canonical.MatrixConnection{
		{Target: 1, Sources: []int64{0}, Operation: canonical.ConnOpConnect},
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	byT := map[int64][]int64{}
	for _, c := range post {
		byT[c.Target] = c.Sources
	}
	if got := byT[1]; len(got) != 1 || got[0] != 0 {
		t.Errorf("t-1 post=%v want [0] (Connect replaces, keeps exactly 1 src)", got)
	}
	if _, seen := byT[0]; !seen {
		t.Fatal("t-0 loser tally not emitted — consumer won't redraw")
	}
	if got := byT[0]; len(got) != 0 {
		t.Errorf("t-0 post=%v want [] (s-0 stolen by t-1)", got)
	}
}

// TestApplyConnection_OneToN_Disconnect_ClearsTarget asserts that
// Disconnect on a oneToN matrix actually CLEARS the target's source
// per spec p.89. Earlier (PR #98) Disconnect was coerced to Absolute,
// which kept the target routed instead of clearing it — that was the
// regression. Empty sources[] after the subtract is valid; the spec
// permits an unrouted target.
func TestApplyConnection_OneToN_Disconnect_ClearsTarget(t *testing.T) {
	m := &canonical.Matrix{
		Type: canonical.MatrixOneToN,
		Connections: []canonical.MatrixConnection{
			{Target: 0, Sources: []int64{5}}, // t-0 currently ← s-5
		},
	}
	srv := buildMatrixTree(t, m)
	post, err := srv.applyMatrixConnections("1.1", []canonical.MatrixConnection{
		{Target: 0, Sources: []int64{5}, Operation: canonical.ConnOpDisconnect},
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(post) != 1 {
		t.Fatalf("want 1 echo connection, got %d", len(post))
	}
	if len(post[0].Sources) != 0 {
		t.Errorf("post sources = %v, want [] (Disconnect cleared the target — PR #98 regression check)", post[0].Sources)
	}
	// Tree state must match.
	updated := srv.tree.byOID["1.1"].el.(*canonical.Matrix)
	for _, c := range updated.Connections {
		if c.Target == 0 && len(c.Sources) != 0 {
			t.Errorf("tree target 0 sources = %v, want [] (Disconnect must clear in tree, not echo only)", c.Sources)
		}
	}
}

// TestApplyConnection_OneToOne_Disconnect_LeavesEmpty: same Disconnect
// semantics on oneToOne. After Disconnect{tgt=0, sources=[3]} the target
// has no source and the bijection allows that empty state per spec.
func TestApplyConnection_OneToOne_Disconnect_LeavesEmpty(t *testing.T) {
	m := &canonical.Matrix{
		Type: canonical.MatrixOneToOne,
		Connections: []canonical.MatrixConnection{
			{Target: 0, Sources: []int64{3}},
			{Target: 1, Sources: []int64{4}},
		},
	}
	srv := buildMatrixTree(t, m)
	post, err := srv.applyMatrixConnections("1.1", []canonical.MatrixConnection{
		{Target: 0, Sources: []int64{3}, Operation: canonical.ConnOpDisconnect},
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(post) != 1 || post[0].Target != 0 || len(post[0].Sources) != 0 {
		t.Errorf("post = %+v, want [{target=0 sources=[]}]", post)
	}
	// t-1 must remain untouched.
	updated := srv.tree.byOID["1.1"].el.(*canonical.Matrix)
	for _, c := range updated.Connections {
		if c.Target == 1 && (len(c.Sources) != 1 || c.Sources[0] != 4) {
			t.Errorf("t-1 unintentionally changed: %v, want [4]", c.Sources)
		}
	}
}

// TestApplyConnection_NToN_MaxPerTarget_Rejects asserts spec p.88
// maximumConnectsPerTarget enforcement. A Connect that would put the
// target over the per-target cap is rejected — the echo carries the
// unchanged current sources with disposition=tally and the tree state
// stays put.
func TestApplyConnection_NToN_MaxPerTarget_Rejects(t *testing.T) {
	maxPT := int64(2)
	m := &canonical.Matrix{
		Type:                     canonical.MatrixNToN,
		MaximumConnectsPerTarget: &maxPT,
		Connections: []canonical.MatrixConnection{
			{Target: 0, Sources: []int64{0, 1}}, // already at the cap
		},
	}
	srv := buildMatrixTree(t, m)
	post, err := srv.applyMatrixConnections("1.1", []canonical.MatrixConnection{
		{Target: 0, Sources: []int64{2}, Operation: canonical.ConnOpConnect},
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(post) != 1 {
		t.Fatalf("want 1 echo, got %d", len(post))
	}
	if len(post[0].Sources) != 2 || post[0].Sources[0] != 0 || post[0].Sources[1] != 1 {
		t.Errorf("echo sources = %v, want [0 1] (cap enforced, state unchanged)", post[0].Sources)
	}
	// Tree must be unchanged.
	updated := srv.tree.byOID["1.1"].el.(*canonical.Matrix)
	if len(updated.Connections[0].Sources) != 2 {
		t.Errorf("tree mutated to %v despite cap rejection", updated.Connections[0].Sources)
	}
}

// TestApplyConnection_NToN_MaxTotal_Rejects asserts spec p.88
// maximumTotalConnects enforcement across the whole matrix.
func TestApplyConnection_NToN_MaxTotal_Rejects(t *testing.T) {
	maxTotal := int64(3)
	m := &canonical.Matrix{
		Type:                 canonical.MatrixNToN,
		MaximumTotalConnects: &maxTotal,
		Connections: []canonical.MatrixConnection{
			{Target: 0, Sources: []int64{0}},
			{Target: 1, Sources: []int64{1, 2}}, // grand total now 3 — at the cap
		},
	}
	srv := buildMatrixTree(t, m)
	// Adding even one more crosspoint would push total to 4 → reject.
	post, err := srv.applyMatrixConnections("1.1", []canonical.MatrixConnection{
		{Target: 2, Sources: []int64{0}, Operation: canonical.ConnOpConnect},
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(post[0].Sources) != 0 {
		t.Errorf("target 2 echo = %v, want [] (total cap enforced)", post[0].Sources)
	}
	updated := srv.tree.byOID["1.1"].el.(*canonical.Matrix)
	for _, c := range updated.Connections {
		if c.Target == 2 && len(c.Sources) != 0 {
			t.Errorf("tree mutated: target 2 has %v despite total-cap rejection", c.Sources)
		}
	}
}

// TestApplyConnection_NToN_ConnectOp_Unions confirms nToN is NOT
// coerced — Connect on nToN still unions (many-to-many is the point).
func TestApplyConnection_NToN_ConnectOp_Unions(t *testing.T) {
	m := &canonical.Matrix{
		Type: canonical.MatrixNToN,
		Connections: []canonical.MatrixConnection{
			{Target: 0, Sources: []int64{0}},
		},
	}
	srv := buildMatrixTree(t, m)
	post, err := srv.applyMatrixConnections("1.1", []canonical.MatrixConnection{
		{Target: 0, Sources: []int64{1}, Operation: canonical.ConnOpConnect},
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(post[0].Sources) != 2 {
		t.Fatalf("nToN + Connect must union, got %v want [0 1]", post[0].Sources)
	}
}

// TestApplyConnection_ConnectAndDisconnect exercises the nToN additive ops.
func TestApplyConnection_ConnectAndDisconnect(t *testing.T) {
	m := &canonical.Matrix{
		Type: canonical.MatrixNToN,
		Connections: []canonical.MatrixConnection{
			{Target: 0, Sources: []int64{0}},
		},
	}
	srv := buildMatrixTree(t, m)

	post, err := srv.applyMatrixConnections("1.1", []canonical.MatrixConnection{
		{Target: 0, Sources: []int64{1}, Operation: canonical.ConnOpConnect},
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if len(post[0].Sources) != 2 {
		t.Fatalf("after connect want 2 sources, got %v", post[0].Sources)
	}

	post, err = srv.applyMatrixConnections("1.1", []canonical.MatrixConnection{
		{Target: 0, Sources: []int64{0}, Operation: canonical.ConnOpDisconnect},
	})
	if err != nil {
		t.Fatalf("disconnect: %v", err)
	}
	if len(post[0].Sources) != 1 || post[0].Sources[0] != 1 {
		t.Errorf("after disconnect want [1], got %v", post[0].Sources)
	}
}
