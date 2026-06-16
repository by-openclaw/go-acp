package probelsw02p

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"dhs/internal/consumer"
	"dhs/internal/metrics"
	"dhs/internal/probel-sw02p/codec"
	"dhs/internal/wiretrace"
)

// errForcedDecode is the synthetic error the decodeErrHook seam injects so
// the otherwise-unreachable post-Unpack decode guards (see decode_seam.go)
// run under test. codec.Unpack has already validated every frame these
// guards see, so a real codec.DecodeXxx cannot fail there.
var errForcedDecode = errors.New("probel-sw02p: forced decode error (test)")

// failFromCall installs decodeErrHook so it returns errForcedDecode on the
// nth call onward (1-based) and nil before that, then restores the previous
// hook on test cleanup. A stateful counter is how a single parameterless
// seam targets one specific decode invocation: matcher decodes run before
// the post-Send decode of the same frame, so "fail from call 2" drives a
// post-Send guard while "fail on call 1 only" drives a matcher / listener
// guard.
func failFromCall(t *testing.T, n int64) {
	t.Helper()
	var calls atomic.Int64
	prev := decodeErrHook
	decodeErrHook = func() error {
		if calls.Add(1) >= n {
			return errForcedDecode
		}
		return nil
	}
	t.Cleanup(func() { decodeErrHook = prev })
}

// failOnlyCall1 installs decodeErrHook so it fails the FIRST decode call and
// succeeds afterwards — used to reject the first candidate frame at a
// matcher / listener, proving the decode-error arm, while a second valid
// frame still drives the happy path.
func failOnlyCall1(t *testing.T) {
	t.Helper()
	var calls atomic.Int64
	prev := decodeErrHook
	decodeErrHook = func() error {
		if calls.Add(1) == 1 {
			return errForcedDecode
		}
		return nil
	}
	t.Cleanup(func() { decodeErrHook = prev })
}

// ---------------------------------------------------------------------------
// Post-Send decode guards: the matcher accepts the reply, then the post-Send
// codec.DecodeXxx(reply) wrapper is forced to fail, exercising each Send
// method's "decode tx NN" wrap-and-return arm.
// ---------------------------------------------------------------------------

func TestSendPostDecodeGuards(t *testing.T) {
	cases := []struct {
		name string
		// matcherDecodes is true when the reply matcher itself decodes the
		// frame (so the post-Send decode is the 2nd hook call); false when
		// the matcher filters on ID only (post-Send decode is the 1st call).
		matcherDecodes bool
		// reply is the valid frame the matrix sends back.
		reply codec.Frame
		// call drives the Send method; returns its error.
		call func(ctx context.Context, p *Plugin) error
	}{
		{"Interrogate", true,
			codec.EncodeTally(codec.TallyParams{Destination: 5, Source: 6}),
			func(c context.Context, p *Plugin) error { _, e := p.SendInterrogate(c, 5); return e }},
		{"Connect", true,
			codec.EncodeConnected(codec.ConnectedParams{Destination: 7, Source: 8}),
			func(c context.Context, p *Plugin) error { _, e := p.SendConnect(c, 7, 8, false); return e }},
		{"ConnectOnGo", false,
			codec.EncodeConnectOnGoAck(codec.ConnectOnGoAckParams{Destination: 1, Source: 2}),
			func(c context.Context, p *Plugin) error { _, e := p.SendConnectOnGo(c, 1, 2, false); return e }},
		{"Go", false,
			codec.EncodeGoDoneAck(codec.GoDoneAckParams{Operation: codec.GoOpSet}),
			func(c context.Context, p *Plugin) error { _, e := p.SendGo(c, codec.GoOpSet); return e }},
		{"StatusRequest", false,
			codec.EncodeStatusResponse2(codec.StatusResponse2Params{}),
			func(c context.Context, p *Plugin) error {
				_, e := p.SendStatusRequest(c, codec.ControllerLH)
				return e
			}},
		{"SourceLockStatusRequest", false,
			codec.EncodeSourceLockStatusResponse(codec.SourceLockStatusResponseParams{Locked: []bool{true, false, true, false}}),
			func(c context.Context, p *Plugin) error {
				_, e := p.SendSourceLockStatusRequest(c, codec.ControllerLH)
				return e
			}},
		{"DualControllerStatusRequest", false,
			codec.EncodeDualControllerStatusResponse(codec.DualControllerStatusResponseParams{}),
			func(c context.Context, p *Plugin) error {
				_, e := p.SendDualControllerStatusRequest(c)
				return e
			}},
		{"ConnectOnGoGroupSalvo", false,
			codec.EncodeConnectOnGoGroupSalvoAck(codec.ConnectOnGoGroupSalvoAckParams{Destination: 1, Source: 2, SalvoID: 3}),
			func(c context.Context, p *Plugin) error {
				_, e := p.SendConnectOnGoGroupSalvo(c, 1, 2, 3, false)
				return e
			}},
		// rx 36's matcher decodes via the raw codec function (not the
		// decoded() seam — that branch is already covered by the SalvoID
		// filter tests), so only the post-Send decode consults the hook:
		// it is the 1st seam call here.
		{"GoGroupSalvo", false,
			codec.EncodeGoDoneGroupSalvoAck(codec.GoDoneGroupSalvoAckParams{Result: codec.GoGroupResultSet, SalvoID: 3}),
			func(c context.Context, p *Plugin) error {
				_, e := p.SendGoGroupSalvo(c, codec.GoOpSet, 3)
				return e
			}},
		{"ExtendedInterrogate", true,
			codec.EncodeExtendedTally(codec.ExtendedTallyParams{Destination: 5000, Source: 6}),
			func(c context.Context, p *Plugin) error {
				_, e := p.SendExtendedInterrogate(c, 5000)
				return e
			}},
		{"ExtendedConnect", true,
			codec.EncodeExtendedConnected(codec.ExtendedConnectedParams{Destination: 3000, Source: 4000}),
			func(c context.Context, p *Plugin) error {
				_, e := p.SendExtendedConnect(c, 3000, 4000)
				return e
			}},
		{"ExtendedConnectOnGo", false,
			codec.EncodeExtendedConnectOnGoAck(codec.ExtendedConnectOnGoAckParams{Destination: 1, Source: 2}),
			func(c context.Context, p *Plugin) error {
				_, e := p.SendExtendedConnectOnGo(c, 1, 2)
				return e
			}},
		{"ExtendedConnectOnGoGroupSalvo", false,
			codec.EncodeExtendedConnectOnGoGroupSalvoAck(codec.ExtendedConnectOnGoGroupSalvoAckParams{Destination: 1, Source: 2, SalvoID: 3}),
			func(c context.Context, p *Plugin) error {
				_, e := p.SendExtendedConnectOnGoGroupSalvo(c, 1, 2, 3)
				return e
			}},
		{"ExtendedProtectInterrogate", true,
			codec.EncodeExtendedProtectTally(codec.ExtendedProtectTallyParams{Destination: 600, Device: 5, Protect: codec.ProtectOEM}),
			func(c context.Context, p *Plugin) error {
				_, e := p.SendExtendedProtectInterrogate(c, 600)
				return e
			}},
		{"ExtendedProtectConnect", true,
			codec.EncodeExtendedProtectConnected(codec.ExtendedProtectConnectedParams{Destination: 700, Device: 9, Protect: codec.ProtectProBel}),
			func(c context.Context, p *Plugin) error {
				_, e := p.SendExtendedProtectConnect(c, 700, 9)
				return e
			}},
		{"ExtendedProtectDisconnect", true,
			codec.EncodeExtendedProtectDisconnected(codec.ExtendedProtectDisconnectedParams{Destination: 700}),
			func(c context.Context, p *Plugin) error {
				_, e := p.SendExtendedProtectDisconnect(c, 700, 9)
				return e
			}},
		{"ProtectDeviceNameRequest", true,
			codec.EncodeProtectDeviceNameResponse(codec.ProtectDeviceNameResponseParams{Device: 42, Name: "REAL"}),
			func(c context.Context, p *Plugin) error {
				_, e := p.SendProtectDeviceNameRequest(c, 42)
				return e
			}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, peer := newPipePlugin(t)
			// Post-Send decode is the last decode call: after the matcher
			// (which decodes for matcherDecodes cases) accepts the frame.
			if tc.matcherDecodes {
				failFromCall(t, 2)
			} else {
				failFromCall(t, 1)
			}
			done := make(chan struct{})
			go func() {
				defer close(done)
				_ = readFrame(t, peer)
				writeFrame(t, peer, tc.reply)
			}()

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			err := tc.call(ctx, p)
			if err == nil {
				t.Fatalf("%s: want forced decode error, got nil", tc.name)
			}
			if !errors.Is(err, errForcedDecode) {
				t.Fatalf("%s: want errForcedDecode, got %v", tc.name, err)
			}
			<-done
		})
	}
}

// ---------------------------------------------------------------------------
// Matcher decode-error guards: the first candidate frame fails to decode
// inside the reply matcher (return-false arm), then a valid frame arrives so
// the Send still completes. Covers the matchers that decode (rx 01 / 02 / 65
// / 66 / 101 / 102 / 104 / 103).
// ---------------------------------------------------------------------------

func TestSendMatcherDecodeErrorGuards(t *testing.T) {
	cases := []struct {
		name  string
		frame codec.Frame // valid frame, sent twice; first is rejected by the forced error
		call  func(ctx context.Context, p *Plugin) error
	}{
		{"Interrogate",
			codec.EncodeTally(codec.TallyParams{Destination: 5, Source: 6}),
			func(c context.Context, p *Plugin) error { _, e := p.SendInterrogate(c, 5); return e }},
		{"Connect",
			codec.EncodeConnected(codec.ConnectedParams{Destination: 7, Source: 8}),
			func(c context.Context, p *Plugin) error { _, e := p.SendConnect(c, 7, 8, false); return e }},
		{"ExtendedInterrogate",
			codec.EncodeExtendedTally(codec.ExtendedTallyParams{Destination: 5000, Source: 6}),
			func(c context.Context, p *Plugin) error { _, e := p.SendExtendedInterrogate(c, 5000); return e }},
		{"ExtendedConnect",
			codec.EncodeExtendedConnected(codec.ExtendedConnectedParams{Destination: 3000, Source: 4000}),
			func(c context.Context, p *Plugin) error { _, e := p.SendExtendedConnect(c, 3000, 4000); return e }},
		{"ExtendedProtectInterrogate",
			codec.EncodeExtendedProtectTally(codec.ExtendedProtectTallyParams{Destination: 600, Device: 5, Protect: codec.ProtectOEM}),
			func(c context.Context, p *Plugin) error { _, e := p.SendExtendedProtectInterrogate(c, 600); return e }},
		{"ExtendedProtectConnect",
			codec.EncodeExtendedProtectConnected(codec.ExtendedProtectConnectedParams{Destination: 700, Device: 9, Protect: codec.ProtectProBel}),
			func(c context.Context, p *Plugin) error { _, e := p.SendExtendedProtectConnect(c, 700, 9); return e }},
		{"ExtendedProtectDisconnect",
			codec.EncodeExtendedProtectDisconnected(codec.ExtendedProtectDisconnectedParams{Destination: 700}),
			func(c context.Context, p *Plugin) error { _, e := p.SendExtendedProtectDisconnect(c, 700, 9); return e }},
		{"ProtectDeviceNameRequest",
			codec.EncodeProtectDeviceNameResponse(codec.ProtectDeviceNameResponseParams{Device: 42, Name: "REAL"}),
			func(c context.Context, p *Plugin) error { _, e := p.SendProtectDeviceNameRequest(c, 42); return e }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, peer := newPipePlugin(t)
			// Call 1 (matcher decode of the first frame) fails → reject;
			// call 2 (matcher decode of the second frame) succeeds → accept;
			// call 3 (post-Send decode) succeeds → happy path.
			failOnlyCall1(t)
			done := make(chan struct{})
			go func() {
				defer close(done)
				_ = readFrame(t, peer)
				writeFrame(t, peer, tc.frame) // rejected at matcher (forced decode error)
				writeFrame(t, peer, tc.frame) // accepted
			}()

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := tc.call(ctx, p); err != nil {
				t.Fatalf("%s: matcher should skip the first frame and accept the second; got %v", tc.name, err)
			}
			<-done
		})
	}
}

// ---------------------------------------------------------------------------
// Listener decode-error guards: the first frame fails to decode inside the
// Subscribe listener (skip arm), the second decodes and fires the callback.
// ---------------------------------------------------------------------------

func TestSubscribeListenerDecodeErrorGuards(t *testing.T) {
	t.Run("ExtendedProtectTally", func(t *testing.T) {
		p, peer := newPipePlugin(t)
		failOnlyCall1(t)
		got := make(chan codec.ExtendedProtectTallyParams, 1)
		if err := p.SubscribeExtendedProtectTally(func(v codec.ExtendedProtectTallyParams) { got <- v }); err != nil {
			t.Fatalf("Subscribe: %v", err)
		}
		writeFrame(t, peer, codec.EncodeExtendedProtectTally(codec.ExtendedProtectTallyParams{Destination: 7})) // skipped
		writeFrame(t, peer, codec.EncodeExtendedProtectTally(codec.ExtendedProtectTallyParams{Destination: 8})) // fires
		awaitTally(t, got)
	})

	t.Run("ExtendedProtectConnected", func(t *testing.T) {
		p, peer := newPipePlugin(t)
		failOnlyCall1(t)
		got := make(chan codec.ExtendedProtectConnectedParams, 1)
		if err := p.SubscribeExtendedProtectConnected(func(v codec.ExtendedProtectConnectedParams) { got <- v }); err != nil {
			t.Fatalf("Subscribe: %v", err)
		}
		writeFrame(t, peer, codec.EncodeExtendedProtectConnected(codec.ExtendedProtectConnectedParams{Destination: 7}))
		writeFrame(t, peer, codec.EncodeExtendedProtectConnected(codec.ExtendedProtectConnectedParams{Destination: 8}))
		select {
		case <-got:
		case <-time.After(2 * time.Second):
			t.Fatal("listener did not fire after skipped frame")
		}
	})

	t.Run("ExtendedProtectDisconnected", func(t *testing.T) {
		p, peer := newPipePlugin(t)
		failOnlyCall1(t)
		got := make(chan codec.ExtendedProtectDisconnectedParams, 1)
		if err := p.SubscribeExtendedProtectDisconnected(func(v codec.ExtendedProtectDisconnectedParams) { got <- v }); err != nil {
			t.Fatalf("Subscribe: %v", err)
		}
		writeFrame(t, peer, codec.EncodeExtendedProtectDisconnected(codec.ExtendedProtectDisconnectedParams{Destination: 7}))
		writeFrame(t, peer, codec.EncodeExtendedProtectDisconnected(codec.ExtendedProtectDisconnectedParams{Destination: 8}))
		select {
		case <-got:
		case <-time.After(2 * time.Second):
			t.Fatal("listener did not fire after skipped frame")
		}
	})

	t.Run("ExtendedProtectTallyDump", func(t *testing.T) {
		p, peer := newPipePlugin(t)
		failOnlyCall1(t)
		got := make(chan codec.ExtendedProtectTallyDumpParams, 1)
		if err := p.SubscribeExtendedProtectTallyDump(func(v codec.ExtendedProtectTallyDumpParams) { got <- v }); err != nil {
			t.Fatalf("Subscribe: %v", err)
		}
		writeFrame(t, peer, codec.EncodeExtendedProtectTallyDump(codec.ExtendedProtectTallyDumpParams{Reset: true}))
		writeFrame(t, peer, codec.EncodeExtendedProtectTallyDump(codec.ExtendedProtectTallyDumpParams{Reset: true}))
		select {
		case <-got:
		case <-time.After(2 * time.Second):
			t.Fatal("listener did not fire after skipped frame")
		}
	})

	t.Run("ProtectDeviceNameRequest", func(t *testing.T) {
		p, peer := newPipePlugin(t)
		failOnlyCall1(t)
		got := make(chan codec.ProtectDeviceNameRequestParams, 1)
		if err := p.SubscribeProtectDeviceNameRequest(func(v codec.ProtectDeviceNameRequestParams) { got <- v }); err != nil {
			t.Fatalf("Subscribe: %v", err)
		}
		writeFrame(t, peer, codec.EncodeProtectDeviceNameRequest(codec.ProtectDeviceNameRequestParams{Device: 1}))
		writeFrame(t, peer, codec.EncodeProtectDeviceNameRequest(codec.ProtectDeviceNameRequestParams{Device: 2}))
		select {
		case <-got:
		case <-time.After(2 * time.Second):
			t.Fatal("listener did not fire after skipped frame")
		}
	})
}

func awaitTally(t *testing.T, got chan codec.ExtendedProtectTallyParams) {
	t.Helper()
	select {
	case <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("listener did not fire after skipped frame")
	}
}

// ---------------------------------------------------------------------------
// rx 075 ROUTER CONFIGURATION REQUEST: both per-variant decode guards plus
// the trailing "unexpected reply ID" invariant.
// ---------------------------------------------------------------------------

// TestSendRouterConfigResponse1DecodeGuard forces tx 076 decode to fail.
func TestSendRouterConfigResponse1DecodeGuard(t *testing.T) {
	p, peer := newPipePlugin(t)
	failFromCall(t, 1) // ID-only matcher; first decode is the post-Send tx 076 decode
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = readFrame(t, peer)
		writeFrame(t, peer, codec.EncodeRouterConfigResponse1(codec.RouterConfigResponse1Params{}))
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := p.SendRouterConfigRequest(ctx)
	if !errors.Is(err, errForcedDecode) {
		t.Fatalf("want errForcedDecode, got %v", err)
	}
	<-done
}

// TestSendRouterConfigResponse2DecodeGuard forces tx 077 decode to fail.
func TestSendRouterConfigResponse2DecodeGuard(t *testing.T) {
	p, peer := newPipePlugin(t)
	failFromCall(t, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = readFrame(t, peer)
		writeFrame(t, peer, codec.EncodeRouterConfigResponse2(codec.RouterConfigResponse2Params{}))
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := p.SendRouterConfigRequest(ctx)
	if !errors.Is(err, errForcedDecode) {
		t.Fatalf("want errForcedDecode, got %v", err)
	}
	<-done
}

// TestSendRouterConfigUnexpectedReplyID drives the §3.2.57 invariant: the
// matcher is widened (via routerConfigExtraMatch) to accept a tx 03 TALLY
// frame the post-Send switch does not handle, so SendRouterConfigRequest
// falls through to the "unexpected reply ID" guard.
func TestSendRouterConfigUnexpectedReplyID(t *testing.T) {
	p, peer := newPipePlugin(t)
	prev := routerConfigExtraMatch
	routerConfigExtraMatch = func(f codec.Frame) bool { return f.ID == codec.TxTally }
	t.Cleanup(func() { routerConfigExtraMatch = prev })

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = readFrame(t, peer)
		writeFrame(t, peer, codec.EncodeTally(codec.TallyParams{Destination: 1, Source: 2}))
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := p.SendRouterConfigRequest(ctx)
	if err == nil || !strings.Contains(err.Error(), "unexpected reply ID") {
		t.Fatalf("want unexpected reply ID error, got %v", err)
	}
	<-done
}

// ---------------------------------------------------------------------------
// validate.go unknown-command invariant: commandName seam returns "" for a
// frame that Unpack accepted, driving the §3 catalogue-coverage invariant.
// ---------------------------------------------------------------------------

func TestValidateUnknownCommandInvariant(t *testing.T) {
	prev := commandName
	commandName = func(codec.CommandID) string { return "" }
	t.Cleanup(func() { commandName = prev })

	// A real, well-formed rx 06 GO frame — Unpack accepts it; the seam then
	// reports it as not-in-catalogue, exercising the invariant branch.
	trames := []wiretrace.Trame{
		{Direction: wiretrace.DirectionRx, Hex: frameHex(codec.EncodeGo(codec.GoParams{Operation: codec.GoOpSet}))},
	}
	p := &Plugin{logger: quietLogger()}
	report, err := p.Validate(context.Background(), trames, consumer.ValidateOpts{})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(report.Invariants) == 0 {
		t.Fatal("want an unknown-command invariant, got none")
	}
	if !strings.Contains(report.Invariants[0], "unknown SW-P-02 command id") {
		t.Errorf("unexpected invariant text: %q", report.Invariants[0])
	}
}

// ---------------------------------------------------------------------------
// plugin.go OnRx / OnTx non-frame fallback: observeRxBytes / observeTxBytes
// fed a sub-frame byte slice take the un-attributed aggregate-only arm. The
// live codec only ever hands these observers fully-Unpacked frames.
// ---------------------------------------------------------------------------

func TestObserveBytesNonFrameFallback(t *testing.T) {
	met := metrics.NewConnector()
	short := []byte{0x01} // < 3 bytes, no SOM → probelCmdFromBytes returns false

	observeRxBytes(met, short)
	observeTxBytes(met, short)

	snap := met.Snapshot()
	if snap.RxFrames != 1 || snap.TxFrames != 1 {
		t.Fatalf("aggregate counters: rx=%d tx=%d; want 1/1", snap.RxFrames, snap.TxFrames)
	}
	for i, n := range snap.RxBytesByCmd {
		if n != 0 {
			t.Fatalf("per-cmd rx attribution at %d = %d; want 0 (fallback must not attribute)", i, n)
		}
	}

	// Sanity: a real frame DOES attribute per-command, proving the two arms
	// diverge.
	frame := codec.Pack(codec.EncodeGo(codec.GoParams{Operation: codec.GoOpSet}))
	met2 := metrics.NewConnector()
	observeRxBytes(met2, frame)
	if met2.Snapshot().RxBytesByCmd[uint8(codec.RxGo)] == 0 {
		t.Fatal("framed bytes were not attributed per-command")
	}
}
