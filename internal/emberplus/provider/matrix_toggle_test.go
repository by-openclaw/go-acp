package emberplus

import (
	"testing"

	"dhs/internal/export/canonical"
)

// TestApplyConnection_OneToOne_AbsoluteSameSource_TogglesOff pins the oneToOne
// toggle-off disconnect. Verified live on the wire against EmberPlusViewer 2.40
// (emberplus_matrix_capture.pcapng): a oneToOne matrix has no separate unroute
// gesture, so the viewer disconnects a lit crosspoint by re-sending the SAME
// source with operation=absolute. The provider must read "absolute-select the
// already-connected source" as DISCONNECT and clear the target to empty.
func TestApplyConnection_OneToOne_AbsoluteSameSource_TogglesOff(t *testing.T) {
	m := &canonical.Matrix{
		Type: canonical.MatrixOneToOne,
		Connections: []canonical.MatrixConnection{
			{Target: 0, Sources: []int64{15}}, // t-0 currently ← s-15
		},
	}
	srv := buildMatrixTree(t, m)
	// Re-send the current source with absolute (default operation) — the
	// EmberPlusViewer disconnect gesture for oneToOne.
	post, err := srv.applyMatrixConnections("1.1", []canonical.MatrixConnection{
		{Target: 0, Sources: []int64{15}, Operation: canonical.ConnOpAbsolute},
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(post) != 1 || post[0].Target != 0 || len(post[0].Sources) != 0 {
		t.Errorf("post = %+v, want [{target=0 sources=[]}] (toggle-off)", post)
	}
	updated := srv.tree.byOID["1.1"].el.(*canonical.Matrix)
	for _, c := range updated.Connections {
		if c.Target == 0 && len(c.Sources) != 0 {
			t.Errorf("tree target 0 sources = %v, want [] (toggle must clear in tree)", c.Sources)
		}
	}
}

// TestApplyConnection_OneToOne_AbsoluteDifferentSource_Replaces confirms the
// toggle only fires for the SAME source: routing target 0 from s-15 to s-9
// (absolute) replaces, it does not disconnect.
func TestApplyConnection_OneToOne_AbsoluteDifferentSource_Replaces(t *testing.T) {
	m := &canonical.Matrix{
		Type: canonical.MatrixOneToOne,
		Connections: []canonical.MatrixConnection{
			{Target: 0, Sources: []int64{15}},
		},
	}
	srv := buildMatrixTree(t, m)
	post, err := srv.applyMatrixConnections("1.1", []canonical.MatrixConnection{
		{Target: 0, Sources: []int64{9}, Operation: canonical.ConnOpAbsolute},
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(post) != 1 || post[0].Target != 0 || len(post[0].Sources) != 1 || post[0].Sources[0] != 9 {
		t.Errorf("post = %+v, want [{target=0 sources=[9]}] (replace, not toggle)", post)
	}
}

// TestApplyConnection_OneToN_AbsoluteSameSource_StaysConnected pins the oneToN
// EXCLUSION from the toggle. A oneToN target always holds a source (you
// re-route it, never unroute to nothing), so re-selecting its current source is
// a confirming no-op — NOT a disconnect. Applying the toggle here would be the
// wrong behavior (caught during live EmberPlusViewer testing).
func TestApplyConnection_OneToN_AbsoluteSameSource_StaysConnected(t *testing.T) {
	m := &canonical.Matrix{
		Type: canonical.MatrixOneToN,
		Connections: []canonical.MatrixConnection{
			{Target: 0, Sources: []int64{6}}, // t-0 currently ← s-6
		},
	}
	srv := buildMatrixTree(t, m)
	post, err := srv.applyMatrixConnections("1.1", []canonical.MatrixConnection{
		{Target: 0, Sources: []int64{6}, Operation: canonical.ConnOpAbsolute},
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(post) != 1 || post[0].Target != 0 || len(post[0].Sources) != 1 || post[0].Sources[0] != 6 {
		t.Errorf("post = %+v, want [{target=0 sources=[6]}] (oneToN keeps the source, no toggle)", post)
	}
	updated := srv.tree.byOID["1.1"].el.(*canonical.Matrix)
	for _, c := range updated.Connections {
		if c.Target == 0 && (len(c.Sources) != 1 || c.Sources[0] != 6) {
			t.Errorf("tree target 0 sources = %v, want [6] (oneToN must not toggle off)", c.Sources)
		}
	}
}
