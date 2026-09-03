package acp2

import (
	"encoding/binary"
	"errors"
	"net"
	"testing"
	"time"

	"dhs/internal/acp2/codec"
	"dhs/internal/export/canonical"
)

// coverage100_test.go closes the residual branches the other provider
// tests don't reach: the typed-value encode error paths in
// buildProperties / encodeValueProp, the handler error fan-outs
// (buildProperties + EncodeProperty failures, write failures), the
// tree.go flatten / newTree / deriveAccess edges, the Serve accept-error
// and broadcast send-failure paths, the session read-error path, and the
// demo emitFloatAnnounce guards. All assertions use the production codec
// and the real types — no behaviour is mocked.

// ----------------------------------------------------------------------
// encoder.go — encodeValueProp / buildProperties typed-value error paths

// badParam returns a parameter whose Value is a type the per-objType
// encoder cannot coerce, forcing the value-encode error branch.
func TestEncodeValueProp_Errors(t *testing.T) {
	// Enum: Value is a non-numeric, non-coercible type → asUint32 error.
	enumE := &entry{objType: codec.ObjTypeEnum, numType: codec.NumTypeU32,
		param: &canonical.Parameter{Value: []byte{1}}}
	if _, err := encodeValueProp(codec.PIDValue, enumE); err == nil {
		t.Error("enum bad value should error")
	}

	// IPv4: Value not a dotted-quad string → ipv4Uint32 error.
	ipE := &entry{objType: codec.ObjTypeIPv4, numType: codec.NumTypeIPv4,
		param: &canonical.Parameter{Value: 12345}}
	if _, err := encodeValueProp(codec.PIDValue, ipE); err == nil {
		t.Error("ipv4 bad value should error")
	}

	// String: Value not a string → asString error.
	strE := &entry{objType: codec.ObjTypeString, numType: codec.NumTypeString,
		param: &canonical.Parameter{Value: 99}}
	if _, err := encodeValueProp(codec.PIDValue, strE); err == nil {
		t.Error("string bad value should error")
	}

	// Number: Value not coercible to int → encodeNumericProp error.
	numE := &entry{objType: codec.ObjTypeNumber, numType: codec.NumTypeS32,
		param: &canonical.Parameter{Value: []byte{1}}}
	if _, err := encodeValueProp(codec.PIDValue, numE); err == nil {
		t.Error("number bad value should error")
	}

	// Unsupported objType → the final fmt.Errorf return.
	nodeE := &entry{objType: codec.ObjTypeNode, param: &canonical.Parameter{}}
	if _, err := encodeValueProp(codec.PIDValue, nodeE); err == nil {
		t.Error("node objType should error in encodeValueProp")
	}
}

// TestBuildProperties_NumberErrors walks every buildProperties Number-arm
// error return: value, default, min, max, step. Each uses a parameter
// whose relevant field is set to an un-coercible type.
func TestBuildProperties_NumberErrors(t *testing.T) {
	mk := func(p *canonical.Parameter) *entry {
		return &entry{objType: codec.ObjTypeNumber, numType: codec.NumTypeS32,
			access: 0x03, param: p}
	}
	bad := []byte{1} // never coercible by asInt64

	// value error
	if _, err := buildProperties(mk(&canonical.Parameter{Value: bad})); err == nil {
		t.Error("number value error not surfaced")
	}
	// default error (value ok, default bad)
	if _, err := buildProperties(mk(&canonical.Parameter{Value: int64(0), Default: bad})); err == nil {
		t.Error("number default error not surfaced")
	}
	// min error
	if _, err := buildProperties(mk(&canonical.Parameter{Value: int64(0), Minimum: bad})); err == nil {
		t.Error("number min error not surfaced")
	}
	// max error
	if _, err := buildProperties(mk(&canonical.Parameter{Value: int64(0), Maximum: bad})); err == nil {
		t.Error("number max error not surfaced")
	}
	// step error (optional constraint present but bad)
	if _, err := buildProperties(mk(&canonical.Parameter{Value: int64(0), Step: bad})); err == nil {
		t.Error("number step error not surfaced")
	}
}

// TestBuildProperties_NumberStepUnit covers the happy step + unit emission
// (lines previously uncovered: step present → append, unit present → append).
func TestBuildProperties_NumberStepUnit(t *testing.T) {
	unit := "dB"
	p := &canonical.Parameter{
		Type: canonical.ParamInteger, Value: int64(0),
		Step: int64(2), Unit: &unit,
	}
	e := &entry{objType: codec.ObjTypeNumber, numType: codec.NumTypeS32,
		access: 0x03, param: p}
	props, err := buildProperties(e)
	if err != nil {
		t.Fatalf("buildProperties: %v", err)
	}
	var hasStep, hasUnit bool
	for _, pr := range props {
		switch pr.PID {
		case codec.PIDStepSize:
			hasStep = true
		case codec.PIDUnit:
			hasUnit = true
		}
	}
	if !hasStep {
		t.Error("pid 12 step not emitted")
	}
	if !hasUnit {
		t.Error("pid 13 unit not emitted")
	}
}

// TestBuildProperties_EnumError covers the Enum value + default error arms.
func TestBuildProperties_EnumError(t *testing.T) {
	bad := []byte{1}
	// value error
	e := &entry{objType: codec.ObjTypeEnum, numType: codec.NumTypeU32, access: 0x03,
		param: &canonical.Parameter{Value: bad}}
	if _, err := buildProperties(e); err == nil {
		t.Error("enum value error not surfaced")
	}
	// default error: value ok, but Default un-coercible
	e = &entry{objType: codec.ObjTypeEnum, numType: codec.NumTypeU32, access: 0x03,
		param: &canonical.Parameter{Value: int64(0), Default: bad}}
	if _, err := buildProperties(e); err == nil {
		t.Error("enum default error not surfaced")
	}
}

// TestBuildProperties_PresetError covers the Preset per-idx value + default
// error arms.
func TestBuildProperties_PresetError(t *testing.T) {
	bad := []byte{1}
	// value error
	e := &entry{objType: codec.ObjTypePreset, numType: codec.NumTypeS32, access: 0x03,
		presetDepth: 1, param: &canonical.Parameter{Value: bad}}
	if _, err := buildProperties(e); err == nil {
		t.Error("preset value error not surfaced")
	}
	// default error: value ok, Default un-coercible
	e = &entry{objType: codec.ObjTypePreset, numType: codec.NumTypeS32, access: 0x03,
		presetDepth: 1, param: &canonical.Parameter{Value: int64(0), Default: bad}}
	if _, err := buildProperties(e); err == nil {
		t.Error("preset default error not surfaced")
	}
}

// TestBuildProperties_IPv4Error covers the IPv4 value error arm.
func TestBuildProperties_IPv4Error(t *testing.T) {
	e := &entry{objType: codec.ObjTypeIPv4, numType: codec.NumTypeIPv4, access: 0x03,
		param: &canonical.Parameter{Value: 123}}
	if _, err := buildProperties(e); err == nil {
		t.Error("ipv4 value error not surfaced")
	}
}

// TestBuildProperties_StringError covers the String value error arm.
func TestBuildProperties_StringError(t *testing.T) {
	e := &entry{objType: codec.ObjTypeString, numType: codec.NumTypeString, access: 0x03,
		param: &canonical.Parameter{Value: 123}}
	if _, err := buildProperties(e); err == nil {
		t.Error("string value error not surfaced")
	}
}

// ----------------------------------------------------------------------
// tree.go — flatten / newTree / deriveAccess / elementID edges

// TestNewTree_RootErrors covers the two newTree guards: nil export and a
// non-Node root.
func TestNewTree_RootErrors(t *testing.T) {
	if _, err := newTree(nil); err == nil {
		t.Error("nil export should error")
	}
	if _, err := newTree(&canonical.Export{Root: nil}); err == nil {
		t.Error("nil root should error")
	}
	// Root is a Parameter, not a Node.
	bad := &canonical.Export{Root: &canonical.Parameter{
		Header: canonical.Header{Number: 1}, Type: canonical.ParamInteger}}
	if _, err := newTree(bad); err == nil {
		t.Error("non-Node root should error")
	}
}

// TestNewTree_SkipsNonNodeSlotChildren covers the slotNode type-assert
// continue: a Parameter directly under the device root (not a slot Node)
// is skipped.
func TestNewTree_SkipsNonNodeSlotChildren(t *testing.T) {
	stray := &canonical.Parameter{Header: canonical.Header{Number: 9},
		Type: canonical.ParamInteger}
	dev := &canonical.Node{Header: canonical.Header{Number: 1, Identifier: "device",
		Children: []canonical.Element{stray}}}
	tr, err := newTree(&canonical.Export{Root: dev})
	if err != nil {
		t.Fatalf("newTree: %v", err)
	}
	if len(tr.perSlot) != 0 {
		t.Errorf("perSlot=%d want 0 (stray param skipped)", len(tr.perSlot))
	}
}

// TestNewTree_FlattenError surfaces a flatten error: a Node child that
// has no Number-bearing element kind would error, but elementID always
// resolves Node/Parameter. Instead drive the Parameter deriveACP2Type
// reject (boolean) which flatten wraps.
func TestNewTree_FlattenError(t *testing.T) {
	boolParam := &canonical.Parameter{
		Header: canonical.Header{Number: 2, Identifier: "B"},
		Type:   canonical.ParamBoolean,
	}
	root := &canonical.Node{Header: canonical.Header{Number: 1, Identifier: "ROOT",
		Children: []canonical.Element{boolParam}}}
	slot := &canonical.Node{Header: canonical.Header{Number: 1, Identifier: "slot-1",
		Children: []canonical.Element{root}}}
	dev := &canonical.Node{Header: canonical.Header{Number: 1, Identifier: "device",
		Children: []canonical.Element{slot}}}
	if _, err := newTree(&canonical.Export{Root: dev}); err == nil {
		t.Error("boolean leaf should make newTree error")
	}
}

// TestFlatten_DisabledSkipped covers both disabled paths: a disabled leaf
// is dropped from pid=14 AND never indexed (isDisabledElement continue),
// and a disabled Parameter passed straight to flatten returns nil early.
func TestFlatten_DisabledSkipped(t *testing.T) {
	disFmt := "s32,disabled"
	disabled := &canonical.Parameter{
		Header: canonical.Header{Number: 7, Identifier: "Hidden"},
		Type:   canonical.ParamInteger, Value: int64(0), Format: &disFmt,
	}
	okFmt := "s32"
	visible := &canonical.Parameter{
		Header: canonical.Header{Number: 8, Identifier: "Visible"},
		Type:   canonical.ParamInteger, Value: int64(0), Format: &okFmt,
	}
	parent := &canonical.Node{Header: canonical.Header{Number: 2, Identifier: "N",
		Children: []canonical.Element{disabled, visible}}}

	index := map[uint32]*entry{}
	if err := flatten(1, 0, parent, index); err != nil {
		t.Fatalf("flatten: %v", err)
	}
	if _, ok := index[7]; ok {
		t.Error("disabled leaf 7 should not be indexed")
	}
	if _, ok := index[8]; !ok {
		t.Error("visible leaf 8 should be indexed")
	}
	pe := index[2]
	for _, c := range pe.children {
		if c == 7 {
			t.Error("disabled leaf 7 leaked into pid=14 children")
		}
	}

	// Disabled Parameter handed straight to flatten → early nil return,
	// nothing indexed.
	idx2 := map[uint32]*entry{}
	if err := flatten(1, 0, disabled, idx2); err != nil {
		t.Fatalf("flatten disabled leaf: %v", err)
	}
	if len(idx2) != 0 {
		t.Errorf("disabled leaf direct-flatten indexed %d entries, want 0", len(idx2))
	}
}

// TestFlatten_UnknownElement covers flatten's trailing return nil for an
// element kind that is neither Node nor Parameter (a Function).
func TestFlatten_UnknownElement(t *testing.T) {
	idx := map[uint32]*entry{}
	fn := &canonical.Function{Header: canonical.Header{Number: 3}}
	if err := flatten(1, 0, fn, idx); err != nil {
		t.Fatalf("flatten unknown element: %v", err)
	}
	if len(idx) != 0 {
		t.Errorf("unknown element indexed %d, want 0", len(idx))
	}
}

// TestElementID_Unknown covers elementID's error arm for a non-Node /
// non-Parameter element (a Function).
func TestElementID_Unknown(t *testing.T) {
	fn := &canonical.Function{Header: canonical.Header{Number: 3}}
	if _, err := elementID(fn); err == nil {
		t.Error("elementID on unknown element should error")
	}
}

// TestDeriveAccess_All covers the four deriveAccess arms incl. the
// default-zero fall-through for an unrecognised access string.
func TestDeriveAccess_All(t *testing.T) {
	cases := []struct {
		in   string
		want uint8
	}{
		{canonical.AccessRead, 0x01},
		{canonical.AccessWrite, 0x02},
		{canonical.AccessReadWrite, 0x03},
		{"bogus", 0x00},
	}
	for _, tc := range cases {
		if got := deriveAccess(tc.in); got != tc.want {
			t.Errorf("deriveAccess(%q)=%02x want %02x", tc.in, got, tc.want)
		}
	}
}

// TestPresetIdxHint_Empty covers the empty-string idx token skip and the
// absent-hint nil return.
func TestPresetIdxHint_Empty(t *testing.T) {
	mk := func(f string) *canonical.Parameter { ff := f; return &canonical.Parameter{Format: &ff} }
	// idx with empty pipe segments → skipped, but valid ones kept.
	if got := presetIdxHint(mk("preset,idx=100||200")); len(got) != 2 || got[0] != 100 || got[1] != 200 {
		t.Errorf("idx=100||200 → %v want [100 200]", got)
	}
	// no idx token → nil.
	if got := presetIdxHint(mk("preset,depth=2")); got != nil {
		t.Errorf("no idx → %v want nil", got)
	}
}

// ----------------------------------------------------------------------
// handlers.go — buildProperties / encode error fan-outs + write failures

// brokenEntry is an entry whose Value can't be encoded, so handlers that
// call buildProperties / EncodeProperty on it take their error arms.
func brokenEntry(objID uint32) *entry {
	return &entry{objID: objID, slot: 1, access: 0x03,
		objType: codec.ObjTypeNumber, numType: codec.NumTypeS32,
		param: &canonical.Parameter{Value: []byte{1}}} // un-coercible
}

// installEntry puts e into a one-session server's tree at (slot, e.objID)
// and returns a session whose conn is a live net.Pipe (peer drained).
func sessionWithEntry(t *testing.T, e *entry) (*session, net.Conn) {
	t.Helper()
	sess, peer := newTestSession(t)
	sess.srv.tree.perSlot[1][e.objID] = e
	return sess, peer
}

// drain reads and discards everything from c until it closes.
func drain(c net.Conn) {
	go func() {
		buf := make([]byte, 256)
		for {
			if _, err := c.Read(buf); err != nil {
				return
			}
		}
	}()
}

// TestHandleGetObject_BuildPropertiesError drives handleGetObject onto an
// entry whose buildProperties fails → ErrProtocol reply.
func TestHandleGetObject_BuildPropertiesError(t *testing.T) {
	sess, peer := sessionWithEntry(t, brokenEntry(100))
	defer func() { _ = sess.conn.Close() }()
	defer func() { _ = peer.Close() }()
	drain(peer)
	sess.handleGetObject(1, &codec.ACP2Message{
		Type: codec.ACP2TypeRequest, MTID: 1, Func: codec.ACP2FuncGetObject, ObjID: 100})
}

// TestHandleGetProperty_BuildPropertiesError drives handleGetProperty's
// buildProperties error arm.
func TestHandleGetProperty_BuildPropertiesError(t *testing.T) {
	sess, peer := sessionWithEntry(t, brokenEntry(101))
	defer func() { _ = sess.conn.Close() }()
	defer func() { _ = peer.Close() }()
	drain(peer)
	sess.handleGetProperty(1, &codec.ACP2Message{
		Type: codec.ACP2TypeRequest, MTID: 1, Func: codec.ACP2FuncGetProperty,
		ObjID: 101, PID: codec.PIDValue})
}

// TestHandleSetProperty_NoProperties drives the len(Properties)==0 reject.
func TestHandleSetProperty_NoProperties(t *testing.T) {
	p := &canonical.Parameter{Type: canonical.ParamInteger, Value: int64(0)}
	fp := "s32"
	p.Format = &fp
	e := &entry{objID: 102, slot: 1, access: 0x03, objType: codec.ObjTypeNumber,
		numType: codec.NumTypeS32, param: p}
	sess, peer := sessionWithEntry(t, e)
	defer func() { _ = sess.conn.Close() }()
	defer func() { _ = peer.Close() }()
	drain(peer)
	sess.handleSetProperty(1, &codec.ACP2Message{
		Type: codec.ACP2TypeRequest, MTID: 1, Func: codec.ACP2FuncSetProperty,
		ObjID: 102, PID: codec.PIDValue, Properties: nil})
}

// TestHandlers_WriteFailure closes the session conn so every reply write
// fails, exercising the write-error log arms across replyACP2 (and thus
// the handler reply paths) + handleAN2Internal's reply-write-fail arm.
func TestHandlers_WriteFailure(t *testing.T) {
	// AN2 internal reply write failure.
	sess, peer := newTestSession(t)
	_ = peer.Close()
	_ = sess.conn.Close() // writes now fail immediately
	sess.handleAN2Internal(&codec.AN2Frame{
		Proto: codec.AN2ProtoInternal, Type: codec.AN2TypeRequest,
		Payload: []byte{codec.AN2FuncGetVersion}})

	// ACP2 reply write failure (replyACP2 → write error arm).
	sess2, peer2 := newTestSession(t)
	_ = peer2.Close()
	_ = sess2.conn.Close()
	sess2.replyACP2(1, &codec.ACP2Message{
		Type: codec.ACP2TypeReply, MTID: 1, Func: codec.ACP2FuncGetVersion, PID: 2})
}

// TestWrite_EncodeError forces session.write's EncodeAN2Frame error arm by
// handing it a frame with an out-of-range proto the AN2 encoder rejects.
func TestWrite_EncodeError(t *testing.T) {
	sess, peer := newTestSession(t)
	defer func() { _ = sess.conn.Close() }()
	defer func() { _ = peer.Close() }()
	// A payload larger than MaxPayload (65536) forces the encoder to
	// error before any socket write.
	big := make([]byte, 0x1_0001) // 65537 > MaxPayload
	err := sess.write(&codec.AN2Frame{
		Proto: codec.AN2ProtoACP2, Slot: 1, Type: codec.AN2TypeData, Payload: big})
	if err == nil {
		t.Error("write of oversize frame should surface an encode error")
	}
}

// ----------------------------------------------------------------------
// server.go — Serve accept-error (non-ErrClosed) + broadcast send failure

// TestServe_AcceptHardError makes ln.Accept return a non-ErrClosed error
// so acceptLoop returns it (the close(s.stopped)+return err arm). A
// synthetic listener yields a non-net.ErrClosed error on Accept.
func TestServe_AcceptHardError(t *testing.T) {
	srv := newServer(quietLogger(), buildServeExport())
	hl := &hardErrListener{}
	err := srv.acceptLoop(hl)
	if err == nil || errors.Is(err, net.ErrClosed) {
		t.Fatalf("acceptLoop should surface the hard accept error, got %v", err)
	}
}

// hardErrListener is a net.Listener whose Accept always fails with a
// non-net.ErrClosed error, driving acceptLoop's hard-error return arm.
type hardErrListener struct{}

func (*hardErrListener) Accept() (net.Conn, error) { return nil, errors.New("synthetic accept failure") }
func (*hardErrListener) Close() error              { return nil }
func (*hardErrListener) Addr() net.Addr            { return dummyAddr{} }

type dummyAddr struct{}

func (dummyAddr) Network() string { return "tcp" }
func (dummyAddr) String() string  { return "127.0.0.1:0" }

// TestBroadcastAnnounce_SendFailure registers a session whose conn is
// closed, subscribes it, and broadcasts — the per-session write fails and
// the "announce send failed" warn arm runs.
func TestBroadcastAnnounce_SendFailure(t *testing.T) {
	srv := newServer(quietLogger(), buildServeExport())
	a, b := net.Pipe()
	_ = a.Close()
	_ = b.Close()
	sess := newSession(srv, a)
	sess.enable(codec.AN2ProtoACP2)
	srv.registerSession(sess)

	ann := &codec.ACP2Message{
		Type: codec.ACP2TypeAnnounce, MTID: 0, Func: 0, PID: codec.PIDValue,
		Body: appendObjIDIdx(10, 0, nil)}
	srv.broadcastAnnounce(1, ann) // must not panic; logs the send failure
}

// ----------------------------------------------------------------------
// session.go — run() read-error (non-EOF) path

// TestSessionRun_ReadError feeds run() a connection that returns a
// non-EOF read error (a half-closed pipe that yields garbage then errors)
// so the "session read error" warn arm executes, then run returns.
func TestSessionRun_ReadError(t *testing.T) {
	srv := newServer(quietLogger(), buildServeExport())
	a, b := net.Pipe()
	sess := newSession(srv, a)

	done := make(chan struct{})
	go func() { sess.run(); close(done) }()

	// Write a truncated/garbage AN2 frame then close so ReadAN2Frame hits
	// a decode error (non-EOF) before the conn fully closes.
	go func() {
		// Bad magic → ReadAN2Frame returns a decode error, not io.EOF.
		_, _ = b.Write([]byte{0x00, 0x00, 0, 2, 0, 0, 0, 0})
		time.Sleep(10 * time.Millisecond)
		_ = b.Close()
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		_ = a.Close()
		t.Fatal("session.run did not return after read error")
	}
}

// ----------------------------------------------------------------------
// demo.go — emitFloatAnnounce unbounded-range default arms

// TestEmitFloatAnnounce_NoConstraints covers the !hasMin / !hasMax default
// arms (min=-100, max=100) for a Float entry without declared bounds. The
// not-found / wrong-type guards and the happy + cancel paths are covered
// by demo_test.go.
func TestEmitFloatAnnounce_NoConstraints(t *testing.T) {
	str := func(s string) *string { return &s }
	freeF := &canonical.Parameter{
		Header: canonical.Header{Number: 19, Identifier: "FreeFloat",
			Access: canonical.AccessReadWrite, Children: canonical.EmptyChildren()},
		Type: canonical.ParamReal, Value: float64(0), Format: str("float"),
	}
	root := &canonical.Node{Header: canonical.Header{
		Number: 1, Identifier: "ROOT_NODE_V2", Access: canonical.AccessRead,
		Children: []canonical.Element{freeF}}}
	slot1 := &canonical.Node{Header: canonical.Header{
		Number: 1, Identifier: "slot-1", Access: canonical.AccessRead,
		Children: []canonical.Element{root}}}
	exp := &canonical.Export{Root: &canonical.Node{Header: canonical.Header{
		Number: 1, Identifier: "device", Access: canonical.AccessRead,
		Children: []canonical.Element{slot1}}}}

	srv := newServer(quietLogger(), exp)
	// obj 19 is Float with no min/max → default range (-100,100) used.
	srv.emitFloatAnnounce(1, 19, 0.5)
	e, _ := srv.tree.lookup(1, 19)
	if _, isF := e.param.Value.(float64); !isF {
		t.Errorf("unbounded float value type=%T want float64", e.param.Value)
	}
}

// ensure binary import stays referenced.
var _ = binary.BigEndian

// TestMetricsExposed pins the Metrics() accessor (#969): it is non-nil,
// stable across calls, and the connector actually counts a served
// frame — so --metrics-addr has something to scrape.
func TestMetricsExposed(t *testing.T) {
	srv := newServer(quietLogger(), buildServeExport())
	m := srv.Metrics()
	if m == nil {
		t.Fatal("Metrics() returned nil")
	}
	if srv.Metrics() != m {
		t.Fatal("Metrics() must return the same connector each call")
	}
	before := m.Snapshot().TxFrames
	// A served reply flows through session.write, which counts tx.
	sess := &session{srv: srv, conn: &nopConn{}}
	if err := sess.write(&codec.AN2Frame{
		Proto: codec.AN2ProtoACP2, Slot: 1, Type: codec.AN2TypeData, Payload: []byte{0, 0, 0, 0},
	}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := m.Snapshot().TxFrames; got != before+1 {
		t.Fatalf("tx frames = %d, want %d (write must be counted)", got, before+1)
	}
}

// nopConn is a net.Conn whose Write succeeds and discards — lets a
// session.write run without a real socket.
type nopConn struct{ net.Conn }

func (*nopConn) Write(b []byte) (int, error) { return len(b), nil }
func (*nopConn) Close() error                { return nil }
