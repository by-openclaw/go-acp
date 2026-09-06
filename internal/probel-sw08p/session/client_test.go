package session

import (
	"context"
	"dhs/internal/probel-sw08p/codec"
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"
)

func TestHexDump(t *testing.T) {
	cases := []struct {
		in   []byte
		want string
	}{
		{nil, ""},
		{[]byte{0x10}, "10"},
		{[]byte{0x10, 0x02, 0x01}, "10 02 01"},
		{[]byte{0xAB, 0xCD, 0xEF, 0x00, 0xFF}, "ab cd ef 00 ff"},
	}
	for _, tc := range cases {
		got := codec.HexDump(tc.in)
		if got != tc.want {
			t.Errorf("codec.HexDump(%v) = %q; want %q", tc.in, got, tc.want)
		}
	}
}

// TestClientLoopback wires both ends of a net.Pipe into a Client each
// and verifies:
//   - a framed Send round-trips through codec.Pack/codec.Unpack on both sides
//   - an unmatched frame is delivered to Subscribe listeners
//   - Close unblocks a pending Send with io.EOF
func TestClientLoopback(t *testing.T) {
	a, b := net.Pipe()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	disable := false
	cfg := ClientConfig{WireHexLog: &disable}
	clientA := NewClientFromConn(a, logger, cfg)
	clientB := NewClientFromConn(b, logger, cfg)
	defer func() { _ = clientA.Close() }()
	defer func() { _ = clientB.Close() }()

	// A subscribes to events so B's unsolicited frame is captured.
	var wg sync.WaitGroup
	wg.Add(1)
	clientA.Subscribe(func(f codec.Frame) {
		defer wg.Done()
		if f.ID != codec.TxCrosspointTally {
			t.Errorf("listener got cmd %02x; want %02x", f.ID, codec.TxCrosspointTally)
		}
		if len(f.Payload) != 3 {
			t.Errorf("listener got payload len %d; want 3", len(f.Payload))
		}
	})

	// B sends an unsolicited tally to A.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	tally := codec.Frame{ID: codec.TxCrosspointTally, Payload: []byte{0x00, 0x01, 0x05}}
	if _, err := clientB.Send(ctx, tally, nil); err != nil {
		t.Fatalf("B.Send: %v", err)
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("listener never fired")
	}
}

// TestClientCloseWakesPending verifies an in-flight Send is unblocked
// when the reader goroutine exits (EOF / peer close).
func TestClientCloseWakesPending(t *testing.T) {
	a, b := net.Pipe()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	disable := false
	client := NewClientFromConn(a, logger, ClientConfig{WireHexLog: &disable})

	// Drain and drop everything the client writes so Send's Write() never blocks.
	go func() {
		buf := make([]byte, 512)
		for {
			if _, err := b.Read(buf); err != nil {
				return
			}
		}
	}()

	done := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, err := client.Send(ctx, codec.Frame{ID: codec.RxMaintenance, Payload: []byte{0x00}},
			func(codec.Frame) bool { return true })
		done <- err
	}()

	// Close peer → Read on a returns io.EOF → reader exits →
	// failPending fires.
	time.Sleep(50 * time.Millisecond)
	_ = b.Close()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Send returned nil; want non-nil on peer close")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Send blocked past peer close")
	}
	_ = client.Close()
}

// TestClientOnEventFiresBeforeSubscribe proves that ClientConfig.OnEvent is
// wired in before the reader goroutine starts: a frame already on the wire
// when Dial returns is delivered to OnEvent without a separate Subscribe
// call. Pinned regression for #234 (keepalive Subscribe vs reader race).
func TestClientOnEventFiresBeforeSubscribe(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	// Server side: accept + immediately write a keepalive ping. The
	// ping is on the wire before the client's reader goroutine starts.
	go func() {
		c, aerr := ln.Accept()
		if aerr != nil {
			return
		}
		defer func() { _ = c.Close() }()
		_, _ = c.Write(codec.Pack(codec.EncodeKeepaliveRequest()))
		// Hold the socket open so the client's reader can drain it.
		time.Sleep(2 * time.Second)
	}()

	got := make(chan codec.Frame, 1)
	disable := false
	cfg := ClientConfig{
		WireHexLog: &disable,
		OnEvent: func(c *Client, f codec.Frame) {
			select {
			case got <- f:
			default:
			}
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cli, err := Dial(ctx, nil, ln.Addr().String(), logger, cfg)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = cli.Close() }()

	select {
	case f := <-got:
		if f.ID != codec.TxAppKeepaliveRequest {
			t.Fatalf("OnEvent got cmd %02x; want %02x", f.ID, codec.TxAppKeepaliveRequest)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("OnEvent never fired for pre-Dial wire frame")
	}
}
