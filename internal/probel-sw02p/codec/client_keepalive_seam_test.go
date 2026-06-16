package codec

import (
	"errors"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"
)

// TestApplyTCPKeepalivePeriodError covers the SetKeepAlivePeriod failure arm of
// applyTCPKeepalive. On a live *net.TCPConn that branch can't be provoked in
// isolation (any fd state failing SetKeepAlivePeriod also fails SetKeepAlive,
// which returns first), so we drive it through the package test seams: make the
// first call succeed and the second fail.
func TestApplyTCPKeepalivePeriodError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if _, ok := conn.(*net.TCPConn); !ok {
		t.Fatalf("expected *net.TCPConn, got %T", conn)
	}

	origAlive, origPeriod := tcpSetKeepAlive, tcpSetKeepAlivePeriod
	defer func() { tcpSetKeepAlive, tcpSetKeepAlivePeriod = origAlive, origPeriod }()

	calledAlive := false
	tcpSetKeepAlive = func(*net.TCPConn, bool) error { calledAlive = true; return nil }
	tcpSetKeepAlivePeriod = func(*net.TCPConn, time.Duration) error { return errors.New("boom") }

	// Must not panic; exercises the SetKeepAlivePeriod-failed Debug branch.
	applyTCPKeepalive(conn, time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if !calledAlive {
		t.Fatal("SetKeepAlive seam was not invoked")
	}
}
