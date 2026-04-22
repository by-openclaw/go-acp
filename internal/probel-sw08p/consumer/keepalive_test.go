package probelsw08p

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"acp/internal/probel-sw08p/codec"
)

// TestKeepaliveAutoResponder: when the peer sends TxAppKeepaliveRequest
// (0x11), the consumer plugin answers with RxAppKeepaliveResponse (0x22)
// via the installed async listener.
func TestKeepaliveAutoResponder(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	addr := ln.Addr().String()
	_, portStr, _ := net.SplitHostPort(addr)
	var port int
	for _, c := range portStr {
		port = port*10 + int(c-'0')
	}

	var responses atomic.Int32
	peerReady := make(chan struct{})
	peerDone := make(chan struct{})
	go func() {
		defer close(peerDone)
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = c.Close() }()
		close(peerReady)

		// Send keepalive ping.
		_, _ = c.Write(codec.Pack(codec.EncodeKeepaliveRequest()))

		// Read until we see a rx 0x22 or timeout.
		_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
		buf := make([]byte, 256)
		acc := make([]byte, 0, 256)
		for {
			n, err := c.Read(buf)
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
				if f.ID == codec.RxAppKeepaliveResponse {
					responses.Add(1)
					// ACK the response per spec §2.
					_, _ = c.Write(codec.PackACK())
					return
				}
				_, _ = c.Write(codec.PackACK())
				acc = acc[consumed:]
			}
		}
	}()

	p := &Plugin{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := p.Connect(ctx, "127.0.0.1", port); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = p.Disconnect() }()

	<-peerReady
	// Wait for the responder to see the ping and reply.
	select {
	case <-peerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("peer never saw keepalive response")
	}

	if responses.Load() != 1 {
		t.Errorf("got %d keepalive responses; want 1", responses.Load())
	}
}

// TestIsOnline: after we receive any rx frame, IsOnlineWithin returns
// true inside a fresh window and false past the staleness threshold.
// Pre-connect and unseeded plugins must report offline.
func TestIsOnline(t *testing.T) {
	// Unconnected plugin: always offline.
	fresh := &Plugin{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if fresh.IsOnline() {
		t.Fatal("unconnected plugin reports online")
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	var port int
	for _, c := range portStr {
		port = port*10 + int(c-'0')
	}

	// Peer that ACKs any inbound, then sends one keepalive ping so the
	// consumer logs a rx timestamp.
	go func() {
		c, aerr := ln.Accept()
		if aerr != nil {
			return
		}
		defer func() { _ = c.Close() }()
		_, _ = c.Write(codec.Pack(codec.EncodeKeepaliveRequest()))
		// Hold the socket open for the test duration.
		time.Sleep(3 * time.Second)
	}()

	p := &Plugin{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := p.Connect(ctx, "127.0.0.1", port); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = p.Disconnect() }()

	// Wait for the rx ping to be observed.
	deadline := time.Now().Add(2 * time.Second)
	for !p.IsOnlineWithin(1 * time.Second) {
		if time.Now().After(deadline) {
			t.Fatal("IsOnline never flipped true after rx")
		}
		time.Sleep(20 * time.Millisecond)
	}

	// A very short staleness window makes us offline again.
	time.Sleep(40 * time.Millisecond)
	if p.IsOnlineWithin(10 * time.Millisecond) {
		t.Error("IsOnlineWithin(10ms) should be false after 40ms of silence")
	}
}
