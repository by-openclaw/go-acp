package session

// Coverage for the per-read deadline that detects a silent matrix. Without it
// the reader blocks forever on a half-open link (a NAT/firewall drop with no
// RST) and the session goes quiet without ever failing.

import (
	"dhs/internal/probel-sw02p/codec"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"
)

func TestClientSetIdleTimeout(t *testing.T) {
	c := &Client{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}

	if got := c.IdleTimeout(); got != 0 {
		t.Fatalf("IdleTimeout = %v on a fresh client, want 0", got)
	}
	// No connection yet: must record the value without panicking.
	c.SetIdleTimeout(45 * time.Second)
	if got := c.IdleTimeout(); got != 45*time.Second {
		t.Fatalf("IdleTimeout = %v, want 45s", got)
	}
	c.SetIdleTimeout(-1)
	if got := c.IdleTimeout(); got != 0 {
		t.Fatalf("IdleTimeout = %v for negative input, want 0 (disabled)", got)
	}
}

// With a live connection the value is pushed onto the socket immediately, so a
// reader already blocked picks up the new bound instead of waiting out the old.
func TestClientSetIdleTimeoutAppliesToConn(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		c, aerr := ln.Accept()
		if aerr == nil {
			t.Cleanup(func() { _ = c.Close() })
		}
	}()
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	c := &Client{logger: slog.New(slog.NewTextHandler(io.Discard, nil)), conn: conn}
	c.SetIdleTimeout(50 * time.Millisecond)
	c.SetIdleTimeout(0) // disable path clears the deadline
}

// The reader arms its deadline before every read, so a peer that accepts and
// then says nothing is detected instead of hanging the loop forever.
func TestReadLoopIdleDeadlineFires(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	accepted := make(chan net.Conn, 1)
	go func() {
		c, aerr := ln.Accept()
		if aerr != nil {
			return
		}
		accepted <- c // hold it open, stay silent
	}()
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	c := &Client{
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		conn:       conn,
		readerDone: make(chan struct{}),
	}
	c.SetIdleTimeout(100 * time.Millisecond)
	go c.readLoop(codec.DefaultReadBufferSize)

	select {
	case <-c.readerDone:
	case <-time.After(10 * time.Second):
		t.Fatal("readLoop never exited on a silent peer — the deadline did not apply")
	}
	select {
	case s := <-accepted:
		_ = s.Close()
	default:
	}
}
