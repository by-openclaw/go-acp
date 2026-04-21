package probel

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"acp/internal/probel/codec"
	"acp/internal/export/canonical"
)

// TestKeepaliveSchedulerSendsPings: with a short interval, the server
// emits tx 0x11 frames to a connected peer. Uses a raw net.Conn as the
// "controller" so we can observe wire bytes directly without dragging
// in the consumer plugin (which would auto-respond and hide the ping).
func TestKeepaliveSchedulerSendsPings(t *testing.T) {
	exp := &canonical.Export{
		Root: &canonical.Node{
			Header: canonical.Header{
				Number: 1, Identifier: "router", OID: "1",
				Children: []canonical.Element{
					&canonical.Matrix{
						Header: canonical.Header{Number: 1, Identifier: "matrix-0", OID: "1.1"},
						Type:   canonical.MatrixOneToN, Mode: canonical.ModeLinear,
						TargetCount: 4, SourceCount: 4,
						Labels: []canonical.MatrixLabel{{BasePath: "router.matrix-0.video"}},
					},
				},
			},
		},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := newServer(logger, exp)
	srv.SetKeepaliveInterval(80 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ctx, "127.0.0.1:0") }()

	deadline := time.Now().Add(2 * time.Second)
	var addr string
	for time.Now().Before(deadline) {
		srv.mu.Lock()
		if srv.listener != nil {
			addr = srv.listener.Addr().String()
			srv.mu.Unlock()
			break
		}
		srv.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	if addr == "" {
		t.Fatal("server failed to bind")
	}

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetReadDeadline(time.Now().Add(1 * time.Second))

	// Read bytes and count how many tx 0x11 frames appear.
	var pings atomic.Int32
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		buf := make([]byte, 256)
		acc := make([]byte, 0, 256)
		for {
			n, err := conn.Read(buf)
			if err != nil {
				return
			}
			acc = append(acc, buf[:n]...)
			for len(acc) > 0 {
				if codec.IsACK(acc) || codec.IsNAK(acc) {
					acc = acc[2:]
					continue
				}
				if acc[0] != codec.DLE {
					acc = acc[1:]
					continue
				}
				f, consumed, perr := codec.Unpack(acc)
				if errors.Is(perr, io.ErrUnexpectedEOF) {
					break
				}
				if perr != nil {
					acc = acc[2:]
					continue
				}
				if f.ID == codec.TxAppKeepaliveRequest {
					pings.Add(1)
				}
				// ACK each valid frame per spec §2 so the session is healthy.
				_, _ = conn.Write(codec.PackACK())
				acc = acc[consumed:]
			}
		}
	}()

	// Wait up to 500 ms for at least 3 pings (80 ms interval).
	pingDeadline := time.After(1 * time.Second)
	for pings.Load() < 3 {
		select {
		case <-pingDeadline:
			t.Fatalf("only observed %d pings; want >= 3", pings.Load())
		case <-time.After(20 * time.Millisecond):
		}
	}

	_ = conn.Close()
	<-readDone
	cancel()
	select {
	case <-serveErr:
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return after ctx cancel")
	}
}

// TestKeepaliveDisabledByDefault: without SetKeepaliveInterval, no
// keepalive frames are emitted.
func TestKeepaliveDisabledByDefault(t *testing.T) {
	exp := &canonical.Export{Root: &canonical.Node{Header: canonical.Header{Number: 1, Identifier: "router", OID: "1"}}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := newServer(logger, exp)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = srv.Serve(ctx, "127.0.0.1:0") }()

	deadline := time.Now().Add(1 * time.Second)
	var addr string
	for time.Now().Before(deadline) {
		srv.mu.Lock()
		if srv.listener != nil {
			addr = srv.listener.Addr().String()
			srv.mu.Unlock()
			break
		}
		srv.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	if addr == "" {
		t.Fatal("server failed to bind")
	}

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	if n > 0 {
		t.Errorf("expected no bytes; got %d: %s", n, codec.HexDump(buf[:n]))
	}
	if err == nil {
		t.Error("expected timeout error; got nil")
	}
	cancel()
}
