package probelsw08p

// Cross-layer integration tests: spin up the real provider server on
// a random loopback port, connect the real consumer Plugin, and drive
// end-to-end traffic through the full codec + dispatcher + handler +
// session stack. Unlike the codec-layer retry tests (net.Pipe + fake
// peer) these use a real TCP listener, so they catch regressions in
// ack/dispatch/stream paths that a pipe would hide.

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"acp/internal/export/canonical"
	"acp/internal/probel-sw08p/codec"
	cons "acp/internal/probel-sw08p/consumer"
)

// emptyExport is the smallest tree the provider accepts — enough to
// serve the scaffold, not enough to exercise crosspoint state.
func emptyExport() *canonical.Export {
	return &canonical.Export{
		Root: &canonical.Node{
			Header: canonical.Header{Number: 1, Identifier: "router", OID: "1"},
		},
	}
}

// startProvider boots a real provider on 127.0.0.1:0 and returns its
// bound address + a shutdown function. The test fails fast if the
// listener never comes up within 2 s.
func startProvider(t *testing.T, exp *canonical.Export) (string, func()) {
	t.Helper()
	srv := newServer(slog.New(slog.NewTextHandler(io.Discard, nil)), exp)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx, "127.0.0.1:0") }()

	deadline := time.Now().Add(2 * time.Second)
	var addr string
	for time.Now().Before(deadline) {
		srv.mu.Lock()
		if srv.listener != nil {
			addr = srv.listener.Addr().String()
		}
		srv.mu.Unlock()
		if addr != "" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if addr == "" {
		cancel()
		t.Fatal("provider never bound")
	}
	return addr, func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("provider Serve did not return after ctx cancel")
		}
	}
}

// portOf extracts the numeric port from "host:port".
func portOf(addr string) int {
	var port int
	sawColon := false
	for _, c := range addr {
		if c == ':' {
			sawColon = true
			port = 0
			continue
		}
		if sawColon {
			port = port*10 + int(c-'0')
		}
	}
	return port
}

// newConsumer builds a consumer Plugin via the registered Factory so
// the integration tests use the same construction path as production.
func newConsumer() *cons.Plugin {
	f := &cons.Factory{}
	return f.New(slog.New(slog.NewTextHandler(io.Discard, nil))).(*cons.Plugin)
}

// TestIntegrationMaintenanceRoundTrip (S-Int-A): consumer issues a
// Maintenance request, the provider ACKs at framer level, the consumer
// sees no NAK / timeout / retry, and the consumer's metrics observe at
// least one tx frame for the maintenance cmd.
func TestIntegrationMaintenanceRoundTrip(t *testing.T) {
	addr, stop := startProvider(t, emptyExport())
	defer stop()

	p := newConsumer()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := p.Connect(ctx, "127.0.0.1", portOf(addr)); err != nil {
		t.Fatalf("consumer Connect: %v", err)
	}
	defer func() { _ = p.Disconnect() }()

	cli, err := p.ExposeClient()
	if err != nil {
		t.Fatalf("ExposeClient: %v", err)
	}

	sendCtx, cancelSend := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelSend()
	if _, err := cli.Send(sendCtx,
		codec.Frame{ID: codec.RxMaintenance, Payload: []byte{byte(codec.MaintSoftReset)}},
		nil); err != nil {
		t.Fatalf("Send maintenance: %v", err)
	}

	s := p.Metrics().Snapshot()
	if s.TxFrames < 1 {
		t.Errorf("tx frames = %d, want >= 1", s.TxFrames)
	}
	if s.NAKs != 0 || s.Timeouts != 0 || s.Retries != 0 {
		t.Errorf("unexpected errors: naks=%d to=%d retry=%d", s.NAKs, s.Timeouts, s.Retries)
	}
}

// TestIntegrationStreamingTallyDump (S-Int-B): provider holds a level
// with 200 targets; consumer issues rx021 Crosspoint Tally Dump Request.
// The W3 streaming emitter must split the reply into multiple tx022
// frames (each ≤ 128 tallies). The test subscribes to async events,
// accumulates covered destinations, and asserts both coverage == 200
// and that the stream landed in > 1 frame.
func TestIntegrationStreamingTallyDump(t *testing.T) {
	exp := &canonical.Export{
		Root: &canonical.Node{
			Header: canonical.Header{Number: 1, Identifier: "router", OID: "1",
				Children: []canonical.Element{
					&canonical.Matrix{
						Header: canonical.Header{Number: 1, Identifier: "matrix-0", OID: "1.1"},
						Type:   canonical.MatrixOneToN, Mode: canonical.ModeLinear,
						TargetCount: 200, SourceCount: 200,
						Labels: []canonical.MatrixLabel{{BasePath: "router.matrix-0.level-0"}},
					},
				},
			},
		},
	}
	addr, stop := startProvider(t, exp)
	defer stop()

	p := newConsumer()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := p.Connect(ctx, "127.0.0.1", portOf(addr)); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = p.Disconnect() }()

	cli, err := p.ExposeClient()
	if err != nil {
		t.Fatalf("ExposeClient: %v", err)
	}

	var (
		mu      sync.Mutex
		dstSeen = make(map[int]bool)
		frames  atomic.Int32
		done    = make(chan struct{})
	)
	cli.Subscribe(func(f codec.Frame) {
		if f.ID != codec.TxCrosspointTallyDumpByte && f.ID != codec.TxCrosspointTallyDumpWord {
			return
		}
		frames.Add(1)
		var first, n int
		if f.ID == codec.TxCrosspointTallyDumpByte {
			if len(f.Payload) < 3 {
				return
			}
			n = int(f.Payload[1])
			first = int(f.Payload[2])
		} else {
			if len(f.Payload) < 4 {
				return
			}
			n = int(f.Payload[1])
			first = int(f.Payload[2])*256 + int(f.Payload[3])
		}
		mu.Lock()
		for i := 0; i < n; i++ {
			dstSeen[first+i] = true
		}
		covered := len(dstSeen)
		mu.Unlock()
		if covered >= 200 {
			select {
			case <-done:
			default:
				close(done)
			}
		}
	})

	sendCtx, cancelSend := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelSend()
	if _, err := cli.Send(sendCtx, codec.EncodeCrosspointTallyDumpRequest(
		codec.CrosspointTallyDumpRequestParams{MatrixID: 0, LevelID: 0}),
		nil); err != nil {
		t.Fatalf("Send dump request: %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		mu.Lock()
		covered := len(dstSeen)
		mu.Unlock()
		t.Fatalf("stream did not cover all 200 dsts: got %d after %d frames",
			covered, frames.Load())
	}
	if frames.Load() < 2 {
		t.Errorf("expected streaming split into multiple frames; got %d", frames.Load())
	}
}

// TestIntegrationSalvoBroadcastsConnectedToAllSessions (S-Int-D):
// Opens two real TCP sessions (A + B). Session A fires a batched salvo:
// one cmd 121 Clear, three cmd 120 Build frames (dst=0/1/2, src=9,
// sid=5), then one cmd 121 Set. On the Set:
//
//   - A (originator) receives exactly 1 × cmd 123 GoDoneAck(Set) +
//     3 × cmd 04 Connected (one per applied slot).
//   - B (non-originator) receives exactly 3 × cmd 04 Connected
//     (fanned out via tallies).
//   - Neither session receives any NAK.
//
// Locks the §3.2.3-over-§3.2.30 interpretation: the matrix MUST emit
// cmd 04 per applied slot on salvo commit so every connected
// controller's tally UI stays in sync. Regression guard for #92.
func TestIntegrationSalvoBroadcastsConnectedToAllSessions(t *testing.T) {
	exp := &canonical.Export{
		Root: &canonical.Node{
			Header: canonical.Header{Number: 1, Identifier: "router", OID: "1",
				Children: []canonical.Element{
					&canonical.Matrix{
						Header: canonical.Header{Number: 1, Identifier: "matrix-0", OID: "1.1"},
						Type:   canonical.MatrixOneToN, Mode: canonical.ModeLinear,
						TargetCount: 16, SourceCount: 16,
						Labels: []canonical.MatrixLabel{{BasePath: "router.matrix-0.level-0"}},
					},
				},
			},
		},
	}
	addr, stop := startProvider(t, exp)
	defer stop()

	// Session A — the salvo originator.
	pA := newConsumer()
	ctxA, cancelA := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelA()
	if err := pA.Connect(ctxA, "127.0.0.1", portOf(addr)); err != nil {
		t.Fatalf("A Connect: %v", err)
	}
	defer func() { _ = pA.Disconnect() }()

	// Session B — a passive listener.
	pB := newConsumer()
	ctxB, cancelB := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelB()
	if err := pB.Connect(ctxB, "127.0.0.1", portOf(addr)); err != nil {
		t.Fatalf("B Connect: %v", err)
	}
	defer func() { _ = pB.Disconnect() }()

	// Count incoming cmd 04 Connected + cmd 123 GoDoneAck on both
	// sides. Payloads captured so the test asserts dst/src correctness,
	// not just the count.
	var (
		aMu            sync.Mutex
		aConnected     []codec.CrosspointConnectedParams
		aGoDoneStatus  []codec.SalvoGoDoneStatus
		bMu            sync.Mutex
		bConnected     []codec.CrosspointConnectedParams
	)
	cliA, err := pA.ExposeClient()
	if err != nil {
		t.Fatalf("A ExposeClient: %v", err)
	}
	cliA.Subscribe(func(f codec.Frame) {
		switch f.ID {
		case codec.TxCrosspointConnected:
			if dec, err := codec.DecodeCrosspointConnected(f); err == nil {
				aMu.Lock()
				aConnected = append(aConnected, dec)
				aMu.Unlock()
			}
		case codec.TxSalvoGoDoneAck:
			if dec, err := codec.DecodeSalvoGoDoneAck(f); err == nil {
				aMu.Lock()
				aGoDoneStatus = append(aGoDoneStatus, dec.Status)
				aMu.Unlock()
			}
		}
	})

	cliB, err := pB.ExposeClient()
	if err != nil {
		t.Fatalf("B ExposeClient: %v", err)
	}
	cliB.Subscribe(func(f codec.Frame) {
		if f.ID != codec.TxCrosspointConnected {
			return
		}
		if dec, err := codec.DecodeCrosspointConnected(f); err == nil {
			bMu.Lock()
			bConnected = append(bConnected, dec)
			bMu.Unlock()
		}
	})

	// Build the salvo from A — Clear first to guarantee slot 5 is empty,
	// then 3 Builds, then Set.
	sendCtx, cancelSend := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelSend()

	if _, err := cliA.Send(sendCtx,
		codec.EncodeSalvoGo(codec.SalvoGoParams{Op: codec.SalvoOpClear, SalvoID: 5}),
		nil); err != nil {
		t.Fatalf("A Clear: %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, err := cliA.Send(sendCtx, codec.EncodeSalvoConnectOnGo(codec.SalvoConnectOnGoParams{
			MatrixID: 0, LevelID: 0, DestinationID: uint16(i), SourceID: 9, SalvoID: 5,
		}), nil); err != nil {
			t.Fatalf("A Build dst=%d: %v", i, err)
		}
	}
	if _, err := cliA.Send(sendCtx,
		codec.EncodeSalvoGo(codec.SalvoGoParams{Op: codec.SalvoOpSet, SalvoID: 5}),
		nil); err != nil {
		t.Fatalf("A Set: %v", err)
	}

	// Wait until BOTH A (via streamToSender) and B (via fanOutTally)
	// have observed their 3 cmd 04s. On fast kernels this races the
	// assertions below — Linux CI hit "A received 1 cmd 04; want 3"
	// because the streamToSender delivery was still in flight when the
	// test snapshotted aConnected.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		aMu.Lock()
		nA := len(aConnected)
		aMu.Unlock()
		bMu.Lock()
		nB := len(bConnected)
		bMu.Unlock()
		if nA >= 3 && nB >= 3 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Session A (originator) — 1 × GoDoneAck Set + 3 × cmd 04.
	aMu.Lock()
	gotA := append([]codec.CrosspointConnectedParams(nil), aConnected...)
	gotAStatus := append([]codec.SalvoGoDoneStatus(nil), aGoDoneStatus...)
	aMu.Unlock()
	// GoDoneAck: Clear (empty → None) + Set (applied → Set).
	if len(gotAStatus) < 2 {
		t.Fatalf("A received %d GoDoneAcks; want at least 2 (Clear+Set)", len(gotAStatus))
	}
	if gotAStatus[len(gotAStatus)-1] != codec.SalvoDoneSet {
		t.Errorf("A last GoDoneAck = %#x; want SalvoDoneSet", gotAStatus[len(gotAStatus)-1])
	}
	if len(gotA) != 3 {
		t.Fatalf("A received %d cmd 04 Connected; want 3 (one per applied slot)",
			len(gotA))
	}
	for i, dec := range gotA {
		if dec.DestinationID != uint16(i) || dec.SourceID != 9 {
			t.Errorf("A cmd 04 #%d = (dst=%d, src=%d); want (%d, 9)",
				i, dec.DestinationID, dec.SourceID, i)
		}
	}

	// Session B (non-originator) — 3 × cmd 04, same contents.
	bMu.Lock()
	gotB := append([]codec.CrosspointConnectedParams(nil), bConnected...)
	bMu.Unlock()
	if len(gotB) != 3 {
		t.Fatalf("B received %d cmd 04 Connected; want 3 (fan-out to listener)",
			len(gotB))
	}
	for i, dec := range gotB {
		if dec.DestinationID != uint16(i) || dec.SourceID != 9 {
			t.Errorf("B cmd 04 #%d = (dst=%d, src=%d); want (%d, 9)",
				i, dec.DestinationID, dec.SourceID, i)
		}
	}

	// Zero NAKs on either side.
	if s := pA.Metrics().Snapshot(); s.NAKs != 0 {
		t.Errorf("A NAKs = %d; want 0", s.NAKs)
	}
	if s := pB.Metrics().Snapshot(); s.NAKs != 0 {
		t.Errorf("B NAKs = %d; want 0", s.NAKs)
	}
}

// TestIntegrationReconnect (S-Int-C): Disconnect and immediately
// re-Connect — the second Connect must succeed and the new session
// must pass traffic end-to-end. Exercises the Plugin's connection-
// handoff path to catch "second Connect reuses stale client" bugs.
func TestIntegrationReconnect(t *testing.T) {
	addr, stop := startProvider(t, emptyExport())
	defer stop()

	p := newConsumer()

	for i := 0; i < 3; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		if err := p.Connect(ctx, "127.0.0.1", portOf(addr)); err != nil {
			cancel()
			t.Fatalf("Connect #%d: %v", i, err)
		}
		cli, err := p.ExposeClient()
		if err != nil {
			cancel()
			t.Fatalf("ExposeClient #%d: %v", i, err)
		}
		sendCtx, cancelSend := context.WithTimeout(context.Background(), 1*time.Second)
		if _, err := cli.Send(sendCtx,
			codec.Frame{ID: codec.RxMaintenance, Payload: []byte{byte(codec.MaintSoftReset)}},
			nil); err != nil {
			cancelSend()
			cancel()
			t.Fatalf("Send #%d: %v", i, err)
		}
		cancelSend()
		cancel()
		if err := p.Disconnect(); err != nil {
			t.Fatalf("Disconnect #%d: %v", i, err)
		}
	}
}
