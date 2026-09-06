package emberplus

import (
	"context"
	"dhs/internal/plugin"
	"net"
	"testing"
	"time"

	"dhs/internal/consumer"
	"dhs/internal/consumer/compliance"
	"dhs/internal/emberplus/codec/glow"
	"dhs/internal/emberplus/codec/s101"
)

// pipeSession wires a Session over the server end of a net.Pipe with a
// live writer/reader, and spins a drain goroutine on the client end so
// outbound frames (SendSetValue / SendSubscribe / SendInvoke) never
// block. The returned cleanup closes both ends. Unlike wireSession this
// does NOT start readLoop — the caller drives the send paths directly.
func pipeSession(t *testing.T) (*Session, func()) {
	t.Helper()
	srvConn, cliConn := net.Pipe()
	s := NewSession(discardLogger())
	s.SetProfile(&compliance.Profile{})
	s.mu.Lock()
	s.conn = srvConn
	s.reader = s101.NewReader(srvConn)
	s.writer = s101.NewWriter(srvConn)
	s.closed = false
	s.mu.Unlock()
	r := s101.NewReader(cliConn)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, err := r.ReadFrame(); err != nil {
				return
			}
		}
	}()
	cleanup := func() {
		_ = srvConn.Close()
		_ = cliConn.Close()
		<-done
	}
	return s, cleanup
}

// deadSession returns a Session whose writer is nil, so every Send*
// returns "not connected" — drives the send-error arms.
func deadSession() *Session {
	s := NewSession(discardLogger())
	s.SetProfile(&compliance.Profile{})
	return s
}

// TestSetValue_ErrorArms drives every defensive branch of SetValue after
// the session-liveness gate: nil-session, numericPath fallback, no-path,
// KindUnknown inference, coerce failure, constraint failure, send
// failure, ctx-cancel, and write-timeout.
func TestSetValue_ErrorArms(t *testing.T) {
	ctx := context.Background()

	t.Run("session_nil_after_gate", func(t *testing.T) {
		// sessionConnected true but session nil → the s == nil guard.
		p := freshPlugin()
		p.sessionConnected = true
		p.session = nil
		if _, err := p.SetValue(ctx, consumer.ValueRequest{Path: "1.1", ID: -1},
			consumer.Value{Kind: consumer.KindInt, Int: 1}); err != consumer.ErrNotConnected {
			t.Errorf("want ErrNotConnected, got %v", err)
		}
	})

	t.Run("numericPath_fallback_then_send_error", func(t *testing.T) {
		// glowParam has empty Path → SetValue falls back to numericPath,
		// then SendSetValue on a dead (nil-writer) session errors.
		p := freshPlugin()
		p.sessionConnected = true
		p.session = deadSession()
		e := &treeEntry{
			numericPath: []int32{1, 1},
			glowParam:   &glow.Parameter{Type: glow.ParamTypeInteger}, // empty Path
			obj:         consumer.Object{OID: "1.1", Kind: consumer.KindInt},
		}
		p.numIndex["1.1"] = e
		if _, err := p.SetValue(ctx, consumer.ValueRequest{Path: "1.1", ID: -1},
			consumer.Value{Kind: consumer.KindInt, Int: 1}); err == nil {
			t.Error("want send error on dead session")
		}
	})

	t.Run("no_path_at_all", func(t *testing.T) {
		// glowParam empty Path AND entry.numericPath empty → "no path".
		p := freshPlugin()
		p.sessionConnected = true
		p.session = deadSession()
		e := &treeEntry{
			glowParam: &glow.Parameter{Type: glow.ParamTypeInteger},
			obj:       consumer.Object{OID: "1.1", Kind: consumer.KindInt},
		}
		p.numIndex["1.1"] = e
		if _, err := p.SetValue(ctx, consumer.ValueRequest{Path: "1.1", ID: -1},
			consumer.Value{Kind: consumer.KindInt, Int: 1}); err == nil {
			t.Error("want 'no path' error")
		}
	})

	t.Run("kind_unknown_inferred", func(t *testing.T) {
		// val.Kind == KindUnknown → inherit entry.obj.Kind, then succeed
		// over a live pipe writer.
		s, cleanup := pipeSession(t)
		defer cleanup()
		p := freshPlugin()
		p.sessionConnected = true
		p.session = s
		e := &treeEntry{
			numericPath: []int32{1, 1},
			glowParam:   &glow.Parameter{Path: []int32{1, 1}, Type: glow.ParamTypeInteger},
			obj:         consumer.Object{OID: "1.1", Kind: consumer.KindInt},
		}
		p.numIndex["1.1"] = e
		ctxC, cancel := context.WithCancel(ctx)
		cancel() // cancel so we exit on ctx.Done after the send, fast.
		_, _ = p.SetValue(ctxC, consumer.ValueRequest{Path: "1.1", ID: -1},
			consumer.Value{Kind: consumer.KindUnknown, Int: 7})
	})

	t.Run("coerce_error", func(t *testing.T) {
		p := freshPlugin()
		p.sessionConnected = true
		p.session = deadSession()
		e := &treeEntry{
			numericPath: []int32{1, 1},
			glowParam:   &glow.Parameter{Path: []int32{1, 1}, Type: glow.ParamTypeInteger},
			obj:         consumer.Object{OID: "1.1", Kind: consumer.KindInt},
		}
		p.numIndex["1.1"] = e
		// Int kind with a non-numeric string → coerceStringToTyped errors.
		if _, err := p.SetValue(ctx, consumer.ValueRequest{Path: "1.1", ID: -1},
			consumer.Value{Kind: consumer.KindInt, Str: "notanumber"}); err == nil {
			t.Error("want coerce error")
		}
	})

	t.Run("constraint_error", func(t *testing.T) {
		p := freshPlugin()
		p.sessionConnected = true
		p.session = deadSession()
		maxV := float64(10)
		e := &treeEntry{
			numericPath: []int32{1, 1},
			glowParam:   &glow.Parameter{Path: []int32{1, 1}, Type: glow.ParamTypeInteger, Maximum: maxV},
			obj:         consumer.Object{OID: "1.1", Kind: consumer.KindInt},
		}
		p.numIndex["1.1"] = e
		// 42 > max 10 → applyParameterConstraints errors.
		if _, err := p.SetValue(ctx, consumer.ValueRequest{Path: "1.1", ID: -1},
			consumer.Value{Kind: consumer.KindInt, Int: 42}); err == nil {
			t.Error("want out-of-range error")
		}
	})

	t.Run("ctx_cancel_after_send", func(t *testing.T) {
		s, cleanup := pipeSession(t)
		defer cleanup()
		p := freshPlugin()
		p.sessionConnected = true
		p.session = s
		e := &treeEntry{
			numericPath: []int32{1, 1},
			glowParam:   &glow.Parameter{Path: []int32{1, 1}, Type: glow.ParamTypeInteger},
			obj:         consumer.Object{OID: "1.1", Kind: consumer.KindInt},
		}
		p.numIndex["1.1"] = e
		ctxC, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := p.SetValue(ctxC, consumer.ValueRequest{Path: "1.1", ID: -1},
			consumer.Value{Kind: consumer.KindInt, Int: 5}); err == nil {
			t.Error("want ctx.Err() after cancel")
		}
	})

	t.Run("write_timeout", func(t *testing.T) {
		// Live writer (send succeeds), no confirming announce → after
		// defaultWriteTimeout the timer.C arm fires ErrWriteTimeout.
		s, cleanup := pipeSession(t)
		defer cleanup()
		p := freshPlugin()
		p.sessionConnected = true
		p.session = s
		e := &treeEntry{
			numericPath: []int32{1, 1},
			glowParam:   &glow.Parameter{Path: []int32{1, 1}, Type: glow.ParamTypeInteger},
			obj:         consumer.Object{OID: "1.1", Kind: consumer.KindInt},
		}
		p.numIndex["1.1"] = e
		if _, err := p.SetValue(ctx, consumer.ValueRequest{Path: "1.1", ID: -1},
			consumer.Value{Kind: consumer.KindInt, Int: 5}); err != consumer.ErrWriteTimeout {
			t.Errorf("want ErrWriteTimeout, got %v", err)
		}
	})
}

// TestInvokeFunction_ErrorArms drives the send-error, ctx-done, and
// failed-result arms of InvokeFunction.
func TestInvokeFunction_ErrorArms(t *testing.T) {
	t.Run("send_error", func(t *testing.T) {
		p := freshPlugin()
		p.session = deadSession() // nil writer → SendInvoke errors
		p.numIndex["1.1"] = &treeEntry{
			numericPath: []int32{1, 1},
			glowFunc:    &glow.Function{Path: []int32{1, 1}},
			obj:         consumer.Object{OID: "1.1"},
		}
		p.pathIndex["fn"] = p.numIndex["1.1"]
		if _, err := p.InvokeFunction(context.Background(), "fn", []any{int64(1)}); err == nil {
			t.Error("want send error")
		}
	})

	t.Run("ctx_done", func(t *testing.T) {
		s, cleanup := pipeSession(t)
		defer cleanup()
		p := freshPlugin()
		p.session = s
		p.numIndex["1.1"] = &treeEntry{
			numericPath: []int32{1, 1},
			glowFunc:    &glow.Function{Path: []int32{1, 1}},
			obj:         consumer.Object{OID: "1.1"},
		}
		p.pathIndex["fn"] = p.numIndex["1.1"]
		ctxC, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := p.InvokeFunction(ctxC, "fn", nil); err == nil {
			t.Error("want ctx error")
		}
	})

	t.Run("failed_result", func(t *testing.T) {
		s, cleanup := pipeSession(t)
		defer cleanup()
		p := freshPlugin()
		p.session = s
		p.numIndex["1.1"] = &treeEntry{
			numericPath: []int32{1, 1},
			glowFunc:    &glow.Function{Path: []int32{1, 1}},
			obj:         consumer.Object{OID: "1.1"},
		}
		p.pathIndex["fn"] = p.numIndex["1.1"]
		// Deliver a Success=false result on whatever invocation id the
		// call registers (NextInvocationID starts at 1 on a fresh session).
		go func() {
			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) {
				s.deliverInvocationResult(&glow.InvocationResult{InvocationID: 1, Success: false})
				time.Sleep(5 * time.Millisecond)
			}
		}()
		res, err := p.InvokeFunction(context.Background(), "fn", nil)
		if err == nil {
			t.Error("want invocation-failed error")
		}
		if res == nil {
			t.Error("failed result pointer should still be returned")
		}
	})
}

// TestMatrixConnect_SendError drives the SendMatrixConnect failure arm:
// a matrix entry with matrixState=nil (so validation is skipped) and a
// dead session so the send errors.
func TestMatrixConnect_SendError(t *testing.T) {
	p := freshPlugin()
	p.session = deadSession()
	p.numIndex["1.1"] = &treeEntry{
		numericPath: []int32{1, 1},
		glowMatrix:  &glow.Matrix{Path: []int32{1, 1}},
		obj:         consumer.Object{OID: "1.1"},
	}
	p.pathIndex["mtx"] = p.numIndex["1.1"]
	if err := p.MatrixConnect(context.Background(), "mtx", 0, []int32{0}, glow.ConnOpConnect); err == nil {
		t.Error("want send error on dead session")
	}
}

// TestSubscribeWildcardArms drives subscribeAllParameters and
// subscribeOnDiscovery branch coverage: numericPath-empty skip,
// wildcardMatches-false skip, already-subscribed skip, the SendSubscribe
// error arm (dead session), and the successful send (live pipe).
func TestSubscribeAllParameters_Arms(t *testing.T) {
	t.Run("skips_and_send_error", func(t *testing.T) {
		p := freshPlugin()
		p.session = deadSession() // SendSubscribe errors
		p.subs["*"] = func(consumer.Event) {}

		// (a) glowParam but empty numericPath → skipped (44-45).
		p.numIndex["a"] = &treeEntry{glowParam: &glow.Parameter{}}
		// (b) glowParam + numericPath but filtered out by --no-streams
		//     on a stream param → wildcardMatches false (49-50).
		p.SetWildcardSubscribeFilter(nil, true /*noStreams*/, false)
		p.numIndex["b"] = &treeEntry{
			numericPath: []int32{1, 2},
			glowParam:   &glow.Parameter{HasStreamIdentifier: true, StreamIdentifier: 9},
		}
		// (c) a plain param that passes the filter → reaches SendSubscribe,
		//     which errors on the dead session (69-72).
		p.numIndex["c"] = &treeEntry{
			numericPath: []int32{1, 3},
			glowParam:   &glow.Parameter{},
		}
		p.subscribeAllParameters()

		// (d) Now "c" is in streamSubs → a second pass hits the
		//     already-subscribed skip (62-64).
		p.SetWildcardSubscribeFilter(nil, false, false)
		p.subscribeAllParameters()
	})

	t.Run("successful_send", func(t *testing.T) {
		s, cleanup := pipeSession(t)
		defer cleanup()
		p := freshPlugin()
		p.session = s
		p.subs["*"] = func(consumer.Event) {}
		p.numIndex["c"] = &treeEntry{
			numericPath: []int32{1, 4},
			glowParam:   &glow.Parameter{},
		}
		p.subscribeAllParameters()
		p.subsMu.RLock()
		_, ok := p.streamSubs["1.4"]
		p.subsMu.RUnlock()
		if !ok {
			t.Error("expected 1.4 registered in streamSubs after successful subscribe")
		}
	})
}

// TestSubscribeOnDiscovery_Arms drives the filtered-out skip (99-101)
// and the full send path including the SendSubscribe error arm (123-127)
// and the success path (128-129).
func TestSubscribeOnDiscovery_Arms(t *testing.T) {
	t.Run("filtered_out", func(t *testing.T) {
		p := freshPlugin()
		p.subs["*"] = func(consumer.Event) {}
		p.SetWildcardSubscribeFilter(nil, true /*noStreams*/, false)
		// stream param + filter that drops streams → wildcardMatches false.
		p.subscribeOnDiscovery(&treeEntry{
			numericPath: []int32{2, 1},
			glowParam:   &glow.Parameter{HasStreamIdentifier: true, StreamIdentifier: 3},
		})
	})

	t.Run("send_error", func(t *testing.T) {
		p := freshPlugin()
		p.session = deadSession()
		p.subs["*"] = func(consumer.Event) {}
		p.subscribeOnDiscovery(&treeEntry{
			numericPath: []int32{2, 2},
			glowParam:   &glow.Parameter{},
		})
	})

	t.Run("success", func(t *testing.T) {
		s, cleanup := pipeSession(t)
		defer cleanup()
		p := freshPlugin()
		p.session = s
		p.subs["*"] = func(consumer.Event) {}
		p.subscribeOnDiscovery(&treeEntry{
			numericPath: []int32{2, 3},
			glowParam:   &glow.Parameter{},
		})
		p.subsMu.RLock()
		_, ok := p.streamSubs["2.3"]
		p.subsMu.RUnlock()
		if !ok {
			t.Error("expected 2.3 in streamSubs after discovery subscribe")
		}
	})
}

// TestAppendTemplateObjects covers the loop body: a nil template is
// skipped, a real template is serialized into a synthetic Object.
func TestAppendTemplateObjects(t *testing.T) {
	p := freshPlugin()
	p.templates["nil"] = nil
	p.templates["5"] = &glow.Template{Number: 5, Description: "tmpl", Qualified: true}
	out := appendTemplateObjects(nil, p)
	var found bool
	for _, o := range out {
		if o.OID == "5" && o.Label == "tmpl" {
			found = true
		}
	}
	if !found {
		t.Errorf("template not serialized; got %+v", out)
	}
}

// TestDecodeStreamBytes_Int64Fallthrough covers the trailing return-nil:
// an Int64 format has size 8 (passes the length check) but no decode
// case in the switch → returns nil.
func TestDecodeStreamBytes_Int64Fallthrough(t *testing.T) {
	if v := decodeStreamBytes(glow.StreamFmtSignedInt64BigEndian, make([]byte, 8)); v != nil {
		t.Errorf("Int64 format has no decode case → want nil, got %v", v)
	}
}

// TestOnSessionStateChange_NoTransition covers the prev==connected
// early-return (no state flip → no work).
func TestOnSessionStateChange_NoTransition(t *testing.T) {
	p := freshPlugin()
	p.sessionConnected = true
	// Calling with the same value must early-return without panicking
	// (no reconnect start, no event emit).
	p.onSessionStateChange(true, "noop")
}

// TestDisconnect_NoSession covers the s == nil return path of Disconnect.
func TestDisconnect_NoSession(t *testing.T) {
	p := freshPlugin()
	p.session = nil
	if err := p.Disconnect(); err != nil {
		t.Errorf("Disconnect with no session = %v, want nil", err)
	}
}

// TestReconnectCtrl_StartAlreadyActive covers the already-active
// early-return of reconnectCtrl.start.
func TestReconnectCtrl_StartAlreadyActive(t *testing.T) {
	p := (&Factory{}).New(plugin.Deps{Logger: discardLogger()}).(*Plugin)
	p.connIP = "127.0.0.1"
	p.connPort = 1
	p.reconnectPolicyOverride = &reconnectPolicy{
		InitialBackoff: time.Hour, // park the loop in backoff so it stays active
		MaxBackoff:     time.Hour,
		MaxAttempts:    0,
	}
	p.reconnect.start(p)
	// Second start must observe active==true and return immediately.
	p.reconnect.start(p)
	p.reconnect.stop()
}
