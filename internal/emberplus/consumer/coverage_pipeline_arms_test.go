package emberplus

import (
	"context"
	"net"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"dhs/internal/consumer"
	"dhs/internal/consumer/compliance"
	"dhs/internal/emberplus/codec/glow"
	"dhs/internal/emberplus/codec/s101"
	"dhs/internal/datastore"
	"dhs/internal/transport"
)

// withUnknownCTX gives a freshPlugin the audit collector Connect would
// have installed, so the unknownCTX.record arms in the process* fns fire.
func withUnknownCTX(p *Plugin) *Plugin {
	p.unknownCTX = newUnknownCTXAudit()
	return p
}

// TestProcess_UnknownCTXArms drives the unknownCTX.record() arms in
// processNode / processParameter / processMatrix / processFunction.
func TestProcess_UnknownCTXArms(t *testing.T) {
	p := withUnknownCTX(freshPlugin())
	p.handleElements([]glow.Element{
		{Node: &glow.Node{Number: 1, Identifier: "root", UnknownContents: []int32{99}, Children: []glow.Element{
			{Parameter: &glow.Parameter{Number: 1, Identifier: "p", Type: glow.ParamTypeInteger, Value: int64(1), UnknownContents: []int32{77}}},
			{Matrix: &glow.Matrix{Number: 2, Identifier: "m", MatrixType: glow.MatrixTypeNToN, TargetCount: 2, SourceCount: 2, UnknownContents: []int32{55}}},
			{Function: &glow.Function{Number: 3, Identifier: "f", UnknownContents: []int32{33}}},
		}}},
	})
	rows := p.unknownCTX.snapshot()
	if len(rows) != 4 {
		t.Errorf("want 4 unknown-CTX rows (node/param/matrix/func), got %d: %+v", len(rows), rows)
	}
}

// TestNodeMeta_IsRoot covers the IsRoot meta arm.
func TestNodeMeta_IsRoot(t *testing.T) {
	m := nodeMeta(&glow.Node{Identifier: "root", IsRoot: true})
	if m["isRoot"] != true {
		t.Errorf("nodeMeta isRoot = %v, want true", m["isRoot"])
	}
}

// TestMatrixMeta_ParametersLocationSlice covers the []int32
// ParametersLocation arm of matrixMeta.
func TestMatrixMeta_ParametersLocationSlice(t *testing.T) {
	m := matrixMeta(&glow.Matrix{
		Identifier:         "m",
		MatrixType:         glow.MatrixTypeNToN,
		ParametersLocation: []int32{1, 2, 3},
	})
	if m["parametersLocation"] != "1.2.3" {
		t.Errorf("parametersLocation = %v, want 1.2.3", m["parametersLocation"])
	}
}

// TestProcessParameter_AccessWrite covers the AccessWrite â†’ obj.Access=2
// switch arm.
func TestProcessParameter_AccessWrite(t *testing.T) {
	p := freshPlugin()
	p.handleElements([]glow.Element{
		{Parameter: &glow.Parameter{Number: 1, Identifier: "wp", Type: glow.ParamTypeInteger,
			Access: glow.AccessWrite, Value: int64(3)}},
	})
	e := p.numIndex["1"]
	if e == nil || e.obj.Access != 2 {
		t.Errorf("write-only param Access = %v, want 2", e)
	}
}

// TestProcessParameter_DescriptionOnlyRestore covers the
// announce-with-no-value path that restores the prior live value.
func TestProcessParameter_DescriptionOnlyRestore(t *testing.T) {
	p := freshPlugin()
	// First sighting carries a real value.
	p.handleElements([]glow.Element{
		{Parameter: &glow.Parameter{Number: 1, Identifier: "v", Type: glow.ParamTypeInteger, Value: int64(42)}},
	})
	// Second announce: NO value on the wire, only a description change.
	p.handleElements([]glow.Element{
		{Parameter: &glow.Parameter{Number: 1, Identifier: "v", Description: "changed"}},
	})
	e := p.numIndex["1"]
	if e == nil || e.obj.Value.Int != 42 {
		t.Errorf("description-only announce lost the live value: %+v", e)
	}
}

// TestProcessParameter_StreamIDCollision covers the shared-streamId-
// without-descriptor compliance arm.
func TestProcessParameter_StreamIDCollision(t *testing.T) {
	p := freshPlugin()
	// First param registers streamId 5 (no descriptor).
	p.handleElements([]glow.Element{
		{Parameter: &glow.Parameter{Number: 1, Identifier: "a", Type: glow.ParamTypeInteger,
			HasStreamIdentifier: true, StreamIdentifier: 5}},
	})
	// Second param re-uses streamId 5 with no StreamDescriptor â†’ collision.
	p.handleElements([]glow.Element{
		{Parameter: &glow.Parameter{Number: 2, Identifier: "b", Type: glow.ParamTypeInteger,
			HasStreamIdentifier: true, StreamIdentifier: 5}},
	})
	if p.profile.Snapshot()[StreamIDCollisionNoDescriptor] == 0 {
		t.Error("expected StreamIDCollisionNoDescriptor compliance event")
	}
}

// TestProcessParameter_DualFireSuppression covers notifySubscribers'
// skip-when-unchanged arm. Stream dispatch (deliverStreamValue) fires
// notifySubscribers on the SAME persistent entry; delivering the same
// value twice fires once then suppresses the redundant second (no diff +
// rendered matches lastEmittedRendered).
func TestProcessParameter_DualFireSuppression(t *testing.T) {
	p := freshPlugin()
	fires := 0
	p.subs["*"] = func(consumer.Event) { fires++ }
	entry := &treeEntry{
		numericPath: []int32{1},
		glowParam:   &glow.Parameter{Number: 1, Identifier: "v", Type: glow.ParamTypeInteger, Value: int64(0)},
		obj:         consumer.Object{OID: "1", Kind: consumer.KindInt},
	}
	p.numIndex["1"] = entry
	// First stream value 7 â†’ changed â†’ fires.
	p.deliverStreamValue(entry, int64(7))
	first := fires
	if first == 0 {
		t.Fatal("first stream delivery should fire")
	}
	// Same value 7 again â†’ no diff + rendered matches â†’ suppressed.
	p.deliverStreamValue(entry, int64(7))
	if fires != first {
		t.Errorf("redundant identical stream value should be suppressed: fires went %d â†’ %d", first, fires)
	}
}

// TestProcessMatrix_AnnounceDeltaNotifies covers processMatrix's
// non-initial branch (announced connection â†’ notifyMatrixSubscribers).
func TestProcessMatrix_AnnounceDeltaNotifies(t *testing.T) {
	p := freshPlugin()
	got := make(chan consumer.Event, 4)
	p.subs["*"] = func(e consumer.Event) {
		select {
		case got <- e:
		default:
		}
	}
	mat := func(conns []glow.Connection) *glow.Matrix {
		return &glow.Matrix{Number: 1, Identifier: "m", MatrixType: glow.MatrixTypeNToN,
			TargetCount: 4, SourceCount: 4, Connections: conns}
	}
	// First sighting (initial) â€” target 0 routed from source 0; does NOT notify.
	p.handleElements([]glow.Element{{Matrix: mat([]glow.Connection{{Target: 0, Sources: []int32{0}}})}})
	// Second processMatrix â€” target 0 (already KNOWN) reroutes 0 -> 2: a real
	// change, fires notifyMatrixSubscribers. (A brand-new target would be
	// treated as initial population and stay silent.)
	p.handleElements([]glow.Element{{Matrix: mat([]glow.Connection{{Target: 0, Sources: []int32{2}, Disposition: glow.ConnDispLocked}})}})
	select {
	case e := <-got:
		if e.MatrixChange == nil {
			t.Error("expected MatrixChange payload on announce delta")
		}
	case <-time.After(time.Second):
		t.Error("no matrix change event fired on announce delta")
	}
}

// TestProcessMatrix_EmptyConnGetDirectory covers the matrix-with-no-
// connections branch that fires a background SendMatrixGetDirectory when
// a session is attached.
func TestProcessMatrix_EmptyConnGetDirectory(t *testing.T) {
	s, cleanup := pipeSession(t)
	defer cleanup()
	p := freshPlugin()
	p.session = s
	p.handleElements([]glow.Element{
		{Matrix: &glow.Matrix{Number: 1, Identifier: "m", MatrixType: glow.MatrixTypeNToN,
			TargetCount: 2, SourceCount: 2}}, // no Connections â†’ triggers GetDirectory
	})
	// The GetDirectory is sent in a goroutine; give it a moment to run so
	// the branch executes (drained by the pipe reader).
	time.Sleep(50 * time.Millisecond)
}

// TestNotifyMatrixSubscribers_NilGuards covers the nil-entry / nil-matrix
// early return.
func TestNotifyMatrixSubscribers_NilGuards(t *testing.T) {
	p := freshPlugin()
	p.notifyMatrixSubscribers(nil, glow.Connection{})
	p.notifyMatrixSubscribers(&treeEntry{}, glow.Connection{}) // nil glowMatrix
}

// TestProcessFunction_Children covers the function child-recurse loop.
func TestProcessFunction_Children(t *testing.T) {
	p := freshPlugin()
	p.handleElements([]glow.Element{
		{Function: &glow.Function{Number: 1, Identifier: "f", Children: []glow.Element{
			{Parameter: &glow.Parameter{Number: 1, Identifier: "arg", Type: glow.ParamTypeInteger, Value: int64(1)}},
		}}},
	})
	if _, ok := p.numIndex["1.1"]; !ok {
		t.Error("function child parameter not indexed at 1.1")
	}
}

// TestUnknownCTX_DuplicateOIDDedup covers the dedup branch in
// unknownCTXAudit.record where the same OID is seen twice for one tag.
func TestUnknownCTX_DuplicateOIDDedup(t *testing.T) {
	a := newUnknownCTXAudit()
	a.record("node", []int32{1, 2}, []int32{99})
	a.record("node", []int32{1, 2}, []int32{99}) // same OID + tag â†’ deduped
	rows := a.snapshot()
	if len(rows) != 1 || rows[0].Count != 2 || len(rows[0].SampleOIDs) != 1 {
		t.Errorf("dedup failed: %+v", rows)
	}
}

// TestSeedOneEntry_Template covers the "template" case (returns nil; the
// entry is not added to numIndex â€” templates seed via a separate path).
func TestSeedOneEntry_Template(t *testing.T) {
	p := freshPlugin()
	p.SeedTreeFromCachedObjects(0, []consumer.Object{
		{OID: "5", Label: "tmpl", Meta: map[string]any{"element": "template", "number": int64(5)}},
	})
	// Template must NOT land in numIndex.
	if _, ok := p.numIndex["5"]; ok {
		t.Error("template object should not be indexed in numIndex")
	}
}

// ---- session.go arms -------------------------------------------------

// TestSession_ConnectDialError covers the dial-error wrap in
// Session.Connect (point at a closed port).
func TestSession_ConnectDialError(t *testing.T) {
	s := NewSession(discardLogger())
	s.SetProfile(&compliance.Profile{})
	// Port 1 on loopback: nothing listens â†’ dial fails fast.
	if err := s.Connect(context.Background(), "127.0.0.1", 1); err == nil {
		t.Error("Connect to a closed port should error")
	}
}

// TestSession_ConnectWithRecorder covers the recorder-tap install branch
// of Session.Connect and Plugin.Connect.
func TestSession_ConnectWithRecorder(t *testing.T) {
	addr, stop := startLoopbackProvider(t)
	defer stop()
	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)

	p := (&Factory{}).New(discardLogger()).(*Plugin)
	// Attach a recorder so Plugin.Connect's recorder!=nil arm + the
	// session writer/reader SetTap arms execute.
	rec, err := transport.NewRecorder(filepath.Join(t.TempDir(), "cap.jsonl"))
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}
	defer func() { _ = rec.Close() }()
	p.SetRecorder(rec)
	if err := p.Connect(context.Background(), host, port); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = p.Disconnect() }()
	if _, err := p.Walk(context.Background(), 0); err != nil {
		t.Fatalf("Walk: %v", err)
	}
}

// TestReadLoop_DecodeAndKindArms drives readLoop's malformed-glow decode
// arm (FlagSingle frame carrying junk BER), the middle-fragment
// accumulate arm (Flags=0 with a non-empty payload), the non-EmBER frame
// arm (raw frame whose msgType != MsgEmBER), and the keep-alive-response
// write path â€” by feeding crafted frames over a raw pipe.
func TestReadLoop_DecodeAndKindArms(t *testing.T) {
	s, w, r, cleanup := wireSession(t, time.Hour, time.Hour)
	defer cleanup()

	go s.readLoop()
	go func() {
		for {
			if _, err := r.ReadFrame(); err != nil {
				return
			}
		}
	}()

	// 1) Single EmBER frame with a non-decodable BER payload â†’ glow decode
	//    error arm (continue, no element callback).
	bad := &s101.Frame{
		Slot: s101.SlotDefault, MsgType: s101.MsgEmBER, Command: s101.CmdEmBER,
		Version: s101.VersionS101, Flags: s101.FlagSingle, DTD: s101.DTDGlow,
		Payload: []byte{0xFF, 0xFF, 0xFF}, // not a valid Glow root
	}
	if err := w.WriteFrame(bad); err != nil {
		t.Fatalf("write bad frame: %v", err)
	}

	// 2) Multi-frame with a MIDDLE fragment carrying a non-empty payload
	//    â†’ the default (accumulate) arm of the reassembly switch (565-569),
	//    which the empty-middle case in TestReadLoop_FrameKinds skips at
	//    the len(payload)==0 guard.
	payload := glow.EncodeGetDirectory()
	third := len(payload) / 3
	if third == 0 {
		third = 1
	}
	frag := func(flags byte, body []byte) {
		if err := w.WriteFrame(&s101.Frame{
			Slot: s101.SlotDefault, MsgType: s101.MsgEmBER, Command: s101.CmdEmBER,
			Version: s101.VersionS101, Flags: flags, DTD: s101.DTDGlow, Payload: body,
		}); err != nil {
			t.Fatalf("write frag: %v", err)
		}
	}
	frag(s101.FlagFirst, payload[:third])
	frag(0, payload[third:2*third]) // middle WITH payload â†’ accumulate arm
	frag(s101.FlagLast, payload[2*third:])

	// 3) Keep-alive request â†’ readLoop writes a response (resp-write arm).
	if err := w.WriteFrame(s101.NewKeepAliveRequest()); err != nil {
		t.Fatalf("write keepalive: %v", err)
	}

	// Give the read loop time to process all frames.
	time.Sleep(150 * time.Millisecond)
}

// TestReadLoop_NonEmberFrame covers the !frame.IsEmBER() continue arm: a
// raw frame whose msgType byte (offset 1) is neither MsgEmBER (0x0E) nor
// a keepalive â€” decoded by the reader, then dropped by readLoop. The
// s101.Writer always emits MsgEmBER, so we write the raw bytes straight
// to the client end of the pipe.
func TestReadLoop_NonEmberFrame(t *testing.T) {
	srvConn, cliConn := net.Pipe()
	s := NewSession(discardLogger())
	s.SetProfile(&compliance.Profile{})
	s.mu.Lock()
	s.conn = srvConn
	s.reader = s101.NewReader(srvConn)
	s.writer = s101.NewWriter(srvConn)
	s.closed = false
	s.keepAliveDone = make(chan struct{})
	s.deadManDone = make(chan struct{})
	s.mu.Unlock()
	s.touchRX()
	defer func() { _ = s.Disconnect(); _ = cliConn.Close() }()

	go s.readLoop()

	// msgType=0x77 (not EmBER), non-keepalive command 0x42, plus flags so
	// Decode succeeds with content length >= 5.
	rawNonEmber := wrapS101([]byte{0x00, 0x77, 0x42, 0x01, 0xC0})
	done := make(chan struct{})
	go func() {
		_, _ = cliConn.Write(rawNonEmber)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("raw write to pipe blocked")
	}
	time.Sleep(100 * time.Millisecond)
}

// TestDeadManLoop_QuietContinue covers deadManLoop's age==0 continue:
// lastRX is zero (never touched) so rxAge() returns 0 and the loop
// continues without firing. The minimum tick is 1s, so we must wait
// past one tick before signalling exit.
func TestDeadManLoop_QuietContinue(t *testing.T) {
	s := NewSession(discardLogger())
	s.SetProfile(&compliance.Profile{})
	s.deadManThreshold = 30 * time.Millisecond // tick clamps to 1s minimum
	s.deadManDone = make(chan struct{})
	// Do NOT touchRX â†’ lastRX stays zero â†’ rxAge()==0 â†’ continue arm.
	done := make(chan struct{})
	go func() { s.deadManLoop(); close(done) }()
	time.Sleep(1200 * time.Millisecond) // let one 1s tick hit the age==0 path
	close(s.deadManDone)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("deadManLoop did not exit after deadManDone closed")
	}
}

// TestSession_DisconnectClosesConn covers Disconnect's conn.Close() return
// arm and the top-of-readLoop closed-guard return (497) by wiring a
// session over a pipe, starting readLoop, then disconnecting.
func TestSession_DisconnectClosesConn(t *testing.T) {
	s, _, r, cleanup := wireSession(t, time.Hour, time.Hour)
	defer cleanup()
	go s.readLoop()
	go func() {
		for {
			if _, err := r.ReadFrame(); err != nil {
				return
			}
		}
	}()
	time.Sleep(20 * time.Millisecond)
	if err := s.Disconnect(); err != nil {
		t.Errorf("Disconnect: %v", err)
	}
	// A second Disconnect is a no-op (closed already).
	if err := s.Disconnect(); err != nil {
		t.Errorf("second Disconnect: %v", err)
	}
}

// TestWaitForDtdVersion_NilReady covers the ready==nil early return.
func TestWaitForDtdVersion_NilReady(t *testing.T) {
	s := &Session{logger: discardLogger()} // dtdReady is nil
	if got := s.WaitForDtdVersion(context.Background()); got != "" {
		t.Errorf("nil dtdReady â†’ want empty, got %q", got)
	}
}

// TestSplitAndPersistDM_DegenerateAndError drives the SplitAndPersistDM
// arms the happy-path test misses: a matrix-at-root (Root is not a Node)
// and a non-Node root child skip.
func TestSplitAndPersistDM_MatrixAtRoot(t *testing.T) {
	store := datastore.NewTreeStore(t.TempDir())
	p := freshPlugin()
	// A lone Matrix entry at root OID "1" â†’ ExportCanonical yields a
	// canonical.Matrix as Root (not a Node) â†’ degenerate single-DM path.
	seedEntry(p, []int32{1}, "soloMatrix", func(e *treeEntry) {
		e.glowMatrix = &glow.Matrix{Number: 1, Identifier: "soloMatrix",
			MatrixType: glow.MatrixTypeNToN, TargetCount: 1, SourceCount: 1}
	})
	refs, err := p.SplitAndPersistDM(context.Background(), store, "Solo@1.0")
	if err != nil {
		t.Fatalf("SplitAndPersistDM matrix-at-root: %v", err)
	}
	if len(refs) != 1 {
		t.Errorf("matrix-at-root should yield exactly one DM, got %+v", refs)
	}
}
