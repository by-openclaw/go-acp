package emberplus

import (
	"bytes"
	"context"
	"dhs/internal/plugin"
	"net"
	"testing"
	"time"

	"dhs/internal/emberplus/codec/ber"
	"dhs/internal/emberplus/codec/glow"
	"dhs/internal/emberplus/codec/s101"
	"dhs/internal/export/canonical"
)

// TestStreamTick_AllKinds covers streamTick across real/integer/boolean/
// default value kinds plus the empty-entries early return.
func TestStreamTick_AllKinds(t *testing.T) {
	if streamTick(nil) != nil {
		t.Error("streamTick(nil) should return nil")
	}
	entries := []streamEntry{
		{id: 1, kind: canonical.ParamReal, min: 0, max: 1},
		{id: 2, kind: canonical.ParamInteger, min: 0, max: 10},
		{id: 3, kind: canonical.ParamBoolean},
		{id: 4, kind: "other", min: 0, max: 1}, // default arm
	}
	payload := streamTick(entries)
	els, err := glow.DecodeRoot(payload)
	if err != nil {
		t.Fatalf("decode stream: %v", err)
	}
	if len(els) != 1 || len(els[0].Streams) != 4 {
		t.Fatalf("expected 4 stream entries, got %+v", els)
	}
}

// TestFanoutStreams_Subscribed drives fanoutStreams with a session that
// is subscribed to a stream parameter, covering the want-list build and
// per-session send.
func TestFanoutStreams_Subscribed(t *testing.T) {
	srv := newServer(plugin.Deps{}, buildRichExport())
	_, srvConn := net.Pipe()
	sess := newSession(srv, srvConn)
	srv.registerSession(sess)
	// Subscribe the session to the stream param OID (1.5).
	srv.subscribe(sess, "1.5")
	// Drain the out channel in the background so send() doesn't block.
	go func() {
		for range sess.out {
		}
	}()
	srv.fanoutStreams(srv.collectStreams())
	srv.dropSession(sess)
	sess.close()
}

// TestCollectStreams_MinMaxFallback covers collectStreams' explicit
// min/max capture from a parameter that declares them.
func TestCollectStreams_MinMaxFallback(t *testing.T) {
	srv := newServer(plugin.Deps{}, buildRichExport())
	streams := srv.collectStreams()
	if len(streams) != 1 {
		t.Fatalf("expected 1 stream, got %d", len(streams))
	}
	// realParam in buildRichExport has no min/max → fallback -60/0.
	if streams[0].min != -60 || streams[0].max != 0 {
		t.Errorf("min/max fallback = %v/%v, want -60/0", streams[0].min, streams[0].max)
	}
}

// TestCollectStreams_NilTree covers the nil-tree guard.
func TestCollectStreams_NilTree(t *testing.T) {
	srv := newServer(plugin.Deps{}, &canonical.Export{})
	if srv.collectStreams() != nil {
		t.Error("collectStreams on nil tree should be nil")
	}
}

// TestWriteEmBERChunks_Multi covers the multi-packet split path
// (FlagFirst / middle / FlagLast) for payloads > maxS101Payload.
func TestWriteEmBERChunks_Multi(t *testing.T) {
	srv := newServer(plugin.Deps{}, buildRichExport())
	cliConn, srvConn := net.Pipe()
	sess := newSession(srv, srvConn)

	// Drain the client side so writes don't block.
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := cliConn.Read(buf); err != nil {
				return
			}
		}
	}()

	big := bytes.Repeat([]byte{0x42}, maxS101Payload*2+10)
	if err := sess.writeEmBERChunks(big); err != nil {
		t.Fatalf("writeEmBERChunks multi: %v", err)
	}
	// Single-frame path (<= maxS101Payload) for completeness.
	if err := sess.writeEmBERChunks([]byte{0x01}); err != nil {
		t.Fatalf("writeEmBERChunks single: %v", err)
	}
	_ = cliConn.Close()
	_ = srvConn.Close()
}

// TestSend_QueueFull covers the drop-on-full branch of session.send.
func TestSend_QueueFull(t *testing.T) {
	srv := newServer(plugin.Deps{}, buildRichExport())
	_, srvConn := net.Pipe()
	sess := newSession(srv, srvConn)
	// Fill the 32-deep queue without draining, then one more → dropped.
	for i := 0; i < cap(sess.out)+5; i++ {
		sess.send([]byte{byte(i)})
	}
	sess.close()
}

// TestFindHelpers_NonQualified drives findCommand / findSetValue /
// findMatrixConnections through the non-qualified (Number-based) Node
// nesting forms + the base-path helpers, by feeding decoded elements
// directly.
func TestFindHelpers_NonQualified(t *testing.T) {
	// Non-qualified Subscribe (Node→...→Parameter→Command) exercises
	// findCommandInElements' Node recursion + nodeBasePath/paramBasePath.
	subLegacy := glow.EncodeSubscribeLegacy([]int32{1, 2, 3})
	els, err := glow.DecodeRoot(subLegacy)
	if err != nil {
		t.Fatalf("decode legacy subscribe: %v", err)
	}
	cmd, path := findCommandInElements(els)
	if cmd == nil || cmd.Number != glow.CmdSubscribe {
		t.Fatalf("findCommand non-qual: cmd=%+v", cmd)
	}
	if oidFromPath(path) == "" {
		t.Errorf("non-qual command path empty: %v", path)
	}

	// Non-qualified SetValue: Node(1){ children: Parameter(2){ value } }.
	setNested := ber.AppConstructed(glow.TagRoot,
		ber.AppConstructed(glow.TagRootElementCollection,
			ber.ContextConstructed(0,
				ber.AppConstructed(glow.TagNode,
					ber.ContextConstructed(glow.NodeNumber, ber.Integer(1)),
					ber.ContextConstructed(glow.NodeChildren,
						ber.AppConstructed(glow.TagElementCollection,
							ber.ContextConstructed(0,
								ber.AppConstructed(glow.TagParameter,
									ber.ContextConstructed(glow.ParamNumber, ber.Integer(2)),
									ber.ContextConstructed(glow.ParamContents, ber.Set(
										ber.ContextConstructed(glow.ParamContentValue, ber.Integer(7)),
									)),
								),
							),
						),
					),
				),
			),
		),
	)
	els2, err := glow.DecodeRoot(ber.EncodeTLV(setNested))
	if err != nil {
		t.Fatalf("decode nested set: %v", err)
	}
	p, v, ok := findSetValueInElements(els2)
	if !ok || v.(int64) != 7 || oidFromPath(p) != "1.2" {
		t.Errorf("findSetValue nested: path=%v v=%v ok=%v", p, v, ok)
	}

	// Non-qualified Matrix connection nested under a Node, via Number.
	matNested := ber.AppConstructed(glow.TagRoot,
		ber.AppConstructed(glow.TagRootElementCollection,
			ber.ContextConstructed(0,
				ber.AppConstructed(glow.TagNode,
					ber.ContextConstructed(glow.NodeNumber, ber.Integer(1)),
					ber.ContextConstructed(glow.NodeChildren,
						ber.AppConstructed(glow.TagElementCollection,
							ber.ContextConstructed(0,
								ber.AppConstructed(glow.TagMatrix,
									ber.ContextConstructed(glow.MatrixNumber, ber.Integer(7)),
									ber.ContextConstructed(glow.MatrixConnections,
										ber.Sequence(ber.ContextConstructed(0,
											ber.AppConstructed(glow.TagConnection,
												ber.ContextConstructed(glow.ConnTarget, ber.Integer(0)),
												ber.ContextConstructed(glow.ConnSources, ber.RelOID([]byte{1})),
											),
										)),
									),
								),
							),
						),
					),
				),
			),
		),
	)
	els3, err := glow.DecodeRoot(ber.EncodeTLV(matNested))
	if err != nil {
		t.Fatalf("decode nested matrix: %v", err)
	}
	mp, conns, ok := findMatrixConnectionsInElements(els3)
	if !ok || len(conns) != 1 || oidFromPath(mp) != "1.7" {
		t.Errorf("findMatrixConnections nested: path=%v conns=%v ok=%v", mp, conns, ok)
	}

	// matrixBasePath Number-only form (no path, non-zero number).
	if got := matrixBasePath(&glow.Matrix{Number: 9}); len(got) != 1 || got[0] != 9 {
		t.Errorf("matrixBasePath number-only: %v", got)
	}
	// matrixBasePath empty (number 0, no path) → nil.
	if got := matrixBasePath(&glow.Matrix{}); got != nil {
		t.Errorf("matrixBasePath empty: %v", got)
	}
	// funcBasePath Number-only form.
	if got := funcBasePath(&glow.Function{Number: 4}); len(got) != 1 || got[0] != 4 {
		t.Errorf("funcBasePath number-only: %v", got)
	}
	// oidFromPath empty.
	if oidFromPath(nil) != "" {
		t.Error("oidFromPath(nil) should be empty")
	}
}

// TestEncodeNonQualMatrix_FullAndError covers the targets/sources/
// connections appends and the bad-parametersLocation error in the
// non-qualified matrix encoder (used inside a template).
func TestEncodeNonQualMatrix_FullAndError(t *testing.T) {
	full := &canonical.Matrix{
		Header:      canonical.Header{Number: 1, Identifier: "m", OID: "1", IsOnline: true, Children: canonical.EmptyChildren()},
		Type:        canonical.MatrixNToN,
		TargetCount: 2, SourceCount: 2,
		Targets:     []canonical.MatrixTarget{{Number: 0}},
		Sources:     []canonical.MatrixSource{{Number: 0}},
		Connections: []canonical.MatrixConnection{{Target: 0, Sources: []int64{0}}},
	}
	if _, err := encodeNonQualMatrix(1, full); err != nil {
		t.Fatalf("encodeNonQualMatrix full: %v", err)
	}
	bad := &canonical.Matrix{
		Header:             canonical.Header{Number: 1, Identifier: "m", OID: "1", IsOnline: true, Children: canonical.EmptyChildren()},
		Type:               canonical.MatrixOneToN,
		ParametersLocation: strp("not.a.number"),
	}
	if _, err := encodeNonQualMatrix(1, bad); err == nil {
		t.Error("encodeNonQualMatrix with bad parametersLocation should error")
	}
}

// TestEncodeMatrixContents_Errors covers the bad-OID error branches for
// parametersLocation and templateReference, plus the description field.
func TestEncodeMatrixContents_Errors(t *testing.T) {
	desc := "d"
	withDesc := &canonical.Matrix{
		Header: canonical.Header{Identifier: "m", Description: &desc}, Type: canonical.MatrixOneToN,
	}
	if _, err := encodeMatrixContents(withDesc); err != nil {
		t.Fatalf("encodeMatrixContents desc: %v", err)
	}
	badTmpl := &canonical.Matrix{Header: canonical.Header{Identifier: "m"}, Type: canonical.MatrixOneToN, TemplateReference: strp("x.y")}
	if _, err := encodeMatrixContents(badTmpl); err == nil {
		t.Error("bad templateReference should error")
	}
}

// TestEncodeLabel_BadBasePath covers the label basePath parse error.
func TestEncodeLabel_BadBasePath(t *testing.T) {
	if _, err := encodeLabel(canonical.MatrixLabel{BasePath: "a.b"}); err == nil {
		t.Error("encodeLabel bad basePath should error")
	}
}

// TestEncodeConnection_OperationField covers the non-absolute operation
// branch (which the default getdir reply never emits).
func TestEncodeConnection_OperationField(t *testing.T) {
	tlv := encodeConnection(canonical.MatrixConnection{
		Target: 0, Sources: []int64{1}, Operation: canonical.ConnOpConnect, Disposition: canonical.ConnDispModified,
	})
	// Connect op + modified disposition both emit their CTX fields.
	if len(tlv.Children) != 4 {
		t.Errorf("expected target+sources+op+disp = 4 fields, got %d", len(tlv.Children))
	}
}

// TestEncodeTemplate_Errors covers the bad-OID and unsupported-kind
// branches of the template encoders.
func TestEncodeTemplate_Errors(t *testing.T) {
	badOID := &canonical.TemplateEntry{OID: "x.y", Identifier: "t",
		Template: &canonical.Node{Header: canonical.Header{Number: 1, Identifier: "n", IsOnline: true, Children: canonical.EmptyChildren()}}}
	if _, err := encodeQualifiedTemplate(badOID); err == nil {
		t.Error("template bad OID should error")
	}
	// Template element whose inner matrix has a bad parametersLocation →
	// encodeTemplateElement propagates the error.
	badInner := &canonical.TemplateEntry{OID: "1", Identifier: "t",
		Template: &canonical.Matrix{
			Header:             canonical.Header{Number: 1, Identifier: "m", IsOnline: true, Children: canonical.EmptyChildren()},
			Type:               canonical.MatrixOneToN,
			ParametersLocation: strp("bad.loc"),
		}}
	if _, err := encodeQualifiedTemplate(badInner); err == nil {
		t.Error("template with bad inner element should error")
	}
}

// TestBuiltins_BadArgTypes covers the bad-arg-type branches in the salvo
// builtins that the happy-path tests don't reach.
func TestBuiltins_BadArgTypes(t *testing.T) {
	srv := buildMatrixTree(t, &canonical.Matrix{Type: canonical.MatrixNToN, TargetCount: 2, SourceCount: 2})
	if _, err := srv.makeBuiltinRecallSalvo()([]any{int64(1), "x"}); err == nil {
		t.Error("recallSalvo bad arg types should error")
	}
	if _, err := srv.makeBuiltinStoreSalvo()([]any{int64(1), "x"}); err == nil {
		t.Error("storeSalvo bad arg types should error")
	}
	if _, err := srv.makeBuiltinListSalvos()([]any{int64(1)}); err == nil {
		t.Error("listSalvos bad arg type should error")
	}
	if _, err := srv.makeBuiltinGetSalvo()([]any{int64(1), int64(2)}); err == nil {
		t.Error("getSalvo bad matrixPath type should error")
	}
	if _, err := srv.makeBuiltinGetSalvo()([]any{"1.1", "x"}); err == nil {
		t.Error("getSalvo bad salvoID type should error")
	}
}

// TestLockStore_ListSort + TestSalvoStore_ListSort cover the sort.Slice
// less-funcs (need ≥2 entries to invoke the comparator).
func TestStores_ListSort(t *testing.T) {
	ls := newLockStore()
	ls.set("m", 3, true)
	ls.set("m", 1, true)
	got := ls.list("m")
	if len(got) != 2 || got[0] != 1 || got[1] != 3 {
		t.Errorf("lock list sort: %v, want [1 3]", got)
	}
	if ls.list("none") != nil {
		t.Error("lock list missing matrix should be nil")
	}

	ss := newSalvoStore()
	ss.store("m", 5, nil)
	ss.store("m", 2, nil)
	ids := ss.list("m")
	if len(ids) != 2 || ids[0] != 2 || ids[1] != 5 {
		t.Errorf("salvo list sort: %v, want [2 5]", ids)
	}
	if ss.list("none") != nil {
		t.Error("salvo list missing matrix should be nil")
	}
	if _, ok := ss.recall("none", 1); ok {
		t.Error("recall missing matrix should miss")
	}
}

// TestBroadcastParam_MissingOID covers the early-return when the oid is
// not in the tree index.
func TestBroadcastParam_MissingOID(t *testing.T) {
	srv := newServer(plugin.Deps{}, buildRichExport())
	srv.broadcastParam("9.9.9", &canonical.Parameter{}) // no panic, returns early
}

// TestBroadcastMatrixConnections_OriginSend covers the origin-send branch
// when the origin session is not already in the broadcast set.
func TestBroadcastMatrixConnections_OriginSend(t *testing.T) {
	srv := newServer(plugin.Deps{}, buildRichExport())
	_, srvConn := net.Pipe()
	origin := newSession(srv, srvConn)
	// origin NOT registered → not in s.sessions, so the origin-send
	// fallback fires.
	go func() {
		for range origin.out {
		}
	}()
	srv.broadcastMatrixConnections("1.7", []canonical.MatrixConnection{{Target: 0, Sources: []int64{1}}}, origin)
	// Missing oid → early return.
	srv.broadcastMatrixConnections("9.9", nil, nil)
	origin.close()
}

// TestCollectStreams_DeclaredMinMax covers the explicit min/max capture
// branches (a stream parameter that declares Minimum/Maximum).
func TestCollectStreams_DeclaredMinMax(t *testing.T) {
	p := &canonical.Parameter{
		Header: canonical.Header{Number: 1, Identifier: "m", Path: "root.m", OID: "1.1", IsOnline: true, Children: canonical.EmptyChildren()},
		Type:   canonical.ParamReal, StreamIdentifier: i64p(3),
		Minimum: float64(-20), Maximum: float64(20),
	}
	root := &canonical.Node{Header: canonical.Header{
		Number: 1, Identifier: "root", Path: "root", OID: "1", IsOnline: true,
		Access: canonical.AccessRead, Children: []canonical.Element{p},
	}}
	srv := newServer(plugin.Deps{}, &canonical.Export{Root: root})
	streams := srv.collectStreams()
	if len(streams) != 1 || streams[0].min != -20 || streams[0].max != 20 {
		t.Errorf("declared min/max: %+v", streams)
	}
}

// TestEncodeNodeContents_Description covers the [1] description branch via
// a non-qual node carrying a description.
func TestEncodeNodeContents_Description(t *testing.T) {
	n := &canonical.Node{Header: canonical.Header{
		Number: 1, Identifier: "n", Description: strp("desc"), IsOnline: true,
		Children: canonical.EmptyChildren(),
	}}
	tlv := encodeNodeContents(n)
	if len(tlv.Children) < 2 {
		t.Errorf("expected identifier+description, got %d children", len(tlv.Children))
	}
}

// TestRejectIfExceedsCap_NewTarget covers the foundTarget=false branch:
// a connect to a brand-new target that pushes the matrix-wide total over
// MaximumTotalConnects.
func TestRejectIfExceedsCap_NewTarget(t *testing.T) {
	m := &canonical.Matrix{
		Type: canonical.MatrixNToN, MaximumTotalConnects: i64p(1),
		Connections: []canonical.MatrixConnection{{Target: 0, Sources: []int64{0}}},
	}
	srv := buildMatrixTree(t, m)
	// New target 9 with a source → total would be 2 > cap 1 → rejected.
	post, err := srv.applyMatrixConnections("1.1", []canonical.MatrixConnection{
		{Target: 9, Sources: []int64{1}, Operation: canonical.ConnOpConnect},
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(post) != 1 || post[0].Disposition != canonical.ConnDispTally {
		t.Errorf("new-target cap reject: %+v", post)
	}
}

// TestApplyMatrixConnections_CollapseSameTarget covers the emit() collapse
// branch where two requests touch the same target in one batch.
func TestApplyMatrixConnections_CollapseSameTarget(t *testing.T) {
	m := &canonical.Matrix{Type: canonical.MatrixNToN, Connections: nil}
	srv := buildMatrixTree(t, m)
	post, err := srv.applyMatrixConnections("1.1", []canonical.MatrixConnection{
		{Target: 0, Sources: []int64{1}, Operation: canonical.ConnOpAbsolute},
		{Target: 0, Sources: []int64{2}, Operation: canonical.ConnOpAbsolute}, // same target → collapse
	})
	if err != nil || len(post) != 1 || len(post[0].Sources) != 1 || post[0].Sources[0] != 2 {
		t.Errorf("collapse same target: post=%+v err=%v", post, err)
	}
}

// TestFindHelpers_QualParamRecursion covers findCommand /findSetValue
// recursion into a QualifiedParameter's children + the Node-children
// findSetValue arm.
func TestFindHelpers_QualParamRecursion(t *testing.T) {
	// QualifiedParameter(path) with a child Command — exercises the
	// findCommandInElements Parameter-children recursion (B39).
	cmd := ber.AppConstructed(glow.TagRoot,
		ber.AppConstructed(glow.TagRootElementCollection,
			ber.ContextConstructed(0,
				ber.AppConstructed(glow.TagQualifiedParameter,
					ber.ContextConstructed(glow.QParamPath, ber.RelOID([]byte{1, 2})),
					ber.ContextConstructed(glow.QParamChildren,
						ber.AppConstructed(glow.TagElementCollection,
							ber.ContextConstructed(0,
								ber.AppConstructed(glow.TagCommand,
									ber.ContextConstructed(glow.CmdCtxNumber, ber.Integer(glow.CmdGetDirectory)),
								),
							),
						),
					),
				),
			),
		),
	)
	els, _ := glow.DecodeRoot(ber.EncodeTLV(cmd))
	if c, _ := findCommandInElements(els); c == nil {
		t.Error("findCommand via qualified-parameter children failed")
	}

	// QualifiedParameter with a nested child Parameter carrying the value
	// (findSetValue Parameter-children arm, B42-B43).
	setNested := ber.AppConstructed(glow.TagRoot,
		ber.AppConstructed(glow.TagRootElementCollection,
			ber.ContextConstructed(0,
				ber.AppConstructed(glow.TagQualifiedParameter,
					ber.ContextConstructed(glow.QParamPath, ber.RelOID([]byte{1})),
					ber.ContextConstructed(glow.QParamChildren,
						ber.AppConstructed(glow.TagElementCollection,
							ber.ContextConstructed(0,
								ber.AppConstructed(glow.TagParameter,
									ber.ContextConstructed(glow.ParamNumber, ber.Integer(2)),
									ber.ContextConstructed(glow.ParamContents, ber.Set(
										ber.ContextConstructed(glow.ParamContentValue, ber.Integer(5)),
									)),
								),
							),
						),
					),
				),
			),
		),
	)
	els2, _ := glow.DecodeRoot(ber.EncodeTLV(setNested))
	if _, v, ok := findSetValueInElements(els2); !ok || v.(int64) != 5 {
		t.Errorf("findSetValue via qualified-parameter children failed: v=%v ok=%v", v, ok)
	}
}

// TestFindMatrixConnections_MatrixChildren covers the recursion into a
// Matrix's children for a nested matrix-in-matrix connection.
func TestFindMatrixConnections_MatrixChildren(t *testing.T) {
	nested := ber.AppConstructed(glow.TagRoot,
		ber.AppConstructed(glow.TagRootElementCollection,
			ber.ContextConstructed(0,
				ber.AppConstructed(glow.TagQualifiedMatrix,
					ber.ContextConstructed(0, ber.RelOID([]byte{1})),
					ber.ContextConstructed(2, // children
						ber.AppConstructed(glow.TagElementCollection,
							ber.ContextConstructed(0,
								ber.AppConstructed(glow.TagMatrix,
									ber.ContextConstructed(glow.MatrixNumber, ber.Integer(7)),
									ber.ContextConstructed(glow.MatrixConnections,
										ber.Sequence(ber.ContextConstructed(0,
											ber.AppConstructed(glow.TagConnection,
												ber.ContextConstructed(glow.ConnTarget, ber.Integer(0)),
											),
										)),
									),
								),
							),
						),
					),
				),
			),
		),
	)
	els, _ := glow.DecodeRoot(ber.EncodeTLV(nested))
	if _, conns, ok := findMatrixConnectionsInElements(els); !ok || len(conns) != 1 {
		t.Errorf("findMatrixConnections via matrix children failed: conns=%v ok=%v", conns, ok)
	}
}

// TestHandleEmber_TraceAndErrors drives the handleEmber fall-through trace
// (unhandled element kinds) + the decode-error and matrix-fail paths via a
// session, exercising those branches directly.
func TestHandleEmber_TraceAndErrors(t *testing.T) {
	srv := newServer(plugin.Deps{}, buildRichExport())
	_, srvConn := net.Pipe()
	sess := newSession(srv, srvConn)
	go func() {
		for range sess.out {
		}
	}()

	// Malformed glow → decode error branch.
	if err := sess.handleEmber([]byte{0x02, 0x05, 0x01}); err == nil {
		t.Error("handleEmber on malformed glow should error")
	}

	// A Root with one of each unhandled element kind (no command/value/conn)
	// → the kinds-trace fall-through. Parameter without value, Node without
	// command, Matrix without connections, Function.
	unhandled := ber.AppConstructed(glow.TagRoot,
		ber.AppConstructed(glow.TagRootElementCollection,
			ber.ContextConstructed(0, ber.AppConstructed(glow.TagParameter,
				ber.ContextConstructed(glow.ParamNumber, ber.Integer(1)))),
			ber.ContextConstructed(0, ber.AppConstructed(glow.TagNode,
				ber.ContextConstructed(glow.NodeNumber, ber.Integer(2)))),
			ber.ContextConstructed(0, ber.AppConstructed(glow.TagMatrix,
				ber.ContextConstructed(glow.MatrixNumber, ber.Integer(3)))),
			ber.ContextConstructed(0, ber.AppConstructed(glow.TagFunction,
				ber.ContextConstructed(glow.FuncNumber, ber.Integer(4)))),
		),
	)
	if err := sess.handleEmber(ber.EncodeTLV(unhandled)); err != nil {
		t.Errorf("handleEmber unhandled trace returned error: %v", err)
	}

	// Matrix connection to a missing matrix → applyMatrixConnections fails,
	// handleEmber swallows it (returns nil).
	badMatrix := glow.EncodeMatrixConnect([]int32{9, 9}, 0, []int32{1}, glow.ConnOpAbsolute)
	if err := sess.handleEmber(badMatrix); err != nil {
		t.Errorf("handleEmber bad matrix should swallow error: %v", err)
	}
	sess.close()
}

// TestReplyGetDirectory_EncodeErrorPropagates is a guard: replyGetDirectory
// on a valid oid returns nil; the encode-error branch is defensive (the
// tree is always consistent) so we just confirm the happy path here.
func TestReplyGetDirectory_Happy(t *testing.T) {
	srv := newServer(plugin.Deps{}, buildRichExport())
	_, srvConn := net.Pipe()
	sess := newSession(srv, srvConn)
	go func() {
		for range sess.out {
		}
	}()
	if err := sess.replyGetDirectory("1.7"); err != nil { // matrix → implicit subscribe
		t.Errorf("replyGetDirectory matrix: %v", err)
	}
	if err := sess.replyGetDirectory("9.9.9"); err == nil {
		t.Error("replyGetDirectory unknown oid should error")
	}
	sess.close()
}

// TestWalkFunctions_NonFunctionRoot covers walkFunctions descending past
// non-Function nodes (the recursion-only branch).
func TestWalkFunctions_NonFunctionRoot(t *testing.T) {
	srv := newServer(plugin.Deps{}, buildRichExport())
	var count int
	srv.walkFunctions(srv.tree.root, func(e *entry, f *canonical.Function) { count++ })
	if count != 1 { // buildRichExport has exactly one Function
		t.Errorf("walkFunctions visited %d functions, want 1", count)
	}
	// nil element guard.
	srv.walkFunctions(nil, func(*entry, *canonical.Function) {})
}

// TestRejectIfExceedsCap_ExistingTarget covers the foundTarget=true
// branch: a Connect on an EXISTING target whose growth pushes the
// matrix-wide total over MaximumTotalConnects.
func TestRejectIfExceedsCap_ExistingTarget(t *testing.T) {
	m := &canonical.Matrix{
		Type: canonical.MatrixNToN, MaximumTotalConnects: i64p(2),
		Connections: []canonical.MatrixConnection{
			{Target: 0, Sources: []int64{0}},
			{Target: 1, Sources: []int64{1}},
		},
	}
	srv := buildMatrixTree(t, m)
	// Connect a 2nd source to existing target 0 → total would be 3 > 2.
	post, err := srv.applyMatrixConnections("1.1", []canonical.MatrixConnection{
		{Target: 0, Sources: []int64{2}, Operation: canonical.ConnOpConnect},
	})
	if err != nil || len(post) != 1 || post[0].Disposition != canonical.ConnDispTally {
		t.Errorf("existing-target total cap: post=%+v err=%v", post, err)
	}
}

// TestHandleEmber_UnknownElementKind covers the "unknown" trace arm: a
// Root carrying only a StreamCollection (decodes to an element with all
// concrete kind pointers nil).
func TestHandleEmber_UnknownElementKind(t *testing.T) {
	srv := newServer(plugin.Deps{}, buildRichExport())
	_, srvConn := net.Pipe()
	sess := newSession(srv, srvConn)
	go func() {
		for range sess.out {
		}
	}()
	stream := ber.AppConstructed(glow.TagRoot,
		ber.AppConstructed(glow.TagStreamCollection,
			ber.ContextConstructed(0,
				ber.AppConstructed(glow.TagStreamEntry,
					ber.ContextConstructed(glow.StreamEntryIdentifier, ber.Integer(1)),
					ber.ContextConstructed(glow.StreamEntryValue, ber.Integer(2)),
				),
			),
		),
	)
	if err := sess.handleEmber(ber.EncodeTLV(stream)); err != nil {
		t.Errorf("handleEmber stream element: %v", err)
	}
	sess.close()
}

// TestEncodeGetDirReply_ChildEncodeError covers the error-propagation
// branch when a child element's encoder fails (a Matrix child with a bad
// parametersLocation OID).
func TestEncodeGetDirReply_ChildEncodeError(t *testing.T) {
	badMtx := &canonical.Matrix{
		Header: canonical.Header{
			Number: 1, Identifier: "m", Path: "root.m", OID: "1.1",
			IsOnline: true, Children: canonical.EmptyChildren(),
		},
		Type: canonical.MatrixOneToN, TargetCount: 1, SourceCount: 1,
		ParametersLocation: strp("not.numeric"),
	}
	root := &canonical.Node{Header: canonical.Header{
		Number: 1, Identifier: "root", Path: "root", OID: "1", IsOnline: true,
		Access: canonical.AccessRead, Children: []canonical.Element{badMtx},
	}}
	srv := newServer(plugin.Deps{}, &canonical.Export{Root: root})
	if _, err := srv.encodeGetDirReply(srv.tree.rootEntry(), false); err == nil {
		t.Error("encodeGetDirReply should propagate child encode error")
	}
}

// TestWritePump_WriteError covers the writePump write-error exit: closing
// the peer end of the pipe makes WriteFrame fail.
func TestWritePump_WriteError(t *testing.T) {
	srv := newServer(plugin.Deps{}, buildRichExport())
	cliConn, srvConn := net.Pipe()
	sess := newSession(srv, srvConn)
	_ = cliConn.Close() // peer gone → writes fail
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sess.writePump(ctx)
	sess.send(bytes.Repeat([]byte{0x01}, 4))
	time.Sleep(30 * time.Millisecond)
	sess.close()
	_ = srvConn.Close()
}

// customElement is a canonical.Element of an unrecognised kind, used to
// reach the default arms of encodeQualifiedElement and
// encodeTemplateElement (the "kind not supported" guards).
type customElement struct{ hdr canonical.Header }

func (c *customElement) Kind() string              { return "custom" }
func (c *customElement) Common() *canonical.Header { return &c.hdr }

// TestEncoders_UnsupportedKind covers the default error arms of
// encodeQualifiedElement and encodeTemplateElement via a custom Element
// kind the type switches don't recognise.
func TestEncoders_UnsupportedKind(t *testing.T) {
	srv := newServer(plugin.Deps{}, buildRichExport())
	ce := &customElement{hdr: canonical.Header{Number: 1, Identifier: "c", OID: "1", IsOnline: true, Children: canonical.EmptyChildren()}}
	if _, err := srv.encodeQualifiedElement(&entry{el: ce, oidParts: []uint32{1}}); err == nil {
		t.Error("encodeQualifiedElement on custom kind should error")
	}
	if _, err := encodeTemplateElement(ce); err == nil {
		t.Error("encodeTemplateElement on custom kind should error")
	}
}

// TestMergeSources_Dedup covers the dedup continue branches in
// mergeSources via a Connect that re-adds an already-present source plus
// a duplicate inside the add list.
func TestMergeSources_Dedup(t *testing.T) {
	m := &canonical.Matrix{
		Type: canonical.MatrixNToN, MaximumConnectsPerTarget: i64p(10), MaximumTotalConnects: i64p(10),
		Connections: []canonical.MatrixConnection{{Target: 0, Sources: []int64{1, 2}}},
	}
	srv := buildMatrixTree(t, m)
	// Connect sources {2,2,3}: 2 already present (existing dedup), 2 dup in add.
	post, err := srv.applyMatrixConnections("1.1", []canonical.MatrixConnection{
		{Target: 0, Sources: []int64{2, 2, 3}, Operation: canonical.ConnOpConnect},
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	// Result should be {1,2,3} — no duplicates.
	if len(post) != 1 || len(post[0].Sources) != 3 {
		t.Errorf("mergeSources dedup: %+v", post)
	}
}

// TestHandleFrame_UnknownCommand covers handleFrame's default arm (a
// frame whose Command byte is none of keepalive/EmBER).
func TestHandleFrame_UnknownCommand(t *testing.T) {
	srv := newServer(plugin.Deps{}, buildRichExport())
	_, srvConn := net.Pipe()
	sess := newSession(srv, srvConn)
	if err := sess.handleFrame(&s101.Frame{Command: 0x7F}); err != nil {
		t.Errorf("handleFrame unknown command should be a no-op: %v", err)
	}
	// Keep-alive response branch.
	if err := sess.handleFrame(&s101.Frame{Command: s101.CmdKeepAliveResp}); err != nil {
		t.Errorf("handleFrame keepalive-resp: %v", err)
	}
	sess.close()
}

// TestSweepIdleSessions_Closes covers the sweep collect + close loop with
// a registered session whose lastActive is older than the cutoff.
func TestSweepIdleSessions_Closes(t *testing.T) {
	srv := newServer(plugin.Deps{}, buildRichExport())
	_, srvConn := net.Pipe()
	sess := newSession(srv, srvConn)
	srv.registerSession(sess)
	// Backdate activity so any positive ttl sweeps it.
	sess.lastActive.Store(time.Now().Add(-time.Hour).UnixNano())
	srv.sweepIdleSessions(time.Minute)
	// After sweep the session should be dropped.
	srv.mu.Lock()
	_, still := srv.sessions[sess]
	srv.mu.Unlock()
	if still {
		t.Error("idle session not swept")
	}
}

// TestStop_WithSessions covers Stop's session-collection + close loop.
func TestStop_WithSessions(t *testing.T) {
	srv := newServer(plugin.Deps{}, buildRichExport())
	_, srvConn := net.Pipe()
	sess := newSession(srv, srvConn)
	srv.registerSession(sess)
	if err := srv.Stop(); err != nil {
		t.Errorf("Stop: %v", err)
	}
	// Stop again (stopOnce guards) — no panic.
	_ = srv.Stop()
}

// TestReplyGetDirectory_EncodeError covers the encode-error propagation in
// replyGetDirectory: a queried node whose child Matrix has a bad OID.
func TestReplyGetDirectory_EncodeError(t *testing.T) {
	badMtx := &canonical.Matrix{
		Header: canonical.Header{
			Number: 2, Identifier: "m", Path: "root.sub.m", OID: "1.1.1",
			IsOnline: true, Children: canonical.EmptyChildren(),
		},
		Type: canonical.MatrixOneToN, TargetCount: 1, SourceCount: 1,
		ParametersLocation: strp("bad.loc"),
	}
	sub := &canonical.Node{Header: canonical.Header{
		Number: 1, Identifier: "sub", Path: "root.sub", OID: "1.1", IsOnline: true,
		Access: canonical.AccessRead, Children: []canonical.Element{badMtx},
	}}
	root := &canonical.Node{Header: canonical.Header{
		Number: 1, Identifier: "root", Path: "root", OID: "1", IsOnline: true,
		Access: canonical.AccessRead, Children: []canonical.Element{sub},
	}}
	srv := newServer(plugin.Deps{}, &canonical.Export{Root: root})
	_, srvConn := net.Pipe()
	sess := newSession(srv, srvConn)
	go func() {
		for range sess.out {
		}
	}()
	if err := sess.replyGetDirectory("1.1"); err == nil {
		t.Error("replyGetDirectory should propagate child encode error")
	}
	sess.close()
}

// TestServe_AcceptAfterCancel exercises the accept loop's ctx-cancel and
// listener-closed exit by cancelling immediately after the listener is up.
func TestServe_AcceptAfterCancel(t *testing.T) {
	srv := newServer(plugin.Deps{}, buildRichExport())
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ctx, "127.0.0.1:0") }()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		srv.mu.Lock()
		up := srv.listener != nil
		srv.mu.Unlock()
		if up {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	cancel()
	select {
	case <-errc:
	case <-time.After(2 * time.Second):
		t.Error("Serve did not return after cancel")
	}
	_ = srv.Stop()
}
