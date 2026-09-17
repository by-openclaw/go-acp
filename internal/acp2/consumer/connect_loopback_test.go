package acp2

import (
	"context"
	"errors"
	"testing"
	"time"

	"dhs/internal/consumer"

	"dhs/internal/acp2/codec"
)

// connect_loopback_test.go drives Plugin.Connect / Session.Connect end
// to end against the loopback harness, covering the full AN2 + ACP2
// handshake, GetDeviceInfo, GetSlotInfo, Disconnect, and the
// not-connected branches.

// connectPlugin starts a fake server with keep-alive disabled (so no
// background prober races the test) and returns a connected plugin
// plus a cleanup func.
func connectPlugin(t *testing.T, srv *fakeServer, host string, port int) *Plugin {
	t.Helper()
	p := &Plugin{logger: testLogger()}
	// Disable the keepalive prober + watchdog so tests are deterministic
	// and don't fire spurious AN2 GetSlotInfo traffic.
	p.SetKeepAlive(consumer.KeepAliveConfig{
		Interval: consumer.DisableInterval,
		Timeout:  consumer.DisableTimeout,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := p.Connect(ctx, host, port); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = p.Disconnect() })
	return p
}

func TestPluginConnect_Handshake(t *testing.T) {
	srv, host, port := newFakeServer(t)
	defer srv.stop()

	p := connectPlugin(t, srv, host, port)

	di, err := p.GetDeviceInfo(context.Background())
	if err != nil {
		t.Fatalf("GetDeviceInfo: %v", err)
	}
	if di.IP != host {
		t.Errorf("IP = %q, want %q", di.IP, host)
	}
	if di.Port != port {
		t.Errorf("Port = %d, want %d", di.Port, port)
	}
	// slotCount=2 → numSlots=2.
	if di.NumSlots != 2 {
		t.Errorf("NumSlots = %d, want 2", di.NumSlots)
	}
	if di.ProtocolVersion != 1 {
		t.Errorf("ProtocolVersion = %d, want 1", di.ProtocolVersion)
	}
}

func TestPluginConnect_SessionVersions(t *testing.T) {
	srv, host, port := newFakeServer(t)
	srv.an2Major = 1
	srv.an2Minor = 4
	srv.acp2Ver = 3
	defer srv.stop()

	p := connectPlugin(t, srv, host, port)
	p.mu.Lock()
	s := p.session
	p.mu.Unlock()

	if got := s.AN2Version(); got != "1.4" {
		t.Errorf("AN2Version = %q, want 1.4", got)
	}
	if got := s.ACP2Version(); got != 3 {
		t.Errorf("ACP2Version = %d, want 3", got)
	}
	if got := s.NumSlots(); got != 2 {
		t.Errorf("NumSlots = %d, want 2", got)
	}
	if s.Host() != host || s.Port() != port {
		t.Errorf("host/port = %s:%d, want %s:%d", s.Host(), s.Port(), host, port)
	}
}

func TestPluginConnect_AlreadyConnected(t *testing.T) {
	srv, host, port := newFakeServer(t)
	defer srv.stop()

	p := connectPlugin(t, srv, host, port)
	if err := p.Connect(context.Background(), host, port); err == nil {
		t.Fatal("second Connect: want error, got nil")
	}
}

func TestSessionConnect_AlreadyConnected(t *testing.T) {
	srv, host, port := newFakeServer(t)
	defer srv.stop()

	s := NewSession(nil, testLogger())
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := s.Connect(ctx, host, port); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = s.Disconnect() }()
	if err := s.Connect(ctx, host, port); err == nil {
		t.Fatal("second Session.Connect: want error")
	}
}

func TestSessionConnect_DialRefused(t *testing.T) {
	// Bind then immediately close to obtain a port nobody listens on.
	srv, host, port := newFakeServer(t)
	srv.stop()

	s := NewSession(nil, testLogger())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := s.Connect(ctx, host, port)
	if err == nil {
		_ = s.Disconnect()
		t.Fatal("Connect to dead port: want error")
	}
	var te *consumer.TransportError
	if !errors.As(err, &te) {
		t.Errorf("error = %v, want *consumer.TransportError", err)
	}
}

func TestGetDeviceInfo_NotConnected(t *testing.T) {
	p := &Plugin{logger: testLogger()}
	if _, err := p.GetDeviceInfo(context.Background()); err != consumer.ErrNotConnected {
		t.Errorf("err = %v, want ErrNotConnected", err)
	}
}

func TestGetSlotInfo_NotConnected(t *testing.T) {
	p := &Plugin{logger: testLogger()}
	if _, err := p.GetSlotInfo(context.Background(), 0); err != consumer.ErrNotConnected {
		t.Errorf("err = %v, want ErrNotConnected", err)
	}
}

func TestGetSlotInfo_FromHandshake(t *testing.T) {
	srv, host, port := newFakeServer(t)
	srv.slotStatus = []uint8{2, 2, 0} // controller present, card1 present, card2 empty
	defer srv.stop()

	p := connectPlugin(t, srv, host, port)

	si, err := p.GetSlotInfo(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetSlotInfo(1): %v", err)
	}
	if si.Status != consumer.SlotPresent {
		t.Errorf("slot 1 Status = %v, want SlotPresent", si.Status)
	}
	if si.State != consumer.SlotStatePresent {
		t.Errorf("slot 1 State = %v, want present", si.State)
	}

	si2, err := p.GetSlotInfo(context.Background(), 2)
	if err != nil {
		t.Fatalf("GetSlotInfo(2): %v", err)
	}
	if si2.Status != consumer.SlotNoCard {
		t.Errorf("slot 2 Status = %v, want SlotNoCard", si2.Status)
	}
}

func TestDisconnect_Idempotent(t *testing.T) {
	srv, host, port := newFakeServer(t)
	defer srv.stop()

	p := connectPlugin(t, srv, host, port)
	if err := p.Disconnect(); err != nil {
		t.Fatalf("first Disconnect: %v", err)
	}
	// Second Disconnect is a no-op.
	if err := p.Disconnect(); err != nil {
		t.Errorf("second Disconnect: %v", err)
	}
}

func TestSessionConnect_HandshakeAN2Error(t *testing.T) {
	srv, host, port := newFakeServer(t)
	srv.an2ErrOnFunc = int(codec.AN2FuncGetVersion) // fail at step 1
	defer srv.stop()

	s := NewSession(nil, testLogger())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// GetVersion error is non-fatal in the decode path (AN2 error frame
	// routes a synthetic reply); the handshake reads reply.Body which is
	// empty. Connect should still proceed past step 1 but the version
	// fields stay zero. Assert Connect succeeds and version is 0.0.
	if err := s.Connect(ctx, host, port); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = s.Disconnect() }()
	if got := s.AN2Version(); got != "0.0" {
		t.Errorf("AN2Version after error reply = %q, want 0.0", got)
	}
}
