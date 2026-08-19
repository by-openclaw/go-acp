package acp2

import (
	"context"
	"net"
	"testing"
	"time"

	"dhs/internal/acp2/codec"
	"dhs/internal/consumer"
	"dhs/internal/export/canonical"
)

// coverage_batch2_final_test.go closes the last reachable branches across
// canonicalize.go, session.go handshake/DoACP2 edges, keepalive prober
// failure, walker mid-walk cancellation, plugin float decode + SetValue
// resolve error, reconnect guards, value_validate resolve error, and the
// diag nil-logger + error-reply arms.

// ----------------------------------------------------------------------
// canonicalize.go — ctx cancel, nil cache entry, rich-parameter
// constraints, leaf node (sortChildrenDeep base), placeholder chain.

// TestCanonicalize_CtxCanceled covers the ctx.Err early-return.
func TestCanonicalize_CtxCanceled(t *testing.T) {
	p := &Plugin{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Canonicalize(ctx); err == nil {
		t.Fatal("Canonicalize with cancelled ctx should error")
	}
}

// TestCanonicalize_NilEntryTreeSkipped covers the entry/tree nil-skip arm:
// a cache slot whose tree is nil is skipped.
func TestCanonicalize_NilEntryTreeSkipped(t *testing.T) {
	p := &Plugin{}
	p.trees = newWalkedTreeCache(8, 0)
	p.trees.Put(0, &WalkedTree{Slot: 0}) // empty tree (no objects) → no slot node
	out, err := p.Canonicalize(context.Background())
	if err != nil {
		t.Fatalf("Canonicalize: %v", err)
	}
	// An object-less tree yields a slot node with only the empty root anchor;
	// the important part is the nil-guard path executed without panic.
	if out == nil || out.Root == nil {
		t.Fatal("nil export")
	}
}

// TestCanonicalize_RichParameterAndLeafNode covers buildACP2Parameter's
// min/max/step/default/unit arms, the Node-container place arm, the
// existing-node upgrade arm (a Node whose path was pre-materialised by
// ensureACP2Chain), and sortChildrenDeep's childless-node base case.
func TestCanonicalize_RichParameterAndLeafNode(t *testing.T) {
	unit := "dB"
	tree := &WalkedTree{
		Slot: 0,
		Objects: []consumer.Object{
			// Root node.
			{Slot: 0, ID: 1, Label: "ROOT", Path: []string{"ROOT"},
				Kind: consumer.KindRaw, Access: 1},
			// A numeric leaf with every constraint set + a unit.
			{Slot: 0, ID: 10, Label: "Gain",
				Path: []string{"ROOT", "GRP", "Gain"}, // GRP auto-materialised
				Kind: consumer.KindInt, Access: 3,
				Value: consumer.Value{Kind: consumer.KindInt, Int: 0},
				Min:   int64(-10), Max: int64(10), Step: int64(1), Def: int64(0),
				Unit:  unit},
			// A Node object whose path == GRP — GRP was already materialised
			// as a placeholder by the leaf above → exercises the
			// existing-node upgrade arm.
			{Slot: 0, ID: 200, Label: "GRP", Path: []string{"ROOT", "GRP"},
				Kind: consumer.KindRaw, Access: 3},
			// A childless leaf Node → sortChildrenDeep recurses into it and
			// hits the len(Children)==0 base case.
			{Slot: 0, ID: 300, Label: "EMPTY", Path: []string{"ROOT", "EMPTY"},
				Kind: consumer.KindRaw, Access: 1},
		},
		ObjTypes: []codec.ACP2ObjType{
			codec.ObjTypeNode, codec.ObjTypeNumber, codec.ObjTypeNode, codec.ObjTypeNode,
		},
		NumTypes: []codec.NumberType{0, codec.NumTypeS32, 0, 0},
	}
	p := &Plugin{}
	p.trees = newWalkedTreeCache(8, 0)
	p.trees.Put(0, tree)

	out, err := p.Canonicalize(context.Background())
	if err != nil {
		t.Fatalf("Canonicalize: %v", err)
	}
	// Walk down to the Gain parameter and verify constraints round-tripped.
	slot0 := out.Root.Common().Children[0].(*canonical.Node)
	root := slot0.Children[0].(*canonical.Node)
	var grp *canonical.Node
	for _, c := range root.Children {
		if n, ok := c.(*canonical.Node); ok && n.Identifier == "GRP" {
			grp = n
		}
	}
	if grp == nil {
		t.Fatal("GRP node not found")
	}
	// GRP's Number should have been upgraded from 0 (placeholder) to 200.
	if grp.Number != 200 {
		t.Errorf("GRP Number = %d, want 200 (upgraded)", grp.Number)
	}
	var gain *canonical.Parameter
	for _, c := range grp.Children {
		if pr, ok := c.(*canonical.Parameter); ok && pr.Identifier == "Gain" {
			gain = pr
		}
	}
	if gain == nil {
		t.Fatal("Gain parameter not found")
	}
	if gain.Minimum == nil || gain.Maximum == nil || gain.Step == nil ||
		gain.Default == nil || gain.Unit == nil || *gain.Unit != "dB" {
		t.Errorf("Gain constraints not all set: %+v", gain)
	}
}

// TestCanonicalize_NilTreeEntry covers the entry.tree==nil skip arm by
// pushing a cache entry whose tree is nil directly into the LRU.
func TestCanonicalize_NilTreeEntry(t *testing.T) {
	p := &Plugin{}
	c := newWalkedTreeCache(8, 0)
	// Push a treeCacheEntry with a nil tree straight into the cache.
	el := c.order.PushFront(&treeCacheEntry{slot: 0, tree: nil})
	c.entries[0] = el
	p.trees = c

	out, err := p.Canonicalize(context.Background())
	if err != nil {
		t.Fatalf("Canonicalize: %v", err)
	}
	// nil-tree slot is skipped → no slot children.
	if len(out.Root.Common().Children) != 0 {
		t.Errorf("nil-tree entry produced %d slot children, want 0",
			len(out.Root.Common().Children))
	}
}

// TestPlaceACP2Object_EmptyPath covers placeACP2Object's empty-Path guard.
func TestPlaceACP2Object_EmptyPath(t *testing.T) {
	nodeByPath := map[string]*canonical.Node{"": {}}
	// An object with no Path → early return, nothing attached.
	placeACP2Object(0, "1.1", "slot-0",
		consumer.Object{ID: 1, Path: nil}, codec.ObjTypeNumber, codec.NumTypeS32, nodeByPath)
	if len(nodeByPath[""].Children) != 0 {
		t.Errorf("empty-path object attached %d children, want 0",
			len(nodeByPath[""].Children))
	}
}

// ----------------------------------------------------------------------
// session.go — handshake GetDeviceInfo error + ACP2 GetVersion error,
// DoACP2 done/nil, an2Request done/nil, readLoop EOF.

// raw helper local to this file: complete N AN2 internal handshake steps
// then optionally answer ACP2, controlled by the caller.
func handshakeListener(t *testing.T, an2Steps int, acp2 func(c net.Conn, f *codec.AN2Frame)) (string, int, func()) {
	return rawListener(t, func(c net.Conn) {
		defer func() { _ = c.Close() }()
		_ = c.SetDeadline(time.Now().Add(3 * time.Second))
		an2Count := 0
		for {
			f, err := codec.ReadAN2Frame(c)
			if err != nil {
				return
			}
			switch f.Proto {
			case codec.AN2ProtoInternal:
				if an2Count >= an2Steps {
					return // stop answering → next request fails
				}
				an2Count++
				fn := f.Payload[0]
				var payload []byte
				switch fn {
				case codec.AN2FuncGetVersion:
					payload = []byte{fn, 1, 0}
				case codec.AN2FuncGetDeviceInfo:
					payload = []byte{fn, 1}
				case codec.AN2FuncGetSlotInfo:
					payload = []byte{fn, 2, 1, byte(codec.AN2ProtoACP2)}
				case codec.AN2FuncEnableProtocolEvents:
					payload = []byte{fn}
				default:
					continue
				}
				_ = writeAN2(c, &codec.AN2Frame{Proto: codec.AN2ProtoInternal,
					Slot: f.Slot, MTID: f.MTID, Type: codec.AN2TypeReply, Payload: payload})
			case codec.AN2ProtoACP2:
				if acp2 != nil {
					acp2(c, f)
				}
			}
		}
	})
}

// TestConnect_GetDeviceInfoCloses covers the GetDeviceInfo handshake step's
// error arm: the server answers GetVersion then closes, so GetDeviceInfo
// fails and Connect aborts via closeLocked.
func TestConnect_GetDeviceInfoCloses(t *testing.T) {
	host, port, stop := handshakeListener(t, 1, nil) // answer only GetVersion
	defer stop()

	s := NewSession(testLogger())
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	if err := s.Connect(ctx, host, port); err == nil {
		_ = s.Disconnect()
		t.Fatal("Connect should fail when GetDeviceInfo is unanswered")
	}
}

// TestConnect_ACP2GetVersionCloses covers the ACP2 GetVersion handshake step
// error: all four AN2 steps succeed, but the ACP2 get_version is never
// answered (server closes), so DoACP2 returns "connection closed".
func TestConnect_ACP2GetVersionCloses(t *testing.T) {
	// 5 AN2 internal steps: GetVersion, GetDeviceInfo, GetSlotInfo(0),
	// GetSlotInfo(1), EnableProtocolEvents — then the ACP2 get_version
	// request arrives and we hang up on it.
	host, port, stop := handshakeListener(t, 5, func(c net.Conn, f *codec.AN2Frame) {
		_ = c.Close() // hang up on the ACP2 get_version request
	})
	defer stop()

	s := NewSession(testLogger())
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	if err := s.Connect(ctx, host, port); err == nil {
		_ = s.Disconnect()
		t.Fatal("Connect should fail when ACP2 GetVersion is unanswered")
	}
}

// TestReadLoop_EOFOnClose covers readLoop's EOF arm: a clean server-side
// close after the handshake yields io.EOF on the next read.
func TestReadLoop_EOFOnClose(t *testing.T) {
	srv, host, port := newFakeServer(t)
	p := connectPlugin(t, srv, host, port)

	// Close the server connection cleanly → client readLoop sees EOF.
	srv.mu.Lock()
	for _, c := range srv.conns {
		_ = c.Close()
	}
	srv.mu.Unlock()

	p.mu.Lock()
	s := p.session
	p.mu.Unlock()
	// Wait for the session's done channel to close (readLoop returned).
	select {
	case <-s.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("readLoop did not return after server close")
	}
	srv.stop()
}

// ----------------------------------------------------------------------
// keepalive.go — prober probe-failure (server stops answering keepalive)

// TestKeepAlive_ProberProbeFailure connects with a fast prober, then closes
// the connection so the prober's an2Request errors (the probe-failed
// continue arm) before the session fully tears down.
func TestKeepAlive_ProberProbeFailure(t *testing.T) {
	srv, host, port := newFakeServer(t)

	p := &Plugin{logger: testLogger()}
	p.SetKeepAlive(consumer.KeepAliveConfig{
		Interval: 15 * time.Millisecond,
		Timeout:  consumer.DisableTimeout,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := p.Connect(ctx, host, port); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = p.Disconnect() }()

	// Drop the server-side conn so the next keepalive probe's an2Request
	// fails (connection closed) → the prober's probe-failed continue arm.
	srv.mu.Lock()
	for _, c := range srv.conns {
		_ = c.Close()
	}
	srv.mu.Unlock()
	// Give the prober a couple of ticks against the dead conn.
	time.Sleep(60 * time.Millisecond)
	srv.stop()
}

// ----------------------------------------------------------------------
// plugin.go — GetValue float decode (decodePropertyValue float arm),
// SetValue resolveRequest error (label, no tree).

// TestGetValue_Float covers decodePropertyValue's KindFloat arm via a
// no-tree float get.
func TestGetValue_Float(t *testing.T) {
	srv, host, port := newFakeServer(t)
	srv.onACP2 = func(slot uint8, req *codec.ACP2Message) *codec.ACP2Message {
		switch req.Func {
		case codec.ACP2FuncGetObject:
			return objectReply(7, []codec.Property{
				u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeNumber)),
				u32Prop(codec.PIDNumberType, 0, uint32(codec.NumTypeFloat)),
			})
		case codec.ACP2FuncGetProperty:
			return propertyReply(codec.ACP2FuncGetProperty, 7,
				codec.MakeValueProperty(codec.PIDValue, codec.NumTypeFloat, f32be(2.5)))
		}
		return errorReply(codec.ErrProtocol)
	}
	defer srv.stop()

	p := connectPlugin(t, srv, host, port)
	val, err := p.GetValue(context.Background(), consumer.ValueRequest{Slot: 0, ID: 7})
	if err != nil {
		t.Fatalf("GetValue: %v", err)
	}
	if val.Kind != consumer.KindFloat || val.Float < 2.4 || val.Float > 2.6 {
		t.Errorf("value = %+v, want float ~2.5", val)
	}
}

// TestSetValue_ResolveError covers SetValue's resolveRequest error arm:
// a label-addressed set with no walked tree → ErrUnknownLabel.
func TestSetValue_ResolveError(t *testing.T) {
	srv, host, port := newFakeServer(t)
	defer srv.stop()

	p := connectPlugin(t, srv, host, port)
	_, err := p.SetValue(context.Background(),
		consumer.ValueRequest{Slot: 0, Label: "NoTreeLabel"},
		consumer.Value{Kind: consumer.KindInt, Int: 1})
	if err == nil {
		t.Fatal("SetValue by label with no tree should error")
	}
}

// TestValidateValue_ResolveError covers ValidateValue's resolveRequest error
// arm (label, no tree).
func TestValidateValue_ResolveError(t *testing.T) {
	srv, host, port := newFakeServer(t)
	defer srv.stop()

	p := connectPlugin(t, srv, host, port)
	err := p.ValidateValue(context.Background(),
		consumer.ValueRequest{Slot: 0, Label: "NoTreeLabel"},
		consumer.Value{Kind: consumer.KindInt, Int: 1})
	if err == nil {
		t.Fatal("ValidateValue by label with no tree should error")
	}
}

// ----------------------------------------------------------------------
// walker.go — WalkIdentity + walkObject mid-walk cancellation arms.

// TestWalk_CancelMidChildren cancels the context after the root object is
// fetched (via OnProgress) so walkObject's mid-children ctx.Err bail runs.
func TestWalk_CancelMidChildren(t *testing.T) {
	srv, host, port := newFakeServer(t)
	srv.onACP2 = func(slot uint8, req *codec.ACP2Message) *codec.ACP2Message {
		if req.Func != codec.ACP2FuncGetObject {
			return errorReply(codec.ErrProtocol)
		}
		if req.ObjID == 1 {
			return objectReply(1, []codec.Property{
				u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeNode)),
				codec.MakeStringProperty(codec.PIDLabel, "ROOT"),
				childrenProp(2, 3, 4),
			})
		}
		return objectReply(req.ObjID, []codec.Property{
			u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeNumber)),
			u32Prop(codec.PIDNumberType, 0, uint32(codec.NumTypeU32)),
		})
	}
	defer srv.stop()

	p := connectPlugin(t, srv, host, port)
	ctx, cancel := context.WithCancel(context.Background())
	// Cancel as soon as the root (first object) is reported, so the
	// mid-children loop sees ctx.Err on the next child.
	p.SetWalkProgress(func(count int, obj *consumer.Object) {
		if count == 1 {
			cancel()
		}
	})
	_, err := p.Walk(ctx, 1)
	if err == nil {
		t.Fatal("Walk cancelled mid-children should return ctx error")
	}
}

// TestWalkIdentity_CancelMidChildren cancels mid-WalkIdentity so the child /
// leaf ctx.Err loops fire. The first getOne (root) succeeds; cancellation
// happens before the child loop processes its entries.
func TestWalkIdentity_CancelMidChildren(t *testing.T) {
	gateClose := make(chan struct{})
	var cancel context.CancelFunc
	srv, host, port := newFakeServer(t)
	srv.onACP2 = func(slot uint8, req *codec.ACP2Message) *codec.ACP2Message {
		if req.Func != codec.ACP2FuncGetObject {
			return errorReply(codec.ErrProtocol)
		}
		switch req.ObjID {
		case 1:
			// After answering root, cancel so the child loop bails.
			if cancel != nil {
				cancel()
			}
			close(gateClose)
			return objectReply(1, []codec.Property{
				u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeNode)),
				codec.MakeStringProperty(codec.PIDLabel, "ROOT_NODE_V2"),
				childrenProp(2, 3),
			})
		}
		return objectReply(req.ObjID, []codec.Property{
			u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeNode)),
			codec.MakeStringProperty(codec.PIDLabel, "IDENTITY"),
			childrenProp(10),
		})
	}
	defer srv.stop()

	p := connectPlugin(t, srv, host, port)
	ctx, c := context.WithCancel(context.Background())
	cancel = c
	_, err := p.IdentityProbe(ctx, 1)
	<-gateClose
	if err == nil {
		t.Fatal("IdentityProbe cancelled mid-walk should error")
	}
}

// TestWalkIdentity_CtxErrInLoops deterministically covers WalkIdentity's
// child-loop and leaf-loop ctx.Err guards. The server cancels the context
// when it receives the first CHILD request (obj 2), so:
//   - the leaf loop inside IDENTITY (obj 2) sees ctx cancelled (127), and
//   - the next child-loop iteration (obj 3) sees ctx cancelled (114).
func TestWalkIdentity_CtxErrInLoops(t *testing.T) {
	var cancel context.CancelFunc
	srv, host, port := newFakeServer(t)
	srv.onACP2 = func(slot uint8, req *codec.ACP2Message) *codec.ACP2Message {
		if req.Func != codec.ACP2FuncGetObject {
			return errorReply(codec.ErrProtocol)
		}
		switch req.ObjID {
		case 1:
			return objectReply(1, []codec.Property{
				u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeNode)),
				codec.MakeStringProperty(codec.PIDLabel, "ROOT_NODE_V2"),
				childrenProp(2, 3),
			})
		case 2:
			// IDENTITY with two leaves so the leaf loop runs twice.
			return objectReply(2, []codec.Property{
				u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeNode)),
				codec.MakeStringProperty(codec.PIDLabel, "IDENTITY"),
				childrenProp(10, 11),
			})
		case 10:
			// First leaf fetched → cancel now. getOne(10) then errors mid-wait
			// (continue, 131); the next leaf-loop iteration (obj 11) observes
			// ctx cancelled at its top check (127). After IDENTITY returns
			// nil,err, the outer WalkIdentity surfaces the cancellation; the
			// next child-loop iteration (obj 3) would also hit 114.
			if cancel != nil {
				cancel()
			}
			return objectReply(10, []codec.Property{
				u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeString)),
				codec.MakeStringProperty(codec.PIDLabel, "Card Name"),
				codec.MakeStringProperty(codec.PIDValue, "Hybrid"),
			})
		}
		return objectReply(req.ObjID, []codec.Property{
			u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeNode)),
			codec.MakeStringProperty(codec.PIDLabel, "X"),
		})
	}
	defer srv.stop()

	p := connectPlugin(t, srv, host, port)
	ctx, c := context.WithCancel(context.Background())
	cancel = c
	if _, err := p.IdentityProbe(ctx, 1); err == nil {
		t.Fatal("IdentityProbe cancelled mid-loops should error")
	}
}

// TestWalkIdentity_CtxErrChildLoop covers WalkIdentity's child-loop ctx.Err
// guard (114): root has two children; both are non-IDENTITY/BOARD so neither
// descends. The server cancels after the first child's get_object reply, so
// the second child-loop iteration observes ctx cancelled at its top check.
func TestWalkIdentity_CtxErrChildLoop(t *testing.T) {
	var cancel context.CancelFunc
	srv, host, port := newFakeServer(t)
	srv.onACP2 = func(slot uint8, req *codec.ACP2Message) *codec.ACP2Message {
		if req.Func != codec.ACP2FuncGetObject {
			return errorReply(codec.ErrProtocol)
		}
		switch req.ObjID {
		case 1:
			return objectReply(1, []codec.Property{
				u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeNode)),
				codec.MakeStringProperty(codec.PIDLabel, "ROOT_NODE_V2"),
				childrenProp(2, 3),
			})
		case 2:
			// First child fetched (not BOARD/IDENTITY → skipped). Cancel so
			// the next child-loop iteration (obj 3) hits the 114 ctx check.
			if cancel != nil {
				cancel()
			}
			return objectReply(2, []codec.Property{
				u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeNode)),
				codec.MakeStringProperty(codec.PIDLabel, "Audio"),
				childrenProp(20),
			})
		}
		return objectReply(req.ObjID, []codec.Property{
			u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeNode)),
			codec.MakeStringProperty(codec.PIDLabel, "Video"),
		})
	}
	defer srv.stop()

	p := connectPlugin(t, srv, host, port)
	ctx, c := context.WithCancel(context.Background())
	cancel = c
	if _, err := p.IdentityProbe(ctx, 1); err == nil {
		t.Fatal("IdentityProbe cancelled in child loop should error")
	}
}

// ----------------------------------------------------------------------
// reconnect.go — reconnectLoop s==nil guard.

// TestReconnectLoop_NilSession covers reconnectLoop's s==nil early return by
// driving it on a Plugin whose session is nil (rc set so the goroutine has
// a done channel, but session never assigned).
func TestReconnectLoop_NilSession(t *testing.T) {
	p := &Plugin{logger: testLogger()}
	p.rc = &reconnectState{done: make(chan struct{})}
	p.rc.stopped.Add(1)
	done := make(chan struct{})
	go func() { p.reconnectLoop(); close(done) }()
	select {
	case <-done: // returns immediately because p.session == nil
	case <-time.After(time.Second):
		close(p.rc.done)
		t.Fatal("reconnectLoop with nil session did not return")
	}
}

// ----------------------------------------------------------------------
// diag.go — nil logger default + ACP2 error-reply arm.

// TestRunDiagnostics_NilLoggerDialError covers the nil-logger default branch
// via the cheap dial-error path (no multi-second probes run).
func TestRunDiagnostics_NilLoggerDialError(t *testing.T) {
	srv, host, port := newFakeServer(t)
	srv.stop() // free the port → dial refused

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := RunDiagnostics(ctx, host, port, 0, nil); err == nil {
		t.Fatal("RunDiagnostics with nil logger to dead port: want error")
	}
}

// ----------------------------------------------------------------------
// keepalive.go — deterministic watchdog transitions + prober session-nil,
// driven by invoking the loop functions directly with a controlled
// keepAliveState (no reliance on goroutine scheduling races).

// TestKeepAliveWatchdog_Transitions runs keepAliveWatchdog directly against a
// session whose lastRx we toggle, deterministically driving both the
// went-silent (wasLive→!live) and live-again (!wasLive→live) arms.
func TestKeepAliveWatchdog_Transitions(t *testing.T) {
	if testing.Short() {
		t.Skip("watchdog tick floor is 1s; skipped in -short")
	}
	p := &Plugin{logger: testLogger()}
	s := &Session{logger: testLogger()}
	p.session = s
	// Timeout small enough that SessionLive flips on stale rx; the watchdog
	// tick interval is floored to 1s internally (timeout/3 clamped), so the
	// transitions only fire ~1s apart.
	// Timeout=1.5s: a stale rx (1h old) reads not-live on the first 1s tick
	// (silent transition); after we refresh rx, the next 1s tick still reads
	// live (rx ~1s old < 1.5s) → live-again transition. The watchdog tick
	// interval is floored to 1s (timeout/3 clamped).
	p.kaCfg = consumer.KeepAliveConfig{Interval: consumer.DisableInterval, Timeout: 1500 * time.Millisecond}
	p.ka = &keepAliveState{done: make(chan struct{})}
	p.ka.stopped.Add(1)

	// Start with stale rx so the first watchdog tick records "went silent".
	s.lastRxNS.Store(time.Now().Add(-time.Hour).UnixNano())
	go p.keepAliveWatchdog(1500 * time.Millisecond)

	// Wait past the first 1s tick so the wasLive→!live arm runs.
	time.Sleep(1200 * time.Millisecond)

	// Refresh rx so the next tick (at ~2s) records "live again".
	s.lastRxNS.Store(time.Now().UnixNano())
	time.Sleep(1300 * time.Millisecond)

	close(p.ka.done)
	p.ka.stopped.Wait()
}

// TestKeepAlive_ProberFullReply connects with a fast prober against the
// default fakeServer (whose GetSlotInfo reply is >=2 bytes) and waits long
// enough for several prober ticks, covering the len(reply)>=2 MarkSlotProbed
// arm. (The existing ProberAndWatchdog test returns before the first tick
// because the handshake replies already mark the session live.)
func TestKeepAlive_ProberFullReply(t *testing.T) {
	srv, host, port := newFakeServer(t)
	defer srv.stop()

	p := &Plugin{logger: testLogger()}
	p.SetKeepAlive(consumer.KeepAliveConfig{
		Interval: 15 * time.Millisecond,
		Timeout:  consumer.DisableTimeout,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := p.Connect(ctx, host, port); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = p.Disconnect() }()

	// Let several prober ticks land; each gets a >=2-byte GetSlotInfo reply.
	time.Sleep(100 * time.Millisecond)

	// The prober's reply updates slot 0 status via MarkSlotProbed(0, &st).
	si, err := p.GetSlotInfo(context.Background(), 0)
	if err != nil {
		t.Fatalf("GetSlotInfo(0): %v", err)
	}
	if si.Status != consumer.SlotPresent {
		t.Errorf("slot 0 status = %v, want present", si.Status)
	}
}

// TestKeepAliveProber_SessionNil runs keepAliveProber directly with a Plugin
// whose session is nil, so the first tick takes the s==nil return arm.
func TestKeepAliveProber_SessionNil(t *testing.T) {
	p := &Plugin{logger: testLogger()}
	p.session = nil
	p.ka = &keepAliveState{done: make(chan struct{})}
	p.ka.stopped.Add(1)
	done := make(chan struct{})
	go func() { p.keepAliveProber(context.Background(), 5*time.Millisecond); close(done) }()
	select {
	case <-done: // returns on the first tick because session==nil
	case <-time.After(time.Second):
		close(p.ka.done)
		t.Fatal("prober with nil session did not return")
	}
	p.ka.stopped.Wait()
}

// TestKeepAliveProber_CtxCanceled runs keepAliveProber with a context that is
// cancelled, driving the ctx.Done select arm.
func TestKeepAliveProber_CtxCanceled(t *testing.T) {
	p := &Plugin{logger: testLogger()}
	p.session = &Session{logger: testLogger()}
	p.ka = &keepAliveState{done: make(chan struct{})}
	p.ka.stopped.Add(1)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { p.keepAliveProber(ctx, time.Hour); close(done) }()
	cancel() // → prober's <-ctx.Done() arm returns
	select {
	case <-done:
	case <-time.After(time.Second):
		close(p.ka.done)
		t.Fatal("prober did not return on ctx cancel")
	}
	p.ka.stopped.Wait()
}

// TestRunDiagnostics_ConnectionClosed covers the connection-closed probe
// outcomes plus the announce-listen loop's done arm: the server completes
// the AN2 + ACP2 handshake, then closes the connection, so every probe
// deterministically reports "error: connection closed" — the first probe
// via either the nil sentinel or the addWaiter fail-fast (whichever side
// of the readLoop exit it lands on), every later probe via fail-fast
// (once one probe has observed the death, waitersDead is already set).
// The announce window (1s) is longer than the probes take, so its select
// deterministically takes the already-closed sess.done arm.
func TestRunDiagnostics_ConnectionClosed(t *testing.T) {
	host, port, stop := handshakeListener(t, 5, func(c net.Conn, f *codec.AN2Frame) {
		// First ACP2 frame is the handshake get_version. Answer it, then
		// close so all RunDiagnostics probes see a dead session.
		_ = writeAN2(c, &codec.AN2Frame{Proto: codec.AN2ProtoACP2, Slot: f.Slot,
			MTID: 0, Type: codec.AN2TypeData,
			Payload: []byte{byte(codec.ACP2TypeReply), f.Payload[1], byte(codec.ACP2FuncGetVersion), 2}})
		_ = c.Close()
	})
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// probe is generous: no probe in this test ever waits it out (each
	// resolves via sentinel/fail-fast) — a short value here would let a
	// stalled CI runner turn a pending sentinel into a spurious timeout.
	results, err := runDiagnostics(ctx, host, port, 1, testLogger(),
		diagTimings{probe: 2 * time.Second, announce: time.Second})
	if err != nil {
		t.Fatalf("runDiagnostics: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("runDiagnostics returned no results")
	}
	for _, r := range results {
		if r.Status != "error: connection closed" {
			t.Errorf("probe %q status = %q, want 'error: connection closed'", r.Name, r.Status)
		}
	}
}

// TestRunDiagnostics_CloseDuringACMP deterministically covers BOTH
// connection-death probe paths: the server handshakes, answers ACP2 data
// probes with errors, then closes the connection the moment it receives
// the first ACMP (proto=3) frame. That probe registered its waiter before
// sending, so it resolves via the nil sentinel ("connection closed"); the
// probes after it start with waitersDead already set, so they resolve via
// the addWaiter fail-fast path.
func TestRunDiagnostics_CloseDuringACMP(t *testing.T) {
	host, port, stop := rawListener(t, func(c net.Conn) {
		defer func() { _ = c.Close() }()
		_ = c.SetDeadline(time.Now().Add(10 * time.Second))
		acpVerSent := false
		for {
			f, err := codec.ReadAN2Frame(c)
			if err != nil {
				return
			}
			switch f.Proto {
			case codec.AN2ProtoInternal:
				fn := f.Payload[0]
				var payload []byte
				switch fn {
				case codec.AN2FuncGetVersion:
					payload = []byte{fn, 1, 0}
				case codec.AN2FuncGetDeviceInfo:
					payload = []byte{fn, 1}
				case codec.AN2FuncGetSlotInfo:
					payload = []byte{fn, 2, 1, byte(codec.AN2ProtoACP2)}
				case codec.AN2FuncEnableProtocolEvents:
					payload = []byte{fn}
				default:
					continue
				}
				_ = writeAN2(c, &codec.AN2Frame{Proto: codec.AN2ProtoInternal,
					Slot: f.Slot, MTID: f.MTID, Type: codec.AN2TypeReply, Payload: payload})
			case codec.AN2ProtoACP2:
				if !acpVerSent {
					acpVerSent = true
					_ = writeAN2(c, &codec.AN2Frame{Proto: codec.AN2ProtoACP2, Slot: f.Slot,
						MTID: 0, Type: codec.AN2TypeData,
						Payload: []byte{byte(codec.ACP2TypeReply), f.Payload[1], byte(codec.ACP2FuncGetVersion), 2}})
					continue
				}
				// Subsequent ACP2 data probes → fast error reply (echo mtid).
				_ = writeAN2(c, &codec.AN2Frame{Proto: codec.AN2ProtoACP2, Slot: f.Slot,
					MTID: 0, Type: codec.AN2TypeData,
					Payload: []byte{byte(codec.ACP2TypeError), f.Payload[1], byte(codec.ErrInvalidObjID), 0}})
			default:
				// First ACMP (proto=3) or proto=4 frame → close so that
				// probe (already registered + sent) gets the nil sentinel.
				return
			}
		}
	})
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	results, err := runDiagnostics(ctx, host, port, 1, testLogger(),
		diagTimings{probe: 2 * time.Second, announce: time.Second})
	if err != nil {
		t.Fatalf("runDiagnostics: %v", err)
	}
	// The three ACMP probes + the proto=4 probe follow the close: all four
	// must report the connection death, never a timeout.
	var closed int
	for _, r := range results {
		if r.Status == "error: connection closed" {
			closed++
		}
	}
	if closed < 4 {
		t.Errorf("got %d 'connection closed' probes, want >= 4; results=%+v", closed, results)
	}
}

// TestRunDiagnostics_CloseDuringProto4 covers the probe timeout arm (the
// ACMP replies are dropped while the connection stays alive, so those
// probes deterministically wait out the injected probe timeout) and the
// late connection death: the server closes on the first proto=4 frame,
// so that probe resolves via the nil sentinel.
func TestRunDiagnostics_CloseDuringProto4(t *testing.T) {
	host, port, stop := rawListener(t, func(c net.Conn) {
		defer func() { _ = c.Close() }()
		_ = c.SetDeadline(time.Now().Add(30 * time.Second))
		acpVerSent := false
		for {
			f, err := codec.ReadAN2Frame(c)
			if err != nil {
				return
			}
			switch f.Proto {
			case codec.AN2ProtoInternal:
				fn := f.Payload[0]
				var payload []byte
				switch fn {
				case codec.AN2FuncGetVersion:
					payload = []byte{fn, 1, 0}
				case codec.AN2FuncGetDeviceInfo:
					payload = []byte{fn, 1}
				case codec.AN2FuncGetSlotInfo:
					payload = []byte{fn, 2, 1, byte(codec.AN2ProtoACP2)}
				case codec.AN2FuncEnableProtocolEvents:
					payload = []byte{fn}
				default:
					continue
				}
				_ = writeAN2(c, &codec.AN2Frame{Proto: codec.AN2ProtoInternal,
					Slot: f.Slot, MTID: f.MTID, Type: codec.AN2TypeReply, Payload: payload})
			case codec.AN2ProtoACP2:
				if !acpVerSent {
					acpVerSent = true
					_ = writeAN2(c, &codec.AN2Frame{Proto: codec.AN2ProtoACP2, Slot: f.Slot,
						MTID: 0, Type: codec.AN2TypeData,
						Payload: []byte{byte(codec.ACP2TypeReply), f.Payload[1], byte(codec.ACP2FuncGetVersion), 2}})
					continue
				}
				_ = writeAN2(c, &codec.AN2Frame{Proto: codec.AN2ProtoACP2, Slot: f.Slot,
					MTID: 0, Type: codec.AN2TypeData,
					Payload: []byte{byte(codec.ACP2TypeError), f.Payload[1], byte(codec.ErrInvalidObjID), 0}})
			case 3:
				// ACMP probe — drop (no reply) so it waits out the probe timeout.
			default:
				// proto=4 probe → close so it resolves via the nil sentinel.
				return
			}
		}
	})
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	results, err := runDiagnostics(ctx, host, port, 1, testLogger(),
		diagTimings{probe: 300 * time.Millisecond, announce: 50 * time.Millisecond})
	if err != nil {
		t.Fatalf("runDiagnostics: %v", err)
	}
	// The three dropped-reply ACMP probes must report the timeout arm.
	var timeouts int
	for _, r := range results {
		if r.Status == "timeout (300ms)" {
			timeouts++
		}
	}
	if timeouts < 3 {
		t.Errorf("got %d timeout probes, want >= 3; results=%+v", timeouts, results)
	}
}

// TestRunDiagnostics_ErrorReplies covers the ACP2 error-reply status arm
// (msg.Type == error) in the probe path: the server answers every ACP2
// data probe with an error frame. The ACMP / proto=4 probes (replies
// dropped by the readLoop) wait out the short injected probe timeout.
func TestRunDiagnostics_ErrorReplies(t *testing.T) {
	srv, host, port := newFakeServer(t)
	srv.onACP2 = func(slot uint8, req *codec.ACP2Message) *codec.ACP2Message {
		return errorReply(codec.ErrInvalidObjID) // every ACP2 probe → fast error reply
	}
	defer srv.stop()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	results, err := runDiagnostics(ctx, host, port, 1, testLogger(),
		diagTimings{probe: 300 * time.Millisecond, announce: 50 * time.Millisecond})
	if err != nil {
		t.Fatalf("runDiagnostics: %v", err)
	}
	var sawErr bool
	for _, r := range results {
		if len(r.Status) >= 5 && r.Status[:5] == "error" {
			sawErr = true
			break
		}
	}
	if !sawErr {
		t.Error("expected at least one error-status probe result")
	}
}
