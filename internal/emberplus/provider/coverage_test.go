package emberplus

import (
	"context"
	"net"
	"testing"
	"time"

	"dhs/internal/emberplus/codec/glow"
	"dhs/internal/emberplus/codec/s101"
	"dhs/internal/export/canonical"
)

// --- rich tree builder -------------------------------------------------

func strp(s string) *string { return &s }
func i64p(v int64) *int64   { return &v }

// buildRichExport assembles a tree that touches every encoder branch:
// a root Node with description/schema/template-ref, parameters of each
// canonical type (with min/max/step/default/format/enum/factor/stream),
// an nToN Matrix with targets/sources/labels/caps/connections, and a
// Function with arguments + result. Plus a top-level Template entry so
// the QualifiedTemplate + non-qual template-element encoders run.
func buildRichExport() *canonical.Export {
	intParam := &canonical.Parameter{
		Header: canonical.Header{
			Number: 1, Identifier: "count", Path: "root.count", OID: "1.1",
			Description: strp("a count"), IsOnline: true,
			Access: canonical.AccessReadWrite, Children: canonical.EmptyChildren(),
		},
		Type: canonical.ParamInteger, Value: int64(5),
		Minimum: int64(0), Maximum: int64(100), Step: int64(1), Default: int64(0),
		Factor: i64p(10), Format: strp("%d u"),
	}
	enumParam := &canonical.Parameter{
		Header: canonical.Header{
			Number: 2, Identifier: "mode", Path: "root.mode", OID: "1.2",
			IsOnline: true, Access: canonical.AccessReadWrite, Children: canonical.EmptyChildren(),
		},
		Type: canonical.ParamEnum, Value: int64(1),
		Enumeration: strp("off\non"),
		EnumMap:     []canonical.EnumEntry{{Key: "off", Value: 0}, {Key: "on", Value: 1}},
	}
	strParam := &canonical.Parameter{
		Header: canonical.Header{
			Number: 3, Identifier: "name", Path: "root.name", OID: "1.3",
			IsOnline: false, Access: canonical.AccessRead, Children: canonical.EmptyChildren(),
		},
		Type: canonical.ParamString, Value: "hello",
		Formula: strp("x|y"),
	}
	boolParam := &canonical.Parameter{
		Header: canonical.Header{
			Number: 4, Identifier: "on", Path: "root.on", OID: "1.4",
			IsOnline: true, Access: canonical.AccessWrite, Children: canonical.EmptyChildren(),
		},
		Type: canonical.ParamBoolean, Value: true,
	}
	realParam := &canonical.Parameter{
		Header: canonical.Header{
			Number: 5, Identifier: "gain", Path: "root.gain", OID: "1.5",
			IsOnline: true, Access: canonical.AccessReadWrite, Children: canonical.EmptyChildren(),
		},
		Type: canonical.ParamReal, Value: float64(-3.0),
		StreamIdentifier:  i64p(7),
		StreamDescriptor:  &canonical.StreamDescriptor{Format: int(glow.StreamFmtFloat64LittleEndian), Offset: 0},
		SchemaIdentifiers: strp("sch"), TemplateReference: strp("9.1"),
	}
	octParam := &canonical.Parameter{
		Header: canonical.Header{
			Number: 6, Identifier: "blob", Path: "root.blob", OID: "1.6",
			IsOnline: true, Access: canonical.AccessReadWrite, Children: canonical.EmptyChildren(),
		},
		Type: canonical.ParamOctets, Value: []byte{0x01, 0x02},
	}
	mtx := &canonical.Matrix{
		Header: canonical.Header{
			Number: 7, Identifier: "mat", Path: "root.mat", OID: "1.7",
			IsOnline: true, Access: canonical.AccessReadWrite, Children: canonical.EmptyChildren(),
		},
		Type: canonical.MatrixNToN, Mode: canonical.ModeNonLinear,
		TargetCount: 2, SourceCount: 2,
		MaximumTotalConnects:     i64p(8),
		MaximumConnectsPerTarget: i64p(2),
		GainParameterNumber:      i64p(3),
		ParametersLocation:       strp("1.7.99"),
		SchemaIdentifiers:        strp("msch"),
		TemplateReference:        strp("9.2"),
		Targets:                  []canonical.MatrixTarget{{Number: 0}, {Number: 1}},
		Sources:                  []canonical.MatrixSource{{Number: 0}, {Number: 1}},
		Labels:                   []canonical.MatrixLabel{{BasePath: "1.7.1", Description: strp("lbl")}},
		Connections: []canonical.MatrixConnection{
			{Target: 0, Sources: []int64{0}, Operation: canonical.ConnOpAbsolute, Disposition: canonical.ConnDispTally},
		},
	}
	fn := &canonical.Function{
		Header: canonical.Header{
			Number: 8, Identifier: "sum", Path: "root.sum", OID: "1.8",
			Description: strp("adds"), IsOnline: true,
			Access: canonical.AccessRead, Children: canonical.EmptyChildren(),
		},
		Arguments: []canonical.TupleItem{{Name: "a", Type: canonical.ParamInteger}, {Name: "b", Type: canonical.ParamInteger}},
		Result:    []canonical.TupleItem{{Name: "sum", Type: canonical.ParamInteger}},
	}
	root := &canonical.Node{
		Header: canonical.Header{
			Number: 1, Identifier: "root", Path: "root", OID: "1",
			Description: strp("the root"), IsOnline: true,
			Access:   canonical.AccessRead,
			Children: []canonical.Element{intParam, enumParam, strParam, boolParam, realParam, octParam, mtx, fn},
		},
		SchemaIdentifiers: strp("rootsch"), TemplateReference: strp("9.3"),
	}
	// Template entries exercising every non-qual element encoder.
	tmplNode := &canonical.Node{Header: canonical.Header{Number: 1, Identifier: "tn", OID: "9.1", IsOnline: true, Children: canonical.EmptyChildren()}}
	tmplParam := &canonical.Parameter{Header: canonical.Header{Number: 1, Identifier: "tp", OID: "9.2", IsOnline: true, Children: canonical.EmptyChildren()}, Type: canonical.ParamInteger}
	tmplMtx := &canonical.Matrix{Header: canonical.Header{Number: 1, Identifier: "tm", OID: "9.3", IsOnline: true, Children: canonical.EmptyChildren()}, Type: canonical.MatrixOneToN, TargetCount: 1, SourceCount: 1}
	tmplFn := &canonical.Function{Header: canonical.Header{Number: 1, Identifier: "tf", OID: "9.4", IsOnline: true, Children: canonical.EmptyChildren()}}
	return &canonical.Export{
		Root: root,
		Templates: []*canonical.TemplateEntry{
			{Number: 1, OID: "9.1", Identifier: "tplNode", Description: strp("d"), Template: tmplNode},
			{Number: 2, OID: "9.2", Identifier: "tplParam", Template: tmplParam},
			{Number: 3, OID: "9.3", Identifier: "tplMtx", Template: tmplMtx},
			{Number: 4, OID: "9.4", Identifier: "tplFn", Template: tmplFn},
		},
	}
}

// --- loopback session driver ------------------------------------------

// drive runs a real session over an in-memory net.Pipe, sends every
// request payload as an S101 EmBER frame plus a keep-alive, then reads
// frames for a short window. Covers session.run / writePump / handleFrame
// / handleEmber / replyGetDirectory / handleInvoke and the server
// bookkeeping (register/drop/subscribe/unsubscribe).
func drive(t *testing.T, srv *server, requests [][]byte) {
	t.Helper()
	cliConn, srvConn := net.Pipe()
	sess := newSession(srv, srvConn)
	srv.registerSession(sess)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { sess.run(ctx); close(done) }()

	w := s101.NewWriter(cliConn)
	r := s101.NewReader(cliConn)

	// Reader goroutine drains replies so writePump never blocks.
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		_ = cliConn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		for {
			if _, err := r.ReadFrame(); err != nil {
				return
			}
		}
	}()

	// Keep-alive request first (covers CmdKeepAliveReq + response write).
	_ = w.WriteFrame(s101.NewKeepAliveRequest())
	// A keep-alive response from the peer (covers CmdKeepAliveResp branch).
	_ = w.WriteFrame(s101.NewKeepAliveResponse())
	for _, req := range requests {
		f := s101.NewEmBERFrame(req)
		_ = w.WriteFrame(f)
	}
	// Give the session time to process.
	time.Sleep(50 * time.Millisecond)
	cancel()
	_ = cliConn.Close()
	<-done
	<-readDone
}

// TestProvider_Loopback drives the full inbound dispatch surface.
func TestProvider_Loopback(t *testing.T) {
	srv := newServer(nil, buildRichExport())
	if srv.tree == nil {
		t.Fatal("tree build failed")
	}

	reqs := [][]byte{
		glow.EncodeGetDirectory(),                       // bare root GetDirectory
		glow.EncodeGetDirectoryFor([]int32{1}),          // GetDirectory on root node
		glow.EncodeGetDirectoryFor([]int32{1, 7}),       // GetDirectory on matrix (implicit subscribe)
		glow.EncodeSubscribe([]int32{1, 5}),             // subscribe stream param
		glow.EncodeUnsubscribe([]int32{1, 5}),           // unsubscribe
		glow.EncodeSetValue([]int32{1, 1}, int64(42)),   // set int param
		glow.EncodeMatrixConnect([]int32{1, 7}, 1, []int32{1}, glow.ConnOpConnect), // matrix connect
		glow.EncodeInvoke([]int32{1, 8}, 99, []any{int64(3), int64(4)}),            // invoke sum
		glow.EncodeGetDirectoryFor([]int32{4, 2}),       // unknown path → replyGetDirectory error
		glow.EncodeSetValue([]int32{9, 9, 9}, int64(1)), // set on missing oid → error path
	}
	drive(t, srv, reqs)

	// Verify the int param actually changed via the loopback SetValue.
	if got := srv.tree.byOID["1.1"].el.(*canonical.Parameter).Value; got != int64(42) {
		t.Errorf("loopback SetValue did not apply: got %v", got)
	}
}

// TestProvider_ServeStop covers Serve (real listener), Stop, and a live
// client round-trip over TCP, plus the idle sweeper and streamer goroutines.
func TestProvider_ServeStop(t *testing.T) {
	srv := newServer(nil, buildRichExport())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ctx, "127.0.0.1:0") }()

	// Wait for the listener to come up.
	var addr string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		srv.mu.Lock()
		ln := srv.listener
		srv.mu.Unlock()
		if ln != nil {
			addr = ln.Addr().String()
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if addr == "" {
		t.Fatal("listener never came up")
	}

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	w := s101.NewWriter(conn)
	r := s101.NewReader(conn)
	_ = w.WriteFrame(s101.NewEmBERFrame(glow.EncodeGetDirectory()))
	_ = conn.SetReadDeadline(time.Now().Add(1 * time.Second))
	if _, err := r.ReadFrame(); err != nil {
		t.Fatalf("read reply: %v", err)
	}
	// Drive a SetValue through the public API to fan out to the session.
	if _, err := srv.SetValue(ctx, "root.count", int64(7)); err != nil {
		t.Fatalf("SetValue: %v", err)
	}
	_ = conn.Close()

	// Exercise the idle sweeper directly (ttl=0 sweeps everything).
	srv.sweepIdleSessions(0)
	// Exercise the streamer fan-out directly.
	srv.fanoutStreams(srv.collectStreams())

	if err := srv.Stop(); err != nil {
		t.Errorf("Stop: %v", err)
	}
	cancel()
	select {
	case <-errc:
	case <-time.After(2 * time.Second):
		t.Error("Serve did not return after Stop")
	}
}

// TestServe_TreeNotLoaded covers the guard when the tree failed to build.
func TestServe_TreeNotLoaded(t *testing.T) {
	// nil export → newTree errors → s.tree stays nil.
	srv := newServer(nil, &canonical.Export{})
	if srv.tree != nil {
		t.Fatal("expected nil tree")
	}
	if err := srv.Serve(context.Background(), "127.0.0.1:0"); err == nil {
		t.Error("Serve with no tree should error")
	}
}

// TestServe_BadAddr covers the listen error path.
func TestServe_BadAddr(t *testing.T) {
	srv := newServer(nil, buildRichExport())
	if err := srv.Serve(context.Background(), "256.256.256.256:99999"); err == nil {
		t.Error("Serve on bad addr should error")
	}
}

// TestRunStreamer_NoStreams covers the early return when the tree has no
// stream parameters.
func TestRunStreamer_NoStreams(t *testing.T) {
	root := &canonical.Node{Header: canonical.Header{
		Number: 1, Identifier: "n", OID: "1", IsOnline: true,
		Children: canonical.EmptyChildren(),
	}}
	srv := newServer(nil, &canonical.Export{Root: root})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	srv.runStreamer(ctx, time.Millisecond) // returns immediately (no streams)
}

// TestRunStreamerAndSweeper_CtxCancel covers the ticker loops' ctx-cancel
// exit in runStreamer and runIdleSweeper.
func TestRunStreamerAndSweeper_CtxCancel(t *testing.T) {
	srv := newServer(nil, buildRichExport())
	ctx, cancel := context.WithCancel(context.Background())
	go srv.runStreamer(ctx, time.Millisecond)
	go srv.runIdleSweeper(ctx, time.Millisecond, time.Hour)
	time.Sleep(10 * time.Millisecond)
	cancel()
	time.Sleep(10 * time.Millisecond)
}

// TestFactory_New covers the plugin Factory.New constructor.
func TestFactory_New(t *testing.T) {
	f := &Factory{}
	p := f.New(nil, buildRichExport())
	if p == nil {
		t.Fatal("Factory.New returned nil")
	}
	if f.Meta().Name != "emberplus" {
		t.Errorf("Meta().Name = %q", f.Meta().Name)
	}
}

// --- pure encoder / helper table tests --------------------------------

// TestEncodeValue_AllTypes covers every encodeValue type arm plus the
// nil and type-mismatch skip branches.
func TestEncodeValue_AllTypes(t *testing.T) {
	cases := []struct {
		typ  string
		v    any
		want bool
	}{
		{canonical.ParamInteger, int64(1), true},
		{canonical.ParamEnum, int64(2), true},
		{canonical.ParamInteger, "nope", false},
		{canonical.ParamReal, float64(1.5), true},
		{canonical.ParamReal, "nope", false},
		{canonical.ParamString, "s", true},
		{canonical.ParamString, 1, false},
		{canonical.ParamBoolean, true, true},
		{canonical.ParamBoolean, 1, false},
		{canonical.ParamOctets, []byte{1}, true},
		{canonical.ParamOctets, "nope", false},
		{canonical.ParamTrigger, nil, false},  // nil short-circuit
		{"unknown-type", int64(1), false},     // unmapped type
	}
	for _, c := range cases {
		if _, ok := encodeValue(c.typ, c.v); ok != c.want {
			t.Errorf("encodeValue(%s,%v)=%v want %v", c.typ, c.v, ok, c.want)
		}
	}
}

// TestParamTypeConst covers every mapping arm + the unknown default.
func TestParamTypeConst(t *testing.T) {
	for _, typ := range []string{
		canonical.ParamInteger, canonical.ParamReal, canonical.ParamString,
		canonical.ParamBoolean, canonical.ParamTrigger, canonical.ParamEnum,
		canonical.ParamOctets,
	} {
		if _, ok := paramTypeConst(typ); !ok {
			t.Errorf("paramTypeConst(%s) not mapped", typ)
		}
	}
	if _, ok := paramTypeConst("bogus"); ok {
		t.Error("paramTypeConst(bogus) should be false")
	}
}

// TestAccessConst covers every access string + the default.
func TestAccessConst(t *testing.T) {
	cases := map[string]int64{
		canonical.AccessRead:      glow.AccessRead,
		canonical.AccessWrite:     glow.AccessWrite,
		canonical.AccessReadWrite: glow.AccessReadWrite,
		"bogus":                   glow.AccessNone,
	}
	for a, want := range cases {
		if got := accessConst(a); got != want {
			t.Errorf("accessConst(%q)=%d want %d", a, got, want)
		}
	}
}

// TestAsInt64AsFloat64 covers every numeric conversion arm + misses.
func TestAsInt64AsFloat64(t *testing.T) {
	for _, v := range []any{int(1), int32(2), int64(3), float64(4)} {
		if _, ok := asInt64(v); !ok {
			t.Errorf("asInt64(%T) failed", v)
		}
	}
	if _, ok := asInt64("x"); ok {
		t.Error("asInt64(string) should fail")
	}
	for _, v := range []any{float64(1), float32(2), int(3), int64(4)} {
		if _, ok := asFloat64(v); !ok {
			t.Errorf("asFloat64(%T) failed", v)
		}
	}
	if _, ok := asFloat64("x"); ok {
		t.Error("asFloat64(string) should fail")
	}
}

// TestMatrixConstHelpers covers matrixTypeConst / addressingModeConst /
// connOperationConst / connDispositionConst across all arms + defaults.
func TestMatrixConstHelpers(t *testing.T) {
	for _, ty := range []string{canonical.MatrixOneToN, "", canonical.MatrixOneToOne, canonical.MatrixNToN} {
		if _, ok := matrixTypeConst(ty); !ok {
			t.Errorf("matrixTypeConst(%q) not mapped", ty)
		}
	}
	if _, ok := matrixTypeConst("bogus"); ok {
		t.Error("matrixTypeConst(bogus) should be false")
	}
	if addressingModeConst(canonical.ModeNonLinear) != glow.MatrixAddrNonLinear {
		t.Error("addressingModeConst nonlinear")
	}
	if addressingModeConst("anything") != glow.MatrixAddrLinear {
		t.Error("addressingModeConst default linear")
	}
	if connOperationConst(canonical.ConnOpConnect) != glow.ConnOpConnect ||
		connOperationConst(canonical.ConnOpDisconnect) != glow.ConnOpDisconnect ||
		connOperationConst("x") != glow.ConnOpAbsolute {
		t.Error("connOperationConst arms")
	}
	if connDispositionConst(canonical.ConnDispModified) != glow.ConnDispModified ||
		connDispositionConst(canonical.ConnDispPending) != glow.ConnDispPending ||
		connDispositionConst(canonical.ConnDispLocked) != glow.ConnDispLocked ||
		connDispositionConst("x") != glow.ConnDispTally {
		t.Error("connDispositionConst arms")
	}
	// session.go name mappers (inverse direction).
	if connOperationName(glow.ConnOpConnect) != canonical.ConnOpConnect ||
		connOperationName(glow.ConnOpDisconnect) != canonical.ConnOpDisconnect ||
		connOperationName(glow.ConnOpAbsolute) != canonical.ConnOpAbsolute {
		t.Error("connOperationName arms")
	}
	if connDispositionName(glow.ConnDispModified) != canonical.ConnDispModified ||
		connDispositionName(glow.ConnDispPending) != canonical.ConnDispPending ||
		connDispositionName(glow.ConnDispLocked) != canonical.ConnDispLocked ||
		connDispositionName(glow.ConnDispTally) != canonical.ConnDispTally {
		t.Error("connDispositionName arms")
	}
}

// TestEncodeBase128_MultiByte covers the multi-byte continuation path.
func TestEncodeBase128_MultiByte(t *testing.T) {
	out := encodeBase128(300)
	if len(out) < 2 || out[0]&0x80 == 0 {
		t.Errorf("encodeBase128(300) = % x, want multi-byte with continuation", out)
	}
	if encodeBase128(5)[0] != 5 {
		t.Error("encodeBase128(5) single byte")
	}
}

// TestEncodeNonQual_ViaTemplates round-trips a tree whose templates use
// every non-qualified element encoder (Node/Parameter/Matrix/Function),
// driving encodeTemplateElement + encodeNonQual* through the root reply.
func TestEncodeNonQual_ViaTemplates(t *testing.T) {
	srv := newServer(nil, buildRichExport())
	reply, err := srv.encodeGetDirReply(srv.tree.rootEntry(), false)
	if err != nil {
		t.Fatalf("encode root reply: %v", err)
	}
	els, err := glow.DecodeRoot(reply)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	var tmplCount int
	for _, e := range els {
		if e.Template != nil {
			tmplCount++
		}
	}
	if tmplCount != 4 {
		t.Errorf("expected 4 templates in root reply, got %d", tmplCount)
	}
}

// TestEncodeElementMinimal_AllKinds drives the bareRoot reply for each
// element kind so encodeElementMinimal's Parameter/Matrix/Function/Node
// arms all run.
func TestEncodeElementMinimal_AllKinds(t *testing.T) {
	srv := newServer(nil, buildRichExport())
	for _, oid := range []string{"1", "1.1", "1.7", "1.8"} {
		e, ok := srv.tree.lookupOID(oid)
		if !ok {
			t.Fatalf("oid %s missing", oid)
		}
		payload, err := srv.encodeGetDirReply(e, true) // bareRoot=true
		if err != nil {
			t.Fatalf("encodeGetDirReply(%s): %v", oid, err)
		}
		if _, err := glow.DecodeRoot(payload); err != nil {
			t.Fatalf("decode(%s): %v", oid, err)
		}
	}
}

// TestEncodeTupleValue_AllTypes covers every encodeTupleValue arm plus
// the unsupported-type fallback, via encodeInvocationResult.
func TestEncodeTupleValue_AllTypes(t *testing.T) {
	srv := newServer(nil, buildRichExport())
	result := []any{nil, true, int(1), int32(2), int64(3), float32(1.5), float64(2.5), "s", []byte{1}, struct{}{}}
	payload := srv.encodeInvocationResult(1, true, result)
	if _, err := glow.DecodeRoot(payload); err != nil {
		t.Fatalf("decode invocation result: %v", err)
	}
	// success=false branch.
	if len(srv.encodeInvocationResult(2, false, nil)) == 0 {
		t.Error("empty failure result")
	}
}

// TestEncodeTupleItem_UnknownType covers the typeConst fallback to Null
// when a tuple item carries an unmapped type.
func TestEncodeTupleItem_UnknownType(t *testing.T) {
	ti := encodeTupleItem(canonical.TupleItem{Name: "", Type: "bogus"})
	// Name empty → name field omitted; type → ParamTypeNull.
	if len(ti.Children) != 1 {
		t.Errorf("expected only type field, got %d children", len(ti.Children))
	}
}

// TestApplyMatrixConnections_Branches drives invoke.go matrix logic:
// caps rejection, locked target, oneToOne source steal, disconnect.
func TestApplyMatrixConnections_Branches(t *testing.T) {
	// nToN with caps.
	nton := &canonical.Matrix{
		Type: canonical.MatrixNToN, MaximumConnectsPerTarget: i64p(1), MaximumTotalConnects: i64p(2),
		Connections: []canonical.MatrixConnection{{Target: 0, Sources: []int64{0}}},
	}
	srv := buildMatrixTree(t, nton)
	// Exceed per-target cap (target 0 → 2 sources, cap 1) → echo current tally.
	post, err := srv.applyMatrixConnections("1.1", []canonical.MatrixConnection{
		{Target: 0, Sources: []int64{0, 1}, Operation: canonical.ConnOpAbsolute},
	})
	if err != nil || len(post) != 1 || post[0].Disposition != canonical.ConnDispTally {
		t.Errorf("cap rejection: post=%+v err=%v", post, err)
	}
	// Connect that exceeds total cap.
	_, _ = srv.applyMatrixConnections("1.1", []canonical.MatrixConnection{
		{Target: 1, Sources: []int64{1}, Operation: canonical.ConnOpConnect},
		{Target: 2, Sources: []int64{0}, Operation: canonical.ConnOpConnect},
	})
	// Disconnect.
	_, _ = srv.applyMatrixConnections("1.1", []canonical.MatrixConnection{
		{Target: 0, Sources: []int64{0}, Operation: canonical.ConnOpDisconnect},
	})

	// oneToOne source steal.
	one := &canonical.Matrix{
		Type: canonical.MatrixOneToOne,
		Connections: []canonical.MatrixConnection{
			{Target: 0, Sources: []int64{5}},
			{Target: 1, Sources: []int64{6}},
		},
	}
	srv2 := buildMatrixTree(t, one)
	post2, err := srv2.applyMatrixConnections("1.1", []canonical.MatrixConnection{
		{Target: 1, Sources: []int64{5}, Operation: canonical.ConnOpConnect}, // steal src 5 from target 0
	})
	if err != nil {
		t.Fatalf("oneToOne steal: %v", err)
	}
	// Expect target 1 ← 5 plus target 0 emptied.
	if len(post2) < 2 {
		t.Errorf("oneToOne steal should emit loser tally, got %+v", post2)
	}

	// Locked target rejection.
	locked := &canonical.Matrix{
		Type: canonical.MatrixNToN,
		Connections: []canonical.MatrixConnection{
			{Target: 0, Sources: []int64{1}, Disposition: canonical.ConnDispLocked, Locked: true},
		},
	}
	srv3 := buildMatrixTree(t, locked)
	post3, err := srv3.applyMatrixConnections("1.1", []canonical.MatrixConnection{
		{Target: 0, Sources: []int64{2}, Operation: canonical.ConnOpAbsolute},
	})
	if err != nil || len(post3) != 1 || post3[0].Disposition != canonical.ConnDispLocked {
		t.Errorf("locked target: post=%+v err=%v", post3, err)
	}

	// Error paths: unknown oid + non-matrix oid.
	if _, err := srv.applyMatrixConnections("9.9.9", nil); err == nil {
		t.Error("applyMatrixConnections on missing oid should error")
	}
	if _, err := srv.applyMatrixConnections("1", nil); err == nil {
		t.Error("applyMatrixConnections on non-matrix oid should error")
	}
}

// TestInvokeFunction covers the registry lookup miss, the nil-funcs
// guard, the callback-error path, and a successful invoke.
func TestInvokeFunction(t *testing.T) {
	srv := buildMatrixTree(t, &canonical.Matrix{Type: canonical.MatrixNToN})
	// No function registered at this oid.
	if _, ok := srv.invokeFunction("1.1", nil); ok {
		t.Error("invoke on unregistered oid should fail")
	}
	srv.funcs.register("1.1", func(args []any) ([]any, error) { return []any{int64(1)}, nil })
	if r, ok := srv.invokeFunction("1.1", nil); !ok || len(r) != 1 {
		t.Errorf("invoke success: r=%v ok=%v", r, ok)
	}
	srv.funcs.register("1.1", func(args []any) ([]any, error) { return nil, context.Canceled })
	if _, ok := srv.invokeFunction("1.1", nil); ok {
		t.Error("invoke callback error should yield ok=false")
	}
	// nil funcs registry guard.
	srv.funcs = nil
	if _, ok := srv.invokeFunction("1.1", nil); ok {
		t.Error("invoke with nil registry should fail")
	}
}

// TestBuiltinSum_NonIntArgs covers the non-integer-args branch not in
// function_test.go's TestBuiltinSum.
func TestBuiltinSum_NonIntArgs(t *testing.T) {
	if _, err := builtinSum([]any{"a", "b"}); err == nil {
		t.Error("sum with non-int args should error")
	}
}

// TestSetupBuiltins_RegistersAndSeeds drives setupBuiltinFunctions across
// every recognised function identifier + the locked-connection pre-seed.
func TestSetupBuiltins_RegistersAndSeeds(t *testing.T) {
	mkFn := func(num int, id, oid string) *canonical.Function {
		return &canonical.Function{Header: canonical.Header{
			Number: num, Identifier: id, Path: "root." + id, OID: oid,
			IsOnline: true, Children: canonical.EmptyChildren(),
		}}
	}
	mtx := &canonical.Matrix{
		Header: canonical.Header{Number: 9, Identifier: "m", Path: "root.m", OID: "1.9", IsOnline: true, Children: canonical.EmptyChildren()},
		Type:   canonical.MatrixNToN, TargetCount: 2, SourceCount: 2,
		Connections: []canonical.MatrixConnection{{Target: 0, Sources: []int64{1}, Locked: true}},
	}
	root := &canonical.Node{Header: canonical.Header{
		Number: 1, Identifier: "root", Path: "root", OID: "1", IsOnline: true,
		Access: canonical.AccessRead,
		Children: []canonical.Element{
			mkFn(1, "sum", "1.1"), mkFn(2, "recallSalvo", "1.2"), mkFn(3, "storeSalvo", "1.3"),
			mkFn(4, "listSalvos", "1.4"), mkFn(5, "getSalvo", "1.5"), mkFn(6, "setLock", "1.6"),
			mkFn(7, "listLocks", "1.7"), mtx,
		},
	}}
	srv := newServer(nil, &canonical.Export{Root: root})
	// Each builtin should now be registered.
	for _, oid := range []string{"1.1", "1.2", "1.3", "1.4", "1.5", "1.6", "1.7"} {
		if _, ok := srv.funcs.lookup(oid); !ok {
			t.Errorf("builtin at %s not registered", oid)
		}
	}
	// The locked seed should have pre-locked target 0 on the matrix.
	if !srv.locks.isLocked("1.9", 0) {
		t.Error("locked connection not pre-seeded into lockStore")
	}
}

// TestRecallStoreGetSalvo drives the salvo builtins end to end including
// the recall-applies-and-broadcasts path and getSalvo formatting.
func TestRecallStoreGetSalvo(t *testing.T) {
	mtx := &canonical.Matrix{
		Type: canonical.MatrixNToN, TargetCount: 4, SourceCount: 4,
		Connections: []canonical.MatrixConnection{
			{Target: 0, Sources: []int64{1, 2}},
			{Target: 5, Sources: []int64{3}},
			{Target: 7, Sources: []int64{}},
		},
	}
	srv := buildMatrixTree(t, mtx)
	store := srv.makeBuiltinStoreSalvo()
	if r, err := store([]any{"1.1", int64(1), ""}); err != nil || r[0] != true {
		t.Fatalf("store all: r=%v err=%v", r, err)
	}
	// store with a target filter that matches nothing → false.
	if r, err := store([]any{"1.1", int64(2), "999"}); err != nil || r[0] != false {
		t.Errorf("store filtered-empty: r=%v err=%v", r, err)
	}
	// getSalvo formats the stored snapshot.
	get := srv.makeBuiltinGetSalvo()
	if r, err := get([]any{"1.1", int64(1)}); err != nil || r[0].(string) == "" {
		t.Errorf("getSalvo: r=%v err=%v", r, err)
	}
	// getSalvo on unknown salvo → empty string.
	if r, err := get([]any{"1.1", int64(99)}); err != nil || r[0].(string) != "" {
		t.Errorf("getSalvo unknown: r=%v err=%v", r, err)
	}
	// recall applies the salvo.
	recall := srv.makeBuiltinRecallSalvo()
	if r, err := recall([]any{"1.1", int64(1)}); err != nil || r[0].(int64) == 0 {
		t.Errorf("recall: r=%v err=%v", r, err)
	}
	// recall unknown salvo → 0.
	if r, err := recall([]any{"1.1", int64(99)}); err != nil || r[0].(int64) != 0 {
		t.Errorf("recall unknown: r=%v err=%v", r, err)
	}
	// list salvos.
	list := srv.makeBuiltinListSalvos()
	if r, err := list([]any{"1.1"}); err != nil || r[0].(string) == "" {
		t.Errorf("listSalvos: r=%v err=%v", r, err)
	}
	// arg-count guards.
	if _, err := store([]any{"1.1"}); err == nil {
		t.Error("store needs 2 args")
	}
	if _, err := recall([]any{"1.1"}); err == nil {
		t.Error("recall needs 2 args")
	}
	if _, err := get([]any{"1.1"}); err == nil {
		t.Error("get needs 2 args")
	}
	if _, err := list(nil); err == nil {
		t.Error("list needs 1 arg")
	}
}

// TestSetLockListLocks drives the lock builtins including toggle + list.
func TestSetLockListLocks(t *testing.T) {
	srv := buildMatrixTree(t, &canonical.Matrix{Type: canonical.MatrixNToN, TargetCount: 4, SourceCount: 4})
	setLock := srv.makeBuiltinSetLock()
	if r, err := setLock([]any{"1.1", int64(2), true}); err != nil || r[0] != false {
		t.Errorf("setLock on: r=%v err=%v", r, err)
	}
	if r, err := setLock([]any{"1.1", int64(2), false}); err != nil || r[0] != true {
		t.Errorf("setLock off (prev locked): r=%v err=%v", r, err)
	}
	listLocks := srv.makeBuiltinListLocks()
	if _, err := listLocks([]any{"1.1"}); err != nil {
		t.Errorf("listLocks: %v", err)
	}
	// arg guards + bad types.
	if _, err := setLock([]any{"1.1"}); err == nil {
		t.Error("setLock needs 3 args")
	}
	if _, err := setLock([]any{1, 2, 3}); err == nil {
		t.Error("setLock bad arg types should error")
	}
	if _, err := listLocks(nil); err == nil {
		t.Error("listLocks needs 1 arg")
	}
	if _, err := listLocks([]any{1}); err == nil {
		t.Error("listLocks bad arg type should error")
	}
}

// TestFilterConnectionsByTargets covers the parse / empty / no-match arms.
func TestFilterConnectionsByTargets(t *testing.T) {
	conns := []canonical.MatrixConnection{{Target: 0}, {Target: 2}, {Target: 5}}
	if got := filterConnectionsByTargets(conns, ""); len(got) != 3 {
		t.Errorf("empty filter returns all: %d", len(got))
	}
	if got := filterConnectionsByTargets(conns, "0;2"); len(got) != 2 {
		t.Errorf("0;2 filter: %d", len(got))
	}
	if got := filterConnectionsByTargets(conns, "xyz"); got != nil {
		t.Errorf("no-digit filter should return nil, got %v", got)
	}
}

// TestSetValue_Errors covers tree.setParamValue + server.SetValue error
// branches: missing oid, non-parameter oid, bad oid segment.
func TestSetValue_Errors(t *testing.T) {
	srv := newServer(nil, buildRichExport())
	if _, err := srv.SetValue(context.Background(), "9.9.9", int64(1)); err == nil {
		t.Error("SetValue on missing oid should error")
	}
	if _, err := srv.tree.setParamValue("1.7", int64(1)); err == nil {
		t.Error("setParamValue on matrix oid should error")
	}
	// parseOID rejects a non-numeric segment.
	if _, err := parseOID("1.x.3"); err == nil {
		t.Error("parseOID bad segment should error")
	}
	if got, _ := parseOID(""); got != nil {
		t.Errorf("parseOID empty should be nil, got %v", got)
	}
}

// TestNewTree_Errors covers nil export, duplicate OID, and bad OID.
func TestNewTree_Errors(t *testing.T) {
	if _, err := newTree(nil); err == nil {
		t.Error("newTree(nil) should error")
	}
	dup := &canonical.Node{Header: canonical.Header{
		Number: 1, Identifier: "a", OID: "1", IsOnline: true,
		Children: []canonical.Element{
			&canonical.Parameter{Header: canonical.Header{Number: 1, Identifier: "b", OID: "1", IsOnline: true, Children: canonical.EmptyChildren()}, Type: canonical.ParamInteger},
		},
	}}
	if _, err := newTree(&canonical.Export{Root: dup}); err == nil {
		t.Error("duplicate OID should error")
	}
	bad := &canonical.Node{Header: canonical.Header{Number: 1, Identifier: "a", OID: "1.x", IsOnline: true, Children: canonical.EmptyChildren()}}
	if _, err := newTree(&canonical.Export{Root: bad}); err == nil {
		t.Error("bad OID should error")
	}
}
