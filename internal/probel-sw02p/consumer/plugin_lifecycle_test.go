package probelsw02p

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"testing"
	"time"

	"dhs/internal/probel-sw02p/codec"
	"dhs/internal/consumer"
	"dhs/internal/transport"
)

// quietLogger returns a slog.Logger that discards all output.
func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestNewFactoryBuildsPlugin exercises Factory.New — the constructor
// returns a *Plugin carrying the supplied logger and implementing the
// consumer.Protocol interface.
func TestNewFactoryBuildsPlugin(t *testing.T) {
	f := &Factory{}
	logger := quietLogger()
	plug := f.New(logger)
	if plug == nil {
		t.Fatal("Factory.New returned nil")
	}
	p, ok := plug.(*Plugin)
	if !ok {
		t.Fatalf("Factory.New returned %T; want *Plugin", plug)
	}
	if p.logger != logger {
		t.Error("Factory.New did not wire the supplied logger")
	}
}

// TestSetAndGetMatrixConfig round-trips MatrixConfig through the
// setter / getter, confirming the stored copy matches.
func TestSetAndGetMatrixConfig(t *testing.T) {
	p := &Plugin{logger: quietLogger()}
	if got := p.MatrixConfig(); got != (MatrixConfig{}) {
		t.Errorf("fresh MatrixConfig() = %+v; want zero value", got)
	}
	want := MatrixConfig{MatrixID: 3, Level: 2, Dsts: 64, Srcs: 32, InitialPoll: true}
	p.SetMatrixConfig(want)
	if got := p.MatrixConfig(); got != want {
		t.Errorf("MatrixConfig() = %+v; want %+v", got, want)
	}
}

// TestGetSlotInfoNotImplemented locks the slot stub — SW-P-02 has no
// slot concept.
func TestGetSlotInfoNotImplemented(t *testing.T) {
	p := &Plugin{logger: quietLogger()}
	if _, err := p.GetSlotInfo(context.Background(), 0); err != consumer.ErrNotImplemented {
		t.Errorf("GetSlotInfo err = %v; want ErrNotImplemented", err)
	}
}

// TestUnsubscribeNotImplemented locks the Unsubscribe stub.
func TestUnsubscribeNotImplemented(t *testing.T) {
	p := &Plugin{logger: quietLogger()}
	if err := p.Unsubscribe(consumer.ValueRequest{}); err != consumer.ErrNotImplemented {
		t.Errorf("Unsubscribe err = %v; want ErrNotImplemented", err)
	}
}

// TestGetDeviceInfoNotConnected: before Connect, GetDeviceInfo bubbles
// up ErrNotConnected from getClient.
func TestGetDeviceInfoNotConnected(t *testing.T) {
	p := &Plugin{logger: quietLogger()}
	if _, err := p.GetDeviceInfo(context.Background()); err != consumer.ErrNotConnected {
		t.Errorf("GetDeviceInfo err = %v; want ErrNotConnected", err)
	}
}

// TestExposeClientNotConnected: ExposeClient returns ErrNotConnected
// before Connect.
func TestExposeClientNotConnected(t *testing.T) {
	p := &Plugin{logger: quietLogger()}
	if _, err := p.ExposeClient(); err != consumer.ErrNotConnected {
		t.Errorf("ExposeClient err = %v; want ErrNotConnected", err)
	}
}

// TestIsOnlineBeforeConnect: with no metrics connector, IsOnline /
// IsOnlineWithin report offline.
func TestIsOnlineBeforeConnect(t *testing.T) {
	p := &Plugin{logger: quietLogger()}
	if p.IsOnline() {
		t.Error("IsOnline() = true before Connect; want false")
	}
	if p.IsOnlineWithin(time.Hour) {
		t.Error("IsOnlineWithin() = true before Connect; want false")
	}
}

// TestConnectDialError exercises the Connect failure path: dialing an
// address with no listener returns a *consumer.TransportError.
func TestConnectDialError(t *testing.T) {
	p := &Plugin{logger: quietLogger()}
	// Port 1 on the loopback is reserved/unused — dial fails fast.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := p.Connect(ctx, "127.0.0.1", 1)
	if err == nil {
		_ = p.Disconnect()
		t.Fatal("Connect to dead port returned nil; want error")
	}
	var te *consumer.TransportError
	if !errors.As(err, &te) {
		t.Errorf("Connect err = %T (%v); want *consumer.TransportError", err, err)
	}
}

// TestConnectIdempotentAndMismatch drives the already-connected
// branches without a real socket: a non-nil client makes a same-host
// reconnect a no-op and a different-host reconnect an error.
func TestConnectIdempotentAndMismatch(t *testing.T) {
	a, b := net.Pipe()
	defer func() {
		_ = a.Close()
		_ = b.Close()
	}()
	p := &Plugin{logger: quietLogger()}
	p.client = codec.NewClientFromConn(a, p.logger, codec.ClientConfig{})
	p.host = "10.0.0.5"
	p.port = 2002
	t.Cleanup(func() { _ = p.client.Close() })

	ctx := context.Background()
	// Same host, port 0 → idempotent no-op.
	if err := p.Connect(ctx, "10.0.0.5", 0); err != nil {
		t.Errorf("idempotent Connect (port 0) = %v; want nil", err)
	}
	// Same host, same port → idempotent no-op.
	if err := p.Connect(ctx, "10.0.0.5", 2002); err != nil {
		t.Errorf("idempotent Connect (same port) = %v; want nil", err)
	}
	// Different host → error.
	if err := p.Connect(ctx, "10.0.0.6", 2002); err == nil {
		t.Error("Connect to different host = nil; want already-connected error")
	}
}

// TestConnectDisconnectRoundTrip drives the real-socket lifecycle: a
// loopback TCP listener accepts the connection, the matrix side writes
// one tx 04 frame so OnRx fires (populating LastRxAt), and the plugin's
// IsOnline / GetDeviceInfo / ExposeClient / Disconnect paths are all
// exercised. Default port resolution (port 0) and recorder wiring are
// covered too.
func TestConnectDisconnectRoundTrip(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	// Capture file so the recorder branch in Connect is exercised.
	recPath := filepath.Join(t.TempDir(), "frames.jsonl")
	rec, err := transport.NewRecorder(recPath)
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}
	t.Cleanup(func() { _ = rec.Close() })

	port := ln.Addr().(*net.TCPAddr).Port

	served := make(chan struct{})
	go func() {
		defer close(served)
		conn, aerr := ln.Accept()
		if aerr != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		// Emit one tx 04 CONNECTED so the consumer's OnRx fires and
		// LastRxAt is set — drives the IsOnline "fresh rx" branch.
		frame := codec.EncodeConnected(codec.ConnectedParams{Destination: 1, Source: 2})
		_, _ = conn.Write(codec.Pack(frame))
		// Hold the connection open until the test tears it down.
		buf := make([]byte, 64)
		for {
			if _, rerr := conn.Read(buf); rerr != nil {
				return
			}
		}
	}()

	p := &Plugin{logger: quietLogger()}
	p.SetRecorder(rec)
	// Dsts left zero so the keepalive goroutine is a no-op (race-safe).

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := p.Connect(ctx, "127.0.0.1", port); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	if p.Metrics() == nil {
		t.Error("Metrics() nil after Connect; want non-nil")
	}
	if p.ComplianceProfile() == nil {
		t.Error("ComplianceProfile() nil after Connect; want non-nil")
	}

	cli, err := p.ExposeClient()
	if err != nil || cli == nil {
		t.Fatalf("ExposeClient after Connect: cli=%v err=%v", cli, err)
	}

	di, err := p.GetDeviceInfo(ctx)
	if err != nil {
		t.Fatalf("GetDeviceInfo: %v", err)
	}
	if di.IP != "127.0.0.1" || di.Port != port {
		t.Errorf("GetDeviceInfo = %+v; want IP=127.0.0.1 Port=%d", di, port)
	}

	// Wait for the inbound tx 04 to register as rx traffic so IsOnline
	// observes a fresh LastRxAt. Poll the metrics snapshot rather than
	// sleeping a fixed duration.
	waitForRx(t, p)
	if !p.IsOnline() {
		t.Error("IsOnline() = false after fresh rx; want true")
	}
	// A zero staleness window is never satisfiable — must read offline.
	if p.IsOnlineWithin(0) {
		t.Error("IsOnlineWithin(0) = true; want false")
	}

	if err := p.Disconnect(); err != nil {
		t.Errorf("Disconnect: %v", err)
	}
	// Metrics preserved after Disconnect (post-mortem inspection).
	if p.Metrics() == nil {
		t.Error("Metrics() nil after Disconnect; want preserved non-nil")
	}
	<-served
}

// waitForRx blocks until the plugin's metrics report a non-zero
// LastRxAt or the test deadline elapses.
func waitForRx(t *testing.T, p *Plugin) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		m := p.Metrics()
		if m != nil && !m.Snapshot().LastRxAt.IsZero() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for rx traffic to register in metrics")
}
