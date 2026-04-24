package probelsw02p

import (
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"acp/internal/probel-sw02p/codec"
)

// TestSubscribeExtendedProtectTally confirms a matrix-side tx 96 is
// delivered to the registered listener.
func TestSubscribeExtendedProtectTally(t *testing.T) {
	a, b := net.Pipe()
	defer func() {
		_ = a.Close()
		_ = b.Close()
	}()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	disable := false
	client := codec.NewClientFromConn(a, logger, codec.ClientConfig{WireHexLog: &disable})

	p := &Plugin{logger: logger}
	p.client = client

	got := make(chan codec.ExtendedProtectTallyParams, 1)
	if err := p.SubscribeExtendedProtectTally(func(params codec.ExtendedProtectTallyParams) {
		got <- params
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	// Matrix writes a tx 96 frame onto b.
	frame := codec.EncodeExtendedProtectTally(codec.ExtendedProtectTallyParams{
		Protect: codec.ProtectOEM, Destination: 5000, Device: 9000,
	})
	if _, err := b.Write(codec.Pack(frame)); err != nil {
		t.Fatalf("matrix write: %v", err)
	}

	select {
	case params := <-got:
		if params.Protect != codec.ProtectOEM || params.Destination != 5000 || params.Device != 9000 {
			t.Errorf("listener got %+v", params)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("listener did not fire")
	}
	_ = client.Close()
}

// TestSubscribeExtendedProtectConnected exercises the tx 97 listener.
func TestSubscribeExtendedProtectConnected(t *testing.T) {
	a, b := net.Pipe()
	defer func() {
		_ = a.Close()
		_ = b.Close()
	}()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	disable := false
	client := codec.NewClientFromConn(a, logger, codec.ClientConfig{WireHexLog: &disable})
	p := &Plugin{logger: logger}
	p.client = client

	got := make(chan codec.ExtendedProtectConnectedParams, 1)
	_ = p.SubscribeExtendedProtectConnected(func(params codec.ExtendedProtectConnectedParams) {
		got <- params
	})

	f := codec.EncodeExtendedProtectConnected(codec.ExtendedProtectConnectedParams{
		Protect: codec.ProtectProBel, Destination: 1, Device: 2,
	})
	_, _ = b.Write(codec.Pack(f))
	select {
	case params := <-got:
		if params.Protect != codec.ProtectProBel || params.Destination != 1 || params.Device != 2 {
			t.Errorf("got %+v", params)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("listener did not fire")
	}
	_ = client.Close()
}

// TestSubscribeExtendedProtectNotConnected verifies the error path.
func TestSubscribeExtendedProtectNotConnected(t *testing.T) {
	p := &Plugin{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if err := p.SubscribeExtendedProtectTally(func(codec.ExtendedProtectTallyParams) {}); err == nil {
		t.Error("SubscribeExtendedProtectTally on unconnected plugin returned nil")
	}
	if err := p.SubscribeExtendedProtectConnected(func(codec.ExtendedProtectConnectedParams) {}); err == nil {
		t.Error("SubscribeExtendedProtectConnected on unconnected plugin returned nil")
	}
	if err := p.SubscribeExtendedProtectDisconnected(func(codec.ExtendedProtectDisconnectedParams) {}); err == nil {
		t.Error("SubscribeExtendedProtectDisconnected on unconnected plugin returned nil")
	}
}
