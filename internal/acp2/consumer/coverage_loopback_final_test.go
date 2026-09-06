package acp2

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	"dhs/internal/acp2/codec"
	"dhs/internal/consumer"
)

// coverage_loopback_final_test.go drives the socket-reachable error,
// cancel, reconnect, keepalive and walk-edge paths that the existing
// loopback suites don't reach. It reuses the fakeServer harness for the
// happy-adjacent cases and a couple of bespoke raw listeners for the
// handshake-failure permutations (server closes mid-handshake, sends a
// short reply, etc.). Every assertion uses the production codec.

// rawListener accepts exactly one TCP connection and hands it to fn on a
// background goroutine. Returns host, port, and a stop closure.
func rawListener(t *testing.T, fn func(net.Conn)) (string, int, func()) {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		c, aerr := ln.Accept()
		if aerr != nil {
			return
		}
		fn(c)
	}()
	host, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)
	stop := func() {
		_ = ln.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
		}
	}
	return host, port, stop
}

// readAN2 reads one AN2 frame or fails the test on a hard error.
func mustReadAN2(c net.Conn) (*codec.AN2Frame, error) {
	return codec.ReadAN2Frame(c)
}

// ----------------------------------------------------------------------
// session.go — handshake step error paths via a server that closes the
// connection right after accept (every an2Request then returns via the
// readLoop's done channel or a send error).

func TestConnect_ServerClosesImmediately(t *testing.T) {
	host, port, stop := rawListener(t, func(c net.Conn) {
		_ = c.Close() // hang up before any handshake reply
	})
	defer stop()

	s := NewSession(nil, testLogger())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := s.Connect(ctx, host, port)
	if err == nil {
		_ = s.Disconnect()
		t.Fatal("Connect against a server that hangs up should error")
	}
}

// TestConnect_ShortGetVersionReply covers the an2Handshake short-reply
// fallbacks: GetVersion replies with a single byte (→ major=0 minor=byte),
// GetDeviceInfo replies with a single byte (→ numSlots=byte), then the
// connection closes so the handshake aborts after the fallbacks ran.
func TestConnect_ShortGetVersionReply(t *testing.T) {
	host, port, stop := rawListener(t, func(c net.Conn) {
		defer func() { _ = c.Close() }()
		// Reply to the first two AN2 requests with 1-byte payloads, then
		// stop replying (close) so the 3rd request fails fast.
		for i := 0; i < 2; i++ {
			f, err := mustReadAN2(c)
			if err != nil {
				return
			}
			// 1-byte payload (just the func echo) → exercises the
			// len(reply)>=1 fallback arms.
			_ = writeAN2(c, &codec.AN2Frame{
				Proto: codec.AN2ProtoInternal, Slot: f.Slot, MTID: f.MTID,
				Type: codec.AN2TypeReply, Payload: []byte{f.Payload[0]},
			})
		}
		// Drain + drop the rest so subsequent requests time out / close.
	})
	defer stop()

	s := NewSession(nil, testLogger())
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	// Connect will fail at a later handshake step, but the short-reply
	// fallback arms for GetVersion + GetDeviceInfo run first.
	_ = s.Connect(ctx, host, port)
	_ = s.Disconnect()
}

// TestConnect_PortZeroDefault covers the port==0 → DefaultPort branch in
// Session.Connect. The dial will fail (nothing on DefaultPort) but the
// branch executes first.
func TestConnect_PortZeroDefault(t *testing.T) {
	s := NewSession(nil, testLogger())
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_ = s.Connect(ctx, "127.0.0.1", 0) // 0 → codec.DefaultPort
	_ = s.Disconnect()
}

// TestDoACP2_ContextTimeout covers DoACP2's ctx.Done arm: the server never
// answers the ACP2 request, so the per-call deadline fires.
func TestDoACP2_ContextTimeout(t *testing.T) {
	srv, host, port := newFakeServer(t)
	srv.onACP2 = func(slot uint8, req *codec.ACP2Message) *codec.ACP2Message {
		return nil // drop ACP2 requests → consumer hits its deadline
	}
	defer srv.stop()

	p := connectPlugin(t, srv, host, port)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_, err := p.GetValue(ctx, consumer.ValueRequest{Slot: 0, ID: 7})
	if err == nil {
		t.Fatal("GetValue with a non-answering server should time out")
	}
}

// TestSendFrame_AfterDisconnect covers sendFrame's conn==nil guard by
// disconnecting the session then issuing a DoACP2 (which calls sendFrame).
func TestSendFrame_AfterDisconnect(t *testing.T) {
	srv, host, port := newFakeServer(t)
	defer srv.stop()

	s := NewSession(nil, testLogger())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.Connect(ctx, host, port); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	_ = s.Disconnect() // conn → nil

	// sendFrame now sees conn==nil → ErrNotConnected (via DoACP2).
	_, err := s.DoACP2(context.Background(), 0, &codec.ACP2Message{
		Type: codec.ACP2TypeRequest, Func: codec.ACP2FuncGetObject, ObjID: 1})
	if err == nil {
		t.Fatal("DoACP2 after Disconnect should error")
	}
}

// ----------------------------------------------------------------------
// plugin.go — GetSlotInfo identity-from-tree, fetchObjectMeta options/label,
// Subscribe/Unsubscribe live.

// TestGetSlotInfo_IdentityFromTree covers the GetSlotInfo branch that pulls
// per-card identity strings out of a walked tree.
func TestGetSlotInfo_IdentityFromTree(t *testing.T) {
	srv, host, port := newFakeServer(t)
	srv.onACP2 = func(slot uint8, req *codec.ACP2Message) *codec.ACP2Message {
		if req.Func != codec.ACP2FuncGetObject {
			return errorReply(codec.ErrProtocol)
		}
		switch req.ObjID {
		case 1:
			return objectReply(1, []codec.Property{
				u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeNode)),
				codec.MakeStringProperty(codec.PIDLabel, "ROOT"),
				childrenProp(2),
			})
		case 2:
			return objectReply(2, []codec.Property{
				u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeString)),
				codec.MakeStringProperty(codec.PIDLabel, "Card Name"),
				codec.MakeStringProperty(codec.PIDValue, "Hybrid"),
			})
		}
		return errorReply(codec.ErrInvalidObjID)
	}
	defer srv.stop()

	p := connectPlugin(t, srv, host, port)
	if _, err := p.Walk(context.Background(), 1); err != nil {
		t.Fatalf("Walk: %v", err)
	}
	si, err := p.GetSlotInfo(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetSlotInfo: %v", err)
	}
	if si.Identity["Card Name"] != "Hybrid" {
		t.Errorf("identity = %v, want Card Name=Hybrid", si.Identity)
	}
}

// TestGetValue_FetchObjectMetaOptionsLabel covers fetchObjectMeta's PIDOptions
// + PIDLabel arms: a no-tree enum get with an options map in the get_object
// reply, so the confirmed value resolves to its label.
func TestGetValue_FetchObjectMetaOptionsLabel(t *testing.T) {
	srv, host, port := newFakeServer(t)
	base := func(slot uint8, req *codec.ACP2Message) *codec.ACP2Message {
		if req.Func == codec.ACP2FuncGetObject && req.ObjID == 4 {
			return objectReply(4, []codec.Property{
				u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeEnum)),
				codec.MakeStringProperty(codec.PIDLabel, "Mode"),
				u32Prop(codec.PIDNumberType, 0, uint32(codec.NumTypePreset)),
				optionsProp(map[uint32]string{0: "Off", 1: "On"}),
			})
		}
		return errorReply(codec.ErrInvalidObjID)
	}
	srv.onACP2 = wrapWithGetProperty(base, func(req *codec.ACP2Message) *codec.ACP2Message {
		return propertyReply(codec.ACP2FuncGetProperty, 4,
			codec.MakeValueProperty(codec.PIDValue, codec.NumTypePreset, beU32Bytes(1)))
	})
	defer srv.stop()

	p := connectPlugin(t, srv, host, port)
	// No prior Walk → fetchObjectMeta path with the options/label arms.
	val, err := p.GetValue(context.Background(), consumer.ValueRequest{Slot: 0, ID: 4})
	if err != nil {
		t.Fatalf("GetValue: %v", err)
	}
	if val.Kind != consumer.KindEnum || val.Str != "On" {
		t.Errorf("value = %+v, want enum On", val)
	}
}

// TestUnsubscribe_Live covers Unsubscribe against a live session (the
// ok && s != nil arm).
func TestUnsubscribe_Live(t *testing.T) {
	srv, host, port := newFakeServer(t)
	defer srv.stop()

	p := connectPlugin(t, srv, host, port)
	req := consumer.ValueRequest{Slot: -1, ID: -1}
	if err := p.Subscribe(req, func(consumer.Event) {}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := p.Unsubscribe(req); err != nil {
		t.Fatalf("Unsubscribe: %v", err)
	}
	// Unsubscribe of an unknown req is a no-op (the !ok path).
	if err := p.Unsubscribe(consumer.ValueRequest{Slot: 7, ID: 7}); err != nil {
		t.Fatalf("Unsubscribe unknown: %v", err)
	}
}

// ----------------------------------------------------------------------
// walker.go — WalkIdentity fallback to obj-0, child/leaf skips, walkObject
// progress + child-error log, getOne cancel.

// TestWalkIdentity_RootFallbackAndSkips drives WalkIdentity through: obj-1
// root fails → fall back to obj-0; a non-IDENTITY/BOARD child is skipped; a
// child get_object error is skipped; a leaf with an error / empty / non-string
// value is skipped; and a valid IDENTITY leaf is captured.
func TestWalkIdentity_RootFallbackAndSkips(t *testing.T) {
	srv, host, port := newFakeServer(t)
	srv.onACP2 = func(slot uint8, req *codec.ACP2Message) *codec.ACP2Message {
		if req.Func != codec.ACP2FuncGetObject {
			return errorReply(codec.ErrProtocol)
		}
		switch req.ObjID {
		case 1:
			return errorReply(codec.ErrInvalidObjID) // force obj-0 fallback
		case 0:
			return objectReply(0, []codec.Property{
				u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeNode)),
				codec.MakeStringProperty(codec.PIDLabel, "ROOT_NODE_V2"),
				childrenProp(2, 3, 9), // IDENTITY, Audio(skip), bad-child
			})
		case 2: // IDENTITY → has good + bad + non-string leaves
			return objectReply(2, []codec.Property{
				u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeNode)),
				codec.MakeStringProperty(codec.PIDLabel, "IDENTITY"),
				childrenProp(10, 11, 12),
			})
		case 3: // Audio → not BOARD/IDENTITY → skipped
			return objectReply(3, []codec.Property{
				u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeNode)),
				codec.MakeStringProperty(codec.PIDLabel, "Audio"),
				childrenProp(20),
			})
		case 10: // good string leaf
			return objectReply(10, []codec.Property{
				u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeString)),
				codec.MakeStringProperty(codec.PIDLabel, "Card Name"),
				codec.MakeStringProperty(codec.PIDValue, "Hybrid"),
			})
		case 11: // non-string leaf (number) → skipped
			return objectReply(11, []codec.Property{
				u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeNumber)),
				codec.MakeStringProperty(codec.PIDLabel, "Temp"),
				u32Prop(codec.PIDNumberType, 0, uint32(codec.NumTypeU32)),
				codec.MakeValueProperty(codec.PIDValue, codec.NumTypeU32, beU32Bytes(40)),
			})
		case 12: // leaf get_object error → skipped
			return errorReply(codec.ErrInvalidObjID)
		case 9: // bad child of root → get_object error → skipped
			return errorReply(codec.ErrInvalidObjID)
		}
		return errorReply(codec.ErrInvalidObjID)
	}
	defer srv.stop()

	p := connectPlugin(t, srv, host, port)
	id, err := p.IdentityProbe(context.Background(), 1)
	if err != nil {
		t.Fatalf("IdentityProbe: %v", err)
	}
	// Card Name present; version missing → "Hybrid@".
	if id != "Hybrid@" {
		t.Errorf("identity = %q, want Hybrid@", id)
	}
}

// TestWalk_ChildErrorLoggedAndProgress covers walkObject's OnProgress
// callback and the child-error WARN arm (a child get_object that errors is
// logged but the walk continues).
func TestWalk_ChildErrorLoggedAndProgress(t *testing.T) {
	srv, host, port := newFakeServer(t)
	srv.onACP2 = func(slot uint8, req *codec.ACP2Message) *codec.ACP2Message {
		if req.Func != codec.ACP2FuncGetObject {
			return errorReply(codec.ErrProtocol)
		}
		switch req.ObjID {
		case 1:
			return objectReply(1, []codec.Property{
				u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeNode)),
				codec.MakeStringProperty(codec.PIDLabel, "ROOT"),
				childrenProp(2, 3), // 2 ok, 3 errors
			})
		case 2:
			return objectReply(2, []codec.Property{
				u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeNumber)),
				codec.MakeStringProperty(codec.PIDLabel, "OK"),
				u32Prop(codec.PIDNumberType, 0, uint32(codec.NumTypeU32)),
				codec.MakeValueProperty(codec.PIDValue, codec.NumTypeU32, beU32Bytes(1)),
			})
		case 3:
			return errorReply(codec.ErrInvalidObjID) // child error → WARN arm
		}
		return errorReply(codec.ErrInvalidObjID)
	}
	defer srv.stop()

	p := connectPlugin(t, srv, host, port)
	var progressCalls int
	p.SetWalkProgress(func(count int, obj *consumer.Object) { progressCalls++ })

	objs, err := p.Walk(context.Background(), 1)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if progressCalls == 0 {
		t.Error("OnProgress never fired")
	}
	// ROOT + OK were added (child 3 errored, not added).
	if len(objs) != 2 {
		t.Errorf("walked %d objects, want 2 (ROOT + OK)", len(objs))
	}
}

// TestGetOne_ContextCanceled covers getOne's ctx.Done early-return: a
// pre-cancelled context makes the very first getOne bail before any send.
func TestGetOne_ContextCanceled(t *testing.T) {
	srv, host, port := newFakeServer(t)
	defer srv.stop()
	p := connectPlugin(t, srv, host, port)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before the call
	_, err := p.IdentityProbe(ctx, 1)
	if err == nil {
		t.Fatal("IdentityProbe with a cancelled ctx should error")
	}
}

// TestWalk_ContextCanceledMidWalk covers walkObject's top-of-call ctx.Done
// arm and the mid-children cancellation bail.
func TestWalk_ContextCanceledMidWalk(t *testing.T) {
	srv, host, port := newFakeServer(t)
	defer srv.stop()
	p := connectPlugin(t, srv, host, port)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := p.Walk(ctx, 1)
	if err == nil {
		t.Fatal("Walk with a cancelled ctx should error")
	}
}

// ----------------------------------------------------------------------
// keepalive.go — prober probe-failure (server stops answering) + the
// short-reply MarkSlotProbed(nil) arm, plus the watchdog went-silent +
// live-again transitions.

// TestKeepAlive_ProberShortReplyAndWatchdog connects with a fast keepalive,
// then makes the prober's AN2 GetSlotInfo reply a single byte so the
// len(reply)>=2 else branch (MarkSlotProbed(0, nil)) runs.
func TestKeepAlive_ProberShortReply(t *testing.T) {
	// A raw listener that completes the handshake then answers the
	// keepalive GetSlotInfo probe with a 1-byte payload so the prober's
	// len(reply)>=2 else branch (MarkSlotProbed(0, nil)) runs.
	host2, port2, stop := rawListener(t, func(c net.Conn) {
		defer func() { _ = c.Close() }()
		acpVerSent := false
		deadline := time.Now().Add(2 * time.Second)
		_ = c.SetDeadline(deadline)
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
					// 1-byte payload → keepalive prober len(reply)>=2 is false.
					payload = []byte{fn}
				case codec.AN2FuncEnableProtocolEvents:
					payload = []byte{fn}
				default:
					continue
				}
				_ = writeAN2(c, &codec.AN2Frame{Proto: codec.AN2ProtoInternal,
					Slot: f.Slot, MTID: f.MTID, Type: codec.AN2TypeReply, Payload: payload})
			case codec.AN2ProtoACP2:
				// answer get_version once (handshake step 5).
				if !acpVerSent {
					acpVerSent = true
					_ = writeAN2(c, &codec.AN2Frame{Proto: codec.AN2ProtoACP2, Slot: f.Slot,
						MTID: 0, Type: codec.AN2TypeData,
						Payload: []byte{byte(codec.ACP2TypeReply), f.Payload[1], byte(codec.ACP2FuncGetVersion), 2}})
				}
			}
		}
	})
	defer stop()

	p := &Plugin{logger: testLogger()}
	p.SetKeepAlive(consumer.KeepAliveConfig{
		Interval: 15 * time.Millisecond,
		Timeout:  45 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := p.Connect(ctx, host2, port2); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = p.Disconnect() }()

	// Let several prober ticks run so the short-reply arm executes.
	time.Sleep(120 * time.Millisecond)
}

// TestKeepAlive_WatchdogSilentThenLive drives the watchdog through both
// transitions: it goes silent (no rx within timeout) then live again once a
// probe lands. The prober is disabled so we control rx by hand.
func TestKeepAlive_WatchdogSilentThenLive(t *testing.T) {
	srv, host, port := newFakeServer(t)
	defer srv.stop()

	p := &Plugin{logger: testLogger()}
	// Disable the prober; enable a short watchdog. With no prober the
	// session has no rx after the handshake, so the watchdog flips to
	// "went silent" on its first tick past the timeout.
	p.SetKeepAlive(consumer.KeepAliveConfig{
		Interval: consumer.DisableInterval,
		Timeout:  30 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := p.Connect(ctx, host, port); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = p.Disconnect() }()

	// Wait long enough for the watchdog to observe silence (wasLive→!live).
	if !waitFor(t, time.Second, func() bool { return !p.SessionLive() }) {
		t.Fatal("watchdog never saw the session go silent")
	}

	// Now refresh rx so the watchdog's next tick sees live-again.
	p.mu.Lock()
	s := p.session
	p.mu.Unlock()
	s.lastRxNS.Store(time.Now().UnixNano())
	if !waitFor(t, time.Second, func() bool { return p.SessionLive() }) {
		t.Fatal("session never reported live again after rx refresh")
	}
	// Give the watchdog a tick to log the live-again transition.
	time.Sleep(40 * time.Millisecond)
}

// ----------------------------------------------------------------------
// reconnect.go — backoff retry-then-succeed + the direct stop/loop guards.

// TestNextBackoff covers the pure backoff helper: exponential doubling
// below the cap and clamping at defaultReconnectCap once doubling would
// overshoot. No real sleeping — this drives the cap branch deterministically.
func TestNextBackoff(t *testing.T) {
	// Doubling below the cap.
	if got := nextBackoff(defaultReconnectInitial); got != 2*time.Second {
		t.Errorf("nextBackoff(1s) = %v, want 2s", got)
	}
	if got := nextBackoff(8 * time.Second); got != 16*time.Second {
		t.Errorf("nextBackoff(8s) = %v, want 16s", got)
	}
	// Doubling that would exceed the cap clamps to the cap.
	if got := nextBackoff(16 * time.Second); got != defaultReconnectCap {
		t.Errorf("nextBackoff(16s) = %v, want cap %v", got, defaultReconnectCap)
	}
	// Already at the cap stays at the cap (idempotent at high attempt counts).
	if got := nextBackoff(defaultReconnectCap); got != defaultReconnectCap {
		t.Errorf("nextBackoff(cap) = %v, want cap %v", got, defaultReconnectCap)
	}
	// A pathologically large delay (very high attempt count) still clamps.
	if got := nextBackoff(24 * time.Hour); got != defaultReconnectCap {
		t.Errorf("nextBackoff(24h) = %v, want cap %v", got, defaultReconnectCap)
	}
}

// TestReconnect_BackoffRetries makes the first reconnect dial fail (server
// briefly down) then succeed, exercising runReconnectBackoff's failure arm
// (delay doubling) and reconnectOnce's success replay.
func TestReconnect_BackoffRetries(t *testing.T) {
	srv, host, port := newFakeServer(t)

	p := &Plugin{logger: testLogger()}
	p.SetKeepAlive(consumer.KeepAliveConfig{
		Interval: consumer.DisableInterval, Timeout: consumer.DisableTimeout,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := p.Connect(ctx, host, port); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = p.Disconnect() }()

	p.mu.Lock()
	first := p.session
	p.mu.Unlock()

	// Stop the whole server (listener + conns). The reconnect loop will
	// retry against a dead port (failure arm) until we bring a new
	// listener up on the same port.
	srv.stop()

	// Bring a fresh server up on the SAME port after a short delay so a
	// later backoff attempt succeeds.
	relisten := make(chan struct{})
	go func() {
		time.Sleep(150 * time.Millisecond)
		ln, err := net.Listen("tcp4", net.JoinHostPort(host, strconv.Itoa(port)))
		if err != nil {
			close(relisten)
			return
		}
		srv2 := &fakeServer{
			t: t, ln: ln, an2Major: 1, an2Minor: 0, slotCount: 2,
			slotStatus: []uint8{2, 2, 2}, slotProtos: []byte{byte(codec.AN2ProtoACP2)},
			acp2Ver: 1, an2ErrOnFunc: -1,
		}
		srv2.wg.Add(1)
		go srv2.acceptLoop()
		close(relisten)
		t.Cleanup(srv2.stop)
	}()

	// Wait for a fresh session pointer (reconnect succeeded).
	ok := waitFor(t, 5*time.Second, func() bool {
		p.mu.Lock()
		s := p.session
		p.mu.Unlock()
		return s != nil && s != first
	})
	<-relisten
	if !ok {
		t.Fatal("reconnect never re-established the session after retries")
	}
}

// TestStopLoops_WhenNeverStarted covers stopReconnectLoop + stopKeepAlive
// early-return guards (rc==nil / ka==nil) by calling Disconnect on a Plugin
// that was Connected with both loops disabled and never started.
func TestStopLoops_WhenNeverStarted(t *testing.T) {
	p := &Plugin{logger: testLogger()}
	// rc and ka are nil; the stop helpers must no-op without panic.
	p.mu.Lock()
	p.stopReconnectLoop()
	p.stopKeepAlive()
	p.mu.Unlock()
}

// TestReconnectOnce_ConnectError covers reconnectOnce's Connect-failure
// return: it drops the session then re-Connects to a dead port, which fails.
func TestReconnectOnce_ConnectError(t *testing.T) {
	srv, host, port := newFakeServer(t)
	p := &Plugin{logger: testLogger()}
	p.SetKeepAlive(consumer.KeepAliveConfig{
		Interval: consumer.DisableInterval, Timeout: consumer.DisableTimeout,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := p.Connect(ctx, host, port); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	srv.stop() // kill the server so the re-Connect fails

	rctx, rcancel := context.WithTimeout(context.Background(), time.Second)
	defer rcancel()
	if _, err := p.reconnectOnce(rctx, host, port); err == nil {
		t.Fatal("reconnectOnce to a dead port should error")
	}
	_ = p.Disconnect()
}

// writeAN2 encodes + writes a frame on a raw conn (test helper).
func writeAN2(c net.Conn, f *codec.AN2Frame) error {
	b, err := codec.EncodeAN2Frame(f)
	if err != nil {
		return err
	}
	_, err = c.Write(b)
	return err
}
