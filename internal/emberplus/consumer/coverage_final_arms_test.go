package emberplus

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"dhs/internal/consumer"
	"dhs/internal/consumer/compliance"
	"dhs/internal/datastore"
	"dhs/internal/emberplus/codec/glow"
	"dhs/internal/emberplus/codec/matrix"
	"dhs/internal/emberplus/codec/s101"
	"dhs/internal/export/canonical"
	"dhs/internal/transport"
)

// newTempRecorder creates a transport.Recorder writing to a temp file,
// for tests that need to exercise the recorder-install code paths.
func newTempRecorder(t *testing.T) (*transport.Recorder, error) {
	t.Helper()
	return transport.NewRecorder(filepath.Join(t.TempDir(), "rec.jsonl"))
}

// nilCommonElement is a canonical.Element whose Common() returns nil,
// used to exercise rootOID's defensive nil-Common branch directly. No
// real canonical element behaves this way (every concrete type embeds
// *Header), so this branch is otherwise unreachable.
type nilCommonElement struct{}

func (nilCommonElement) Kind() string              { return "test" }
func (nilCommonElement) Common() *canonical.Header { return nil }

// TestRootOID covers rootOID across all three arms: nil element, an
// element with a Header (returns its OID), and an element whose Common()
// is nil (returns "").
func TestRootOID(t *testing.T) {
	if got := rootOID(nil); got != "" {
		t.Errorf("rootOID(nil) = %q, want empty", got)
	}
	if got := rootOID(&canonical.Node{Header: canonical.Header{OID: "1.2"}}); got != "1.2" {
		t.Errorf("rootOID(node) = %q, want 1.2", got)
	}
	if got := rootOID(nilCommonElement{}); got != "" {
		t.Errorf("rootOID(nil-Common) = %q, want empty", got)
	}
}

// TestSeedOneEntry_TemplateDirect covers seedOneEntry's "template" case
// directly. SeedTreeFromCachedObjects intercepts template objects before
// seedOneEntry, so this case is only reachable by calling the function
// directly — which is a legitimate unit test of a real code path.
func TestSeedOneEntry_TemplateDirect(t *testing.T) {
	p := freshPlugin()
	got := p.seedOneEntry(consumer.Object{
		OID: "5", Path: []string{"tmpl"},
		Meta: map[string]any{"element": "template"},
	}, time.Now())
	if got != nil {
		t.Errorf("template element → seedOneEntry must return nil, got %+v", got)
	}
}

// TestBuildTemplateEntry_EmptyChoice covers the default (return nil) arm:
// a Template whose Element struct has no populated CHOICE field.
func TestBuildTemplateEntry_EmptyChoice(t *testing.T) {
	p := freshPlugin()
	if got := p.buildTemplateEntry(&glow.Template{
		Number: 1, Path: []int32{1}, Element: &glow.TemplateElement{}, // all nil
	}); got != nil {
		t.Errorf("template with empty CHOICE → want nil, got %+v", got)
	}
}

// TestCanonicalize_MatrixMultiConnSort covers buildMatrix's source-sort
// (497) and connection-sort (507) comparators, which only execute with
// 2+ sources and 2+ connections.
func TestCanonicalize_MatrixMultiConnSort(t *testing.T) {
	p := freshPlugin()
	seedEntry(p, []int32{1}, "root", func(e *treeEntry) {
		e.glowNode = &glow.Node{Number: 1, Identifier: "root"}
	})
	seedEntry(p, []int32{1, 1}, "mat", func(e *treeEntry) {
		m := &glow.Matrix{
			Number: 1, Identifier: "mat", MatrixType: glow.MatrixTypeNToN,
			TargetCount: 4, SourceCount: 4,
			Connections: []glow.Connection{
				// Out-of-order targets + multi-source so both sorts run.
				{Target: 2, Sources: []int32{3, 1}},
				{Target: 0, Sources: []int32{2, 0}},
			},
		}
		e.glowMatrix = m
		e.matrixState = matrix.NewStateFromGlow(m)
	})
	if _, err := p.Canonicalize(context.Background(), CanonicalOptions{
		Labels: "pointer", Gain: "pointer", Templates: "pointer",
	}); err != nil {
		t.Fatalf("Canonicalize: %v", err)
	}
}

// TestCanonicalize_ChildSortTie covers the OID tie-break (109) in the
// child-sort comparator: two siblings with the SAME Number sort by OID.
func TestCanonicalize_ChildSortTie(t *testing.T) {
	p := freshPlugin()
	seedEntry(p, []int32{1}, "root", func(e *treeEntry) {
		e.glowNode = &glow.Node{Number: 1, Identifier: "root"}
	})
	// Two children under root with the SAME canonical Number (obj.ID) but
	// different OIDs (1.1 vs 1.2) → the comparator's Number test ties and
	// falls through to the OID tie-break (109). canonical Number derives
	// from e.obj.ID, so force both to 7.
	seedEntry(p, []int32{1, 1}, "a", func(e *treeEntry) {
		e.obj.ID = 7
		e.glowParam = &glow.Parameter{Number: 7, Identifier: "a", Type: glow.ParamTypeInteger, Value: int64(1)}
	})
	seedEntry(p, []int32{1, 2}, "b", func(e *treeEntry) {
		e.obj.ID = 7
		e.glowParam = &glow.Parameter{Number: 7, Identifier: "b", Type: glow.ParamTypeInteger, Value: int64(2)}
	})
	if _, err := p.Canonicalize(context.Background(), CanonicalOptions{
		Labels: "pointer", Gain: "pointer", Templates: "pointer",
	}); err != nil {
		t.Fatalf("Canonicalize: %v", err)
	}
}

// TestProcessMatrix_ChildrenRecurse covers the matrix child-recurse loop:
// a matrix carrying a child element.
func TestProcessMatrix_ChildrenRecurse(t *testing.T) {
	p := freshPlugin()
	p.handleElements([]glow.Element{
		{Matrix: &glow.Matrix{
			Number: 1, Identifier: "m", MatrixType: glow.MatrixTypeNToN,
			TargetCount: 2, SourceCount: 2,
			Connections: []glow.Connection{{Target: 0, Sources: []int32{0}}}, // non-empty → no GetDirectory
			Children: []glow.Element{
				{Parameter: &glow.Parameter{Number: 5, Identifier: "label", Type: glow.ParamTypeInteger, Value: int64(1)}},
			},
		}},
	})
	if _, ok := p.numIndex["1.5"]; !ok {
		t.Error("matrix child parameter not indexed at 1.5")
	}
}

// TestMatrixConnect_CanConnectFails covers the matrixState.CanConnect
// failure arm: a oneToOne matrix rejects a 2-source connection on a
// target (cardinality-1 invariant, spec p.33).
func TestMatrixConnect_CanConnectFails(t *testing.T) {
	p := freshPlugin()
	p.session = deadSession()
	m := &glow.Matrix{
		Number: 1, Identifier: "mtx", Path: []int32{1, 1},
		MatrixType: glow.MatrixTypeOneToOne, TargetCount: 4, SourceCount: 4,
	}
	e := &treeEntry{
		numericPath: []int32{1, 1},
		glowMatrix:  m,
		matrixState: matrix.NewStateFromGlow(m),
		obj:         consumer.Object{OID: "1.1"},
	}
	p.numIndex["1.1"] = e
	p.pathIndex["mtx"] = e
	// 2 sources on one target violates oneToOne cardinality → CanConnect err.
	if err := p.MatrixConnect(context.Background(), "mtx", 0, []int32{1, 2}, glow.ConnOpAbsolute); err == nil {
		t.Error("want CanConnect validation error")
	}
}

// TestSubscribe_PathResolvesButNoPath covers the subscribePath-empty
// error arm: an entry with a glowParam but neither glowParam.Path nor
// numericPath, so no Command-30 target can be formed.
func TestSubscribe_NoPath(t *testing.T) {
	p := freshPlugin()
	p.session = deadSession()
	e := &treeEntry{
		glowParam: &glow.Parameter{Type: glow.ParamTypeInteger}, // empty Path
		obj:       consumer.Object{OID: "1.1"},
	}
	p.numIndex["1.1"] = e
	p.pathIndex["np"] = e
	if err := p.Subscribe(consumer.ValueRequest{Path: "np"}, func(consumer.Event) {}); err == nil {
		t.Error("want 'parameter not found for subscribe' (no path) error")
	}
}

// TestUnsubscribe_StreamButNoWirePath covers the arm where a sub was a
// stream sub but glowParam.Path is empty → no Command 31, returns nil.
func TestUnsubscribe_StreamButNoWirePath(t *testing.T) {
	p := freshPlugin()
	p.session = deadSession()
	e := &treeEntry{
		numericPath: []int32{1, 1},
		glowParam:   &glow.Parameter{Type: glow.ParamTypeInteger}, // empty wire Path
		obj:         consumer.Object{OID: "1.1"},
	}
	p.numIndex["1.1"] = e
	p.pathIndex["str"] = e
	p.subsMu.Lock()
	p.subs["1.1"] = func(consumer.Event) {}
	p.streamSubs["1.1"] = []int32{1, 1}
	p.subsMu.Unlock()
	if err := p.Unsubscribe(consumer.ValueRequest{Path: "str", ID: -1}); err != nil {
		t.Errorf("stream-sub with empty wire path should return nil: %v", err)
	}
}

// TestEmitRootSessionEvent_NoTreeElse covers the else branch (Label =
// "session") when a wildcard subscriber exists but the tree is empty.
func TestEmitRootSessionEvent_NoTreeElse(t *testing.T) {
	p := freshPlugin()
	got := make(chan consumer.Event, 1)
	p.subs["*"] = func(e consumer.Event) {
		select {
		case got <- e:
		default:
		}
	}
	// Empty numIndex → no rootEntry → else branch sets Label "session".
	p.emitRootSessionEvent(false, "boom")
	select {
	case e := <-got:
		if e.Label != "session" {
			t.Errorf("empty-tree event Label = %q, want session", e.Label)
		}
	case <-time.After(time.Second):
		t.Fatal("no event emitted")
	}
}

// TestResolveDtdVersion_ProbeSuccess covers the live-probe success arm
// (527-530): DtdVersion() is empty on entry, SendGetDirectory succeeds
// over the live pipe, and a concurrently-latched DTD unblocks
// WaitForDtdVersion with a non-empty version.
func TestResolveDtdVersion_ProbeSuccess(t *testing.T) {
	s, cleanup := pipeSession(t)
	defer cleanup()
	p := freshPlugin()
	p.session = s
	// Latch the DTD shortly after the probe send so DtdVersion() is empty
	// on entry (forcing the probe path) but WaitForDtdVersion resolves.
	go func() {
		time.Sleep(20 * time.Millisecond)
		s.noteDtd(0x3C, 0x02) // 60 . 2 → "2.60"
	}()
	if got := p.resolveDtdVersion(context.Background()); got != "2.60" {
		t.Errorf("probe DTD = %q, want 2.60", got)
	}
}

// TestResolveDtdVersion_ProbeTimeoutNoFallback covers the SendGetDirectory-
// succeeds-but-no-DTD arm plus the final return "" (543): the probe sends
// fine over the pipe, WaitForDtdVersion times out (no noteDtd), and there
// is no identity.dtdVersion entry to fall back on.
func TestResolveDtdVersion_ProbeTimeoutNoFallback(t *testing.T) {
	s, cleanup := pipeSession(t)
	defer cleanup()
	p := freshPlugin()
	p.session = s
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if got := p.resolveDtdVersion(ctx); got != "" {
		t.Errorf("no DTD + no fallback → want empty, got %q", got)
	}
}

// TestWalk_CtxCancelMidSettle covers Walk's ctx.Done arm inside the
// settle loop: a live session (so the GetDirectory send succeeds) with a
// context cancelled right after the call begins.
func TestWalk_CtxCancelMidSettle(t *testing.T) {
	s, cleanup := pipeSession(t)
	defer cleanup()
	p := freshPlugin()
	p.session = s
	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after Walk's initial 500ms sleep but during the settle loop.
	go func() {
		time.Sleep(600 * time.Millisecond)
		cancel()
	}()
	if _, err := p.Walk(ctx, 0); err == nil {
		t.Error("Walk should return ctx error when cancelled mid-settle")
	}
}

// TestSessionDisconnect_NilConn covers Session.Disconnect's conn==nil
// return-nil arm.
func TestSessionDisconnect_NilConn(t *testing.T) {
	s := NewSession(discardLogger())
	s.SetProfile(&compliance.Profile{})
	// closed=false, conn=nil → Disconnect closes the done chans (nil here)
	// and returns nil via the conn==nil fall-through.
	if err := s.Disconnect(); err != nil {
		t.Errorf("Disconnect with nil conn = %v, want nil", err)
	}
}

// TestIdentityProbe_Layer3NoProduct covers findIdentityNode's layer-3
// continue: a Node whose direct children have no product candidate.
func TestIdentityProbe_Layer3NoProduct(t *testing.T) {
	p := freshPlugin()
	// A node with children that are NOT product/version candidates →
	// layer 3 finds no product → continue → returns nil overall.
	seedEntry(p, []int32{1}, "root", func(e *treeEntry) {
		e.glowNode = &glow.Node{Number: 1, Identifier: "root"}
	})
	seedEntry(p, []int32{1, 1}, "random", func(e *treeEntry) {
		e.glowParam = &glow.Parameter{Number: 1, Identifier: "random", Type: glow.ParamTypeInteger, Value: int64(1)}
	})
	if got := p.findIdentityNode(); got != nil {
		t.Errorf("findIdentityNode with no identity-shaped node = %+v, want nil", got)
	}
}

// TestResolver_ConnectionParamNonParamChild covers the non-Parameter
// child skip (464) in extractConnectionParamMap: a connection-param node
// whose child is a Node rather than a Parameter.
func TestResolver_ConnectionParamNonParamChild(t *testing.T) {
	// target node → source node → a Node child (not a Parameter) → skipped.
	srcNode := &canonical.Node{Header: canonical.Header{Number: 0, OID: "x",
		Children: []canonical.Element{
			&canonical.Node{Header: canonical.Header{Number: 9, OID: "y"}}, // non-Parameter → continue
		}}}
	tgtNode := &canonical.Node{Header: canonical.Header{Number: 0, OID: "t",
		Children: []canonical.Element{srcNode}}}
	root := &canonical.Node{Header: canonical.Header{Number: 0, OID: "c",
		Children: []canonical.Element{tgtNode}}}
	out := extractConnectionParamMap(root)
	if len(out) != 0 {
		t.Errorf("connection-param map with only non-Parameter leaves = %v, want empty", out)
	}
}

// TestSplitAndPersistDM_WriteError covers the WriteDM-failure arm in the
// slot loop: a TreeStore rooted at a path that is actually a FILE makes
// every WriteDM fail (cannot create a subdirectory under a file).
func TestSplitAndPersistDM_WriteError(t *testing.T) {
	dir := t.TempDir()
	notADir := filepath.Join(dir, "blocker")
	if err := os.WriteFile(notADir, []byte("x"), 0o644); err != nil {
		t.Fatalf("seed blocker file: %v", err)
	}
	store := datastore.NewTreeStore(notADir) // baseDir is a file → MkdirAll fails

	p := freshPlugin()
	seedEntry(p, []int32{1}, "root", func(e *treeEntry) {
		e.glowNode = &glow.Node{Number: 1, Identifier: "root"}
	})
	seedEntry(p, []int32{1, 1}, "oneToN", func(e *treeEntry) {
		e.glowNode = &glow.Node{Number: 1, Identifier: "oneToN"}
	})
	seedEntry(p, []int32{1, 1, 1}, "mat", func(e *treeEntry) {
		m := &glow.Matrix{Number: 1, Identifier: "mat", MatrixType: glow.MatrixTypeOneToN, TargetCount: 1, SourceCount: 1}
		e.glowMatrix = m
		e.matrixState = matrix.NewStateFromGlow(m)
	})
	if _, err := p.SplitAndPersistDM(context.Background(), store, "Router@1.0"); err == nil {
		t.Error("want WriteDM error when baseDir is a file")
	}
}

// TestSplitAndPersistDM_NonNodeRootChild covers the non-Node root-child
// skip: a root with a direct Parameter child (not a Node) alongside a
// matrix slot node.
func TestSplitAndPersistDM_NonNodeRootChild(t *testing.T) {
	store := datastore.NewTreeStore(t.TempDir())
	p := freshPlugin()
	seedEntry(p, []int32{1}, "root", func(e *treeEntry) {
		e.glowNode = &glow.Node{Number: 1, Identifier: "root"}
	})
	// Direct Parameter child of root (1.2) → not a *canonical.Node → skipped.
	seedEntry(p, []int32{1, 2}, "directparam", func(e *treeEntry) {
		e.glowParam = &glow.Parameter{Number: 2, Identifier: "directparam", Type: glow.ParamTypeInteger, Value: int64(1)}
	})
	// A real matrix slot so the loop also has something to persist.
	seedEntry(p, []int32{1, 1}, "oneToN", func(e *treeEntry) {
		e.glowNode = &glow.Node{Number: 1, Identifier: "oneToN"}
	})
	seedEntry(p, []int32{1, 1, 1}, "mat", func(e *treeEntry) {
		m := &glow.Matrix{Number: 1, Identifier: "mat", MatrixType: glow.MatrixTypeOneToN, TargetCount: 1, SourceCount: 1}
		e.glowMatrix = m
		e.matrixState = matrix.NewStateFromGlow(m)
	})
	if _, err := p.SplitAndPersistDM(context.Background(), store, "Router@1.0"); err != nil {
		t.Fatalf("SplitAndPersistDM: %v", err)
	}
}

// TestSplitAndPersistDM_MatrixRootWriteError covers the WriteDM-failure
// arm inside the degenerate matrix-at-root path (66-68): a lone Matrix at
// root + a TreeStore whose baseDir is a file so WriteDM fails.
func TestSplitAndPersistDM_MatrixRootWriteError(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("seed blocker: %v", err)
	}
	store := datastore.NewTreeStore(blocker) // file as baseDir → WriteDM fails

	p := freshPlugin()
	seedEntry(p, []int32{1}, "soloMatrix", func(e *treeEntry) {
		m := &glow.Matrix{Number: 1, Identifier: "soloMatrix",
			MatrixType: glow.MatrixTypeNToN, TargetCount: 1, SourceCount: 1}
		e.glowMatrix = m
		e.matrixState = matrix.NewStateFromGlow(m)
	})
	if _, err := p.SplitAndPersistDM(context.Background(), store, "Solo@1.0"); err == nil {
		t.Error("want WriteDM error for matrix-at-root with file baseDir")
	}
}

// TestProcessParameter_StreamIDReannounceSamePath covers the
// isNewPath==false arm of the streamId-collision check: re-announcing the
// SAME path with the same streamId must NOT re-fire the compliance event.
func TestProcessParameter_StreamIDReannounceSamePath(t *testing.T) {
	p := freshPlugin()
	mk := func() glow.Element {
		return glow.Element{Parameter: &glow.Parameter{Number: 1, Identifier: "a",
			Type: glow.ParamTypeInteger, HasStreamIdentifier: true, StreamIdentifier: 5}}
	}
	p.handleElements([]glow.Element{mk()})
	before := p.profile.Snapshot()[StreamIDCollisionNoDescriptor]
	// Re-announce the SAME parameter (key "1") → isNewPath false → no Note.
	p.handleElements([]glow.Element{mk()})
	after := p.profile.Snapshot()[StreamIDCollisionNoDescriptor]
	if after != before {
		t.Errorf("re-announce of same stream path must not re-fire collision: %d → %d", before, after)
	}
}

// TestProcessParameter_RestoreKindUnknown covers the inner Kind-restore
// arm (1892-1894): a description-only announce on an entry whose cached
// obj.Value has a known Kind but whose merged glowParam yields Kind
// Unknown — so obj.Kind is back-filled from the existing value's Kind.
func TestProcessParameter_RestoreKindUnknown(t *testing.T) {
	p := freshPlugin()
	// Seed an entry with a typed cached value but a TYPELESS glowParam
	// (Type 0 → paramKindFrom yields Unknown on re-announce).
	p.numIndex["1"] = &treeEntry{
		numericPath: []int32{1},
		glowParam:   &glow.Parameter{Number: 1, Identifier: "v"}, // Type unset
		obj:         consumer.Object{OID: "1", Kind: consumer.KindInt, Value: consumer.Value{Kind: consumer.KindInt, Int: 99}},
	}
	// Description-only announce (no Value, no Type) for the same path.
	p.handleElements([]glow.Element{
		{Parameter: &glow.Parameter{Number: 1, Identifier: "v", Description: "changed"}},
	})
	e := p.numIndex["1"]
	if e.obj.Value.Kind != consumer.KindInt || e.obj.Value.Int != 99 {
		t.Errorf("restore arm: value=%+v, want Int 99", e.obj.Value)
	}
	if e.obj.Kind != consumer.KindInt {
		t.Errorf("restore arm: obj.Kind=%v, want KindInt back-filled", e.obj.Kind)
	}
}

// TestReadLoop_TopClosedReturn covers readLoop's top-of-loop closed
// guard (497): the session is marked closed before readLoop checks, so it
// returns immediately without reading.
func TestReadLoop_TopClosedReturn(t *testing.T) {
	srvConn, cliConn := net.Pipe()
	s := NewSession(discardLogger())
	s.SetProfile(&compliance.Profile{})
	s.mu.Lock()
	s.conn = srvConn
	s.reader = s101.NewReader(srvConn)
	s.writer = s101.NewWriter(srvConn)
	s.closed = true // already closed → readLoop's top guard returns at once
	s.mu.Unlock()
	defer func() { _ = srvConn.Close(); _ = cliConn.Close() }()

	done := make(chan struct{})
	go func() { s.readLoop(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("readLoop did not return immediately on a closed session")
	}
}

// TestReadLoop_KeepAliveRespWriteError covers the keep-alive response
// write-failure arm (525-527): a keepalive request arrives, but the
// connection is torn down so writing the response errors.
func TestReadLoop_KeepAliveRespWriteError(t *testing.T) {
	srvConn, cliConn := net.Pipe()
	s := NewSession(discardLogger())
	s.SetProfile(&compliance.Profile{})
	s.mu.Lock()
	s.conn = srvConn
	s.reader = s101.NewReader(srvConn)
	s.writer = s101.NewWriter(srvConn)
	s.closed = false
	s.mu.Unlock()
	s.touchRX()

	go s.readLoop()

	// Send a keepalive request, then immediately close the client end so
	// the readLoop's response WriteFrame fails (broken pipe). Because
	// net.Pipe writes block until a read, closing cliConn unblocks the
	// pending response write with an error.
	w := s101.NewWriter(cliConn)
	go func() {
		_ = w.WriteFrame(s101.NewKeepAliveRequest())
	}()
	time.Sleep(30 * time.Millisecond)
	_ = cliConn.Close() // response write now errors
	time.Sleep(50 * time.Millisecond)
	_ = s.Disconnect()
}

// TestReconnectLoop_WithRecorder covers the recorder-install arm inside
// reconnectLoop: a plugin with a recorder set, dialing a dead port so the
// loop builds a fresh session (installing the recorder) then gives up.
func TestReconnectLoop_WithRecorder(t *testing.T) {
	p := fastWalk((&Factory{}).New(discardLogger()).(*Plugin))
	p.connIP = "127.0.0.1"
	p.connPort = 1 // nothing listens → dial fails, loop builds the session first
	rec, err := newTempRecorder(t)
	if err != nil {
		t.Fatalf("recorder: %v", err)
	}
	defer func() { _ = rec.Close() }()
	p.SetRecorder(rec)
	p.reconnectPolicyOverride = &reconnectPolicy{
		InitialBackoff: time.Millisecond,
		MaxBackoff:     time.Millisecond,
		MaxAttempts:    1,
	}
	done := make(chan struct{})
	go func() { p.reconnectLoop(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("reconnectLoop did not give up")
	}
}
