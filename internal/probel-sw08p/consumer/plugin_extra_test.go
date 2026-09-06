package probelsw08p

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"testing"
	"time"

	"dhs/internal/consumer"
	"dhs/internal/export/canonical"
	"dhs/internal/probel-sw08p/codec"
	"dhs/internal/transport"
)

// portOf parses the port from a listener's address string.
func portOf(t *testing.T, ln net.Listener) int {
	t.Helper()
	_, portStr, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("SplitHostPort: %v", err)
	}
	var port int
	for _, c := range portStr {
		port = port*10 + int(c-'0')
	}
	return port
}

// ---- GetDeviceInfo / GetSlotInfo / ExposeClient / Unsubscribe -------------

func TestGetDeviceInfoNotConnected(t *testing.T) {
	_, err := freshPlugin().GetDeviceInfo(context.Background())
	if !errors.Is(err, consumer.ErrNotConnected) {
		t.Fatalf("err = %v; want ErrNotConnected", err)
	}
}

func TestGetDeviceInfoConnected(t *testing.T) {
	p, _, cleanup := newPeerHarness(t, ackOnly)
	defer cleanup()
	info, err := p.GetDeviceInfo(context.Background())
	if err != nil {
		t.Fatalf("GetDeviceInfo: %v", err)
	}
	if info.IP != "test" || info.Port != codec.DefaultPort {
		t.Errorf("info = %+v; want IP=test port=%d", info, codec.DefaultPort)
	}
}

func TestGetSlotInfoNotImplemented(t *testing.T) {
	_, err := freshPlugin().GetSlotInfo(context.Background(), 0)
	if !errors.Is(err, consumer.ErrNotImplemented) {
		t.Fatalf("err = %v; want ErrNotImplemented", err)
	}
}

func TestExposeClient(t *testing.T) {
	p, _, cleanup := newPeerHarness(t, ackOnly)
	defer cleanup()
	cli, err := p.ExposeClient()
	if err != nil {
		t.Fatalf("ExposeClient: %v", err)
	}
	if cli == nil {
		t.Fatal("ExposeClient returned nil client")
	}
}

func TestExposeClientNotConnected(t *testing.T) {
	_, err := freshPlugin().ExposeClient()
	if !errors.Is(err, consumer.ErrNotConnected) {
		t.Fatalf("err = %v; want ErrNotConnected", err)
	}
}

func TestUnsubscribeNotImplemented(t *testing.T) {
	if err := freshPlugin().Unsubscribe(consumer.ValueRequest{}); !errors.Is(err, consumer.ErrNotImplemented) {
		t.Fatalf("err = %v; want ErrNotImplemented", err)
	}
}

// ---- Connect: idempotency, conflict, and recorder wiring ------------------

func TestConnectIdempotentAndConflict(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		for {
			c, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			go func(c net.Conn) { _, _ = io.Copy(io.Discard, c) }(c)
		}
	}()
	port := portOf(t, ln)

	p := &Plugin{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := p.Connect(ctx, "127.0.0.1", port); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = p.Disconnect() }()

	// Same endpoint, port 0 → resolves to existing connection, idempotent nil.
	if err := p.Connect(ctx, "127.0.0.1", 0); err != nil {
		t.Errorf("idempotent Connect(port 0) = %v; want nil", err)
	}
	// Same endpoint, same port → idempotent nil.
	if err := p.Connect(ctx, "127.0.0.1", port); err != nil {
		t.Errorf("idempotent Connect(same port) = %v; want nil", err)
	}
	// Different host → conflict error.
	if err := p.Connect(ctx, "127.0.0.2", port); err == nil {
		t.Error("Connect to different host returned nil; want already-connected error")
	}
}

func TestConnectDialError(t *testing.T) {
	// Port 1 on loopback is almost certainly refused → TransportError.
	p := &Plugin{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := p.Connect(ctx, "127.0.0.1", 1)
	if err == nil {
		t.Fatal("Connect to refused port returned nil; want TransportError")
	}
	var te *consumer.TransportError
	if !errors.As(err, &te) {
		t.Fatalf("err = %T; want *consumer.TransportError", err)
	}
	if te.Op != "connect" {
		t.Errorf("TransportError.Op = %q; want connect", te.Op)
	}
}

func TestConnectWithRecorder(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	srvDone := make(chan struct{})
	go func() {
		defer close(srvDone)
		c, aerr := ln.Accept()
		if aerr != nil {
			return
		}
		defer func() { _ = c.Close() }()
		// Read the maintenance frame and ACK it so the recorder logs rx too.
		buf := make([]byte, 256)
		if _, rerr := c.Read(buf); rerr == nil {
			_, _ = c.Write(codec.PackACK())
		}
		time.Sleep(50 * time.Millisecond)
	}()
	port := portOf(t, ln)

	recPath := filepath.Join(t.TempDir(), "cap.jsonl")
	rec, err := transport.NewRecorder(recPath)
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}
	defer func() { _ = rec.Close() }()

	p := &Plugin{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	p.SetRecorder(rec)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := p.Connect(ctx, "127.0.0.1", port); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = p.Disconnect() }()

	// Drive one frame so the recorder OnTx/OnRx wrappers run.
	_ = p.Maintenance(ctx, codec.MaintSoftReset, 0, 0)
	<-srvDone
}

// ---- Canonicalize ---------------------------------------------------------

func TestCanonicalizeEmpty(t *testing.T) {
	p := freshPlugin()
	exp, err := p.Canonicalize(context.Background())
	if err != nil {
		t.Fatalf("Canonicalize: %v", err)
	}
	root, ok := exp.Root.(*canonical.Node)
	if !ok {
		t.Fatalf("root type = %T; want *canonical.Node", exp.Root)
	}
	if root.Identifier != "device" {
		t.Errorf("root identifier = %q; want device", root.Identifier)
	}
	if len(root.Children) != 0 {
		t.Errorf("empty config: children = %d; want 0", len(root.Children))
	}
}

func TestCanonicalizeWithMatrixConfig(t *testing.T) {
	p := freshPlugin()
	p.host = "10.0.0.1"
	p.SetMatrixConfig(MatrixConfig{MatrixID: 2, Level: 3, Dsts: 16, Srcs: 8})
	exp, err := p.Canonicalize(context.Background())
	if err != nil {
		t.Fatalf("Canonicalize: %v", err)
	}
	root, ok := exp.Root.(*canonical.Node)
	if !ok {
		t.Fatalf("root type = %T; want *canonical.Node", exp.Root)
	}
	if root.Identifier != "10.0.0.1" {
		t.Errorf("root identifier = %q; want host", root.Identifier)
	}
	if len(root.Children) != 1 {
		t.Fatalf("children = %d; want 1 matrix", len(root.Children))
	}
}

func TestCanonicalizeCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := freshPlugin().Canonicalize(ctx); err == nil {
		t.Fatal("Canonicalize with canceled ctx returned nil; want error")
	}
}
