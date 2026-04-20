package emberplus

import (
	"testing"

	"acp/internal/export/canonical"
	"acp/internal/protocol/emberplus/glow"
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
