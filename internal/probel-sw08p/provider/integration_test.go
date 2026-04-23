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
