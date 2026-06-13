package acp1

import (
	"context"
	"log/slog"
	"net"
	"path/filepath"
	"testing"

	"dhs/internal/consumer"
	"dhs/internal/transport"
)

// TestConnect_UDP_WithRecorder covers connectUDP's recorder-wrap branch.
func TestConnect_UDP_WithRecorder(t *testing.T) {
	capPath := filepath.Join(t.TempDir(), "cap.jsonl")
	rec, err := transport.NewRecorder(capPath)
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}
	p := &Plugin{logger: slog.Default()}
	p.SetRecorder(rec)
	if err := p.Connect(context.Background(), "127.0.0.1", 0); err != nil {
		t.Fatalf("UDP connect with recorder: %v", err)
	}
	// Tear down + close the recorder BEFORE the TempDir cleanup runs so the
	// capture file handle is released on Windows.
	_ = p.Disconnect()
	_ = rec.Close()
}

// freePort returns an ephemeral TCP port that is then released, so a
// connect attempt to it is refused.
func freeTCPPortRefused(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	p := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return p
}

// TestConnect_AutoTCPSuccess: TransportAuto with a live TCP server resolves
// to TCP (the err==nil success arm).
func TestConnect_AutoTCPSuccess(t *testing.T) {
	host, port, stop := echoTCPServer(t, []byte{0x00, 0x05}, false)
	defer stop()
	p := &Plugin{logger: slog.Default()}
	p.SetTransport(TransportAuto)
	if err := p.Connect(context.Background(), host, port); err != nil {
		t.Fatalf("auto connect to live TCP: %v", err)
	}
	t.Cleanup(func() { _ = p.Disconnect() })
	if p.Transport() != TransportTCPDirect {
		t.Errorf("auto resolved to %v, want tcp", p.Transport())
	}
}

// TestConnect_UDPDialError: a host that fails to resolve drives DialUDP's
// error → connectUDP returns, surfacing from the default UDP case.
func TestConnect_UDPDialError(t *testing.T) {
	p := &Plugin{logger: slog.Default()}
	// An unresolvable host name forces DialUDP to fail.
	if err := p.Connect(context.Background(), "no-such-host.invalid.", 2071); err == nil {
		_ = p.Disconnect()
		t.Fatal("UDP connect to unresolvable host: want error")
	}
}

// TestConnect_AutoUDPFallbackDialError: Auto path where TCP is refused AND
// the UDP fallback's DialUDP also fails (unresolvable host).
func TestConnect_AutoUDPFallbackDialError(t *testing.T) {
	p := &Plugin{logger: slog.Default()}
	p.SetTransport(TransportAuto)
	if err := p.Connect(context.Background(), "no-such-host.invalid.", 2071); err == nil {
		_ = p.Disconnect()
		t.Fatal("auto connect to unresolvable host: want error")
	}
}

// TestConnect_AN2DefaultPort: AN2 with port 0 defaults to AN2DefaultPort
// (drives the port==DefaultPort → AN2DefaultPort branch). The connect to a
// dead port still errors, which is fine — we only need the branch.
func TestConnect_AN2DefaultPort(t *testing.T) {
	p := &Plugin{logger: slog.Default()}
	p.SetTransport(TransportAN2)
	// port 0 → defaulted to codec.DefaultPort upstream, then remapped to
	// AN2DefaultPort inside the AN2 case. Nothing is listening → error.
	if err := p.Connect(context.Background(), "127.0.0.1", 0); err == nil {
		_ = p.Disconnect()
		t.Fatal("AN2 connect to default (dead) port: want error")
	}
}

// TestConnect_TCPRefused: a TCP-direct connect to a closed port surfaces a
// transport error (connectTCP dial-fail arm).
func TestConnect_TCPRefused(t *testing.T) {
	p := &Plugin{logger: slog.Default()}
	p.SetTransport(TransportTCPDirect)
	if err := p.Connect(context.Background(), "127.0.0.1", freeTCPPortRefused(t)); err == nil {
		t.Fatal("TCP connect to refused port: want error")
		_ = p.Disconnect()
	}
}

// TestConnect_AN2Refused: AN2 connect to a closed port surfaces a transport
// error (connectAN2 dial-fail arm).
func TestConnect_AN2Refused(t *testing.T) {
	p := &Plugin{logger: slog.Default()}
	p.SetTransport(TransportAN2)
	if err := p.Connect(context.Background(), "127.0.0.1", freeTCPPortRefused(t)); err == nil {
		t.Fatal("AN2 connect to refused port: want error")
		_ = p.Disconnect()
	}
}

// TestConnect_UDP_ListenerBindFail: an injected listener factory that fails
// drives connectUDP's listener-unavailable warn branch. Connect still
// succeeds (the announcement listener is best-effort).
func TestConnect_UDP_ListenerBindFail(t *testing.T) {
	p := &Plugin{logger: slog.Default()}
	p.newListener = func(*slog.Logger, int) (*Listener, error) {
		return nil, errListenerUnavailable
	}
	if err := p.Connect(context.Background(), "127.0.0.1", 0); err != nil {
		t.Fatalf("UDP connect should still succeed despite listener bind fail: %v", err)
	}
	t.Cleanup(func() { _ = p.Disconnect() })
	if p.listener != nil {
		t.Error("listener should be nil when the factory fails")
	}
}

var errListenerUnavailable = &listenerErr{}

type listenerErr struct{}

func (*listenerErr) Error() string { return "synthetic listener bind failure" }

// TestGetDeviceInfo_DoError: a transport error inside Do surfaces from
// GetDeviceInfo (Do-error arm, not IsError).
func TestGetDeviceInfo_DoError(t *testing.T) {
	p, _, _ := newPluginWithClient(t) // empty recv → io.EOF from Do
	if _, err := p.GetDeviceInfo(context.Background()); err == nil {
		t.Fatal("Do error: want error")
	}
}

// TestGetSlotInfo_DoError mirrors the above for GetSlotInfo.
func TestGetSlotInfo_DoError(t *testing.T) {
	p, _, _ := newPluginWithClient(t)
	p.trees = newSlotTreeCache(8, 0)
	if _, err := p.GetSlotInfo(context.Background(), 0); err == nil {
		t.Fatal("Do error: want error")
	}
}

// TestGetValue_DoError: getValue's Do error arm.
func TestGetValue_DoError(t *testing.T) {
	p, _, _ := newPluginWithClient(t)
	p.trees = newSlotTreeCache(8, 0)
	if _, err := p.GetValue(context.Background(),
		consumer.ValueRequest{Slot: 0, Group: "control", ID: 5}); err == nil {
		t.Fatal("Do error: want error")
	}
}

// TestSetValue_DoError: setValue's Do error arm (raw escape hatch so no
// meta-fetch is needed before the send).
func TestSetValue_DoError(t *testing.T) {
	p, _, _ := newPluginWithClient(t)
	p.trees = newSlotTreeCache(8, 0)
	if _, err := p.SetValue(context.Background(),
		consumer.ValueRequest{Slot: 0, Group: "control", ID: 5},
		consumer.Value{Raw: []byte{0x00, 0x01}}); err == nil {
		t.Fatal("Do error: want error")
	}
}
