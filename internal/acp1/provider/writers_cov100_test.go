package acp1

import (
	"context"
	"net"
	"testing"
	"time"
)

// TestServeTCP_GracefulSessionClose: a client that does one exchange then
// closes lets the server session's reader break and wait on writerDone.
func TestServeTCP_GracefulSessionClose(t *testing.T) {
	s := newTestServer(t)
	addr, _ := startTCPServer(t, s)
	conn, err := net.DialTimeout("tcp4", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	body := []byte{0x00, 0x00, 0x00, 0x01, 0x01, 0x01, 0x01, 0x00, 0x01, 0x00}
	var lb [4]byte
	lb[3] = byte(len(body))
	_, _ = conn.Write(lb[:])
	_, _ = conn.Write(body)
	// Read the reply so the session is fully established, then close.
	rb := make([]byte, 256)
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	_, _ = conn.Read(rb)
	_ = conn.Close()
	time.Sleep(100 * time.Millisecond) // let the reader break + wait writerDone
}

// tcpConnPair returns a connected pair of *net.TCPConn (client, server-side).
func tcpConnPair(t *testing.T) (*net.TCPConn, *net.TCPConn) {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	type res struct {
		c   net.Conn
		err error
	}
	ch := make(chan res, 1)
	go func() {
		c, err := ln.Accept()
		ch <- res{c, err}
	}()
	client, err := net.DialTCP("tcp4", nil, ln.Addr().(*net.TCPAddr))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	r := <-ch
	if r.err != nil {
		t.Fatalf("accept: %v", r.err)
	}
	return client, r.c.(*net.TCPConn)
}

// TestRunTCPWriter_Paths covers the ctx-exit, channel-close, and write-error
// arms of the extracted TCP writer.
func TestRunTCPWriter_Paths(t *testing.T) {
	s := newTestServer(t)

	// ctx-exit.
	c1, s1 := tcpConnPair(t)
	defer func() { _ = c1.Close(); _ = s1.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.runTCPWriter(ctx, c1, make(chan []byte))

	// channel-close exit.
	c2, s2 := tcpConnPair(t)
	defer func() { _ = c2.Close(); _ = s2.Close() }()
	closed := make(chan []byte)
	close(closed)
	s.runTCPWriter(context.Background(), c2, closed)

	// write-error: close the LOCAL conn so writeMLENFrame fails on the first
	// push (ErrClosed → the isClosed branch).
	c3, s3 := tcpConnPair(t)
	_ = c3.Close()
	_ = s3.Close()
	send := make(chan []byte, 1)
	send <- []byte{0, 0, 0, 1, 1, 2, 0, 0xAB}
	done := make(chan struct{})
	go func() { s.runTCPWriter(context.Background(), c3, send); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runTCPWriter did not exit on write error")
	}

	// non-closed write error: close only the PEER (RST), keep the local
	// conn open, and push payloads until a write fails with a reset (not
	// ErrClosed) → the warn branch.
	c4, s4 := tcpConnPair(t)
	_ = s4.SetLinger(0) // RST on peer close
	_ = s4.Close()
	defer func() { _ = c4.Close() }()
	send4 := make(chan []byte, 8)
	for i := 0; i < 8; i++ {
		send4 <- []byte{0, 0, 0, 1, 1, 2, 0, byte(i)}
	}
	close(send4)
	s.runTCPWriter(context.Background(), c4, send4)
}

// TestRunAN2Writer_Paths mirrors the writer coverage for the AN2 writer.
func TestRunAN2Writer_Paths(t *testing.T) {
	s := newTestServer(t)

	c1, s1 := tcpConnPair(t)
	defer func() { _ = c1.Close(); _ = s1.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.runAN2Writer(ctx, c1, make(chan []byte))

	c2, s2 := tcpConnPair(t)
	defer func() { _ = c2.Close(); _ = s2.Close() }()
	closed := make(chan []byte)
	close(closed)
	s.runAN2Writer(context.Background(), c2, closed)

	c3, s3 := tcpConnPair(t)
	_ = c3.Close()
	_ = s3.Close()
	send := make(chan []byte, 1)
	send <- []byte{0xC6, 0x35, 0x01, 0x00, 0x00, 0x04, 0x00, 0x00}
	done := make(chan struct{})
	go func() { s.runAN2Writer(context.Background(), c3, send); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runAN2Writer did not exit on write error")
	}

	// non-closed write error via peer RST.
	c4, s4 := tcpConnPair(t)
	_ = s4.SetLinger(0)
	_ = s4.Close()
	defer func() { _ = c4.Close() }()
	send4 := make(chan []byte, 8)
	for i := 0; i < 8; i++ {
		send4 <- []byte{0xC6, 0x35, 0x01, 0x00, 0x00, 0x04, 0x00, 0x00}
	}
	close(send4)
	s.runAN2Writer(context.Background(), c4, send4)
}
