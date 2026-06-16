package codec

import (
	"net"
	"testing"
)

// TestApplyTCPKeepaliveErrorPaths covers the two error-log branches of
// applyTCPKeepalive: SetKeepAlive and SetKeepAlivePeriod returning an
// error. Both fail when the underlying TCP socket is already closed —
// SetsockoptInt on a closed fd returns "use of closed network
// connection". We dial a real loopback listener (so the conn is a
// genuine *net.TCPConn, satisfying the type assertion), then Close it
// before invoking the helper so both setsockopt calls error out and the
// Debug-log arms run. The helper must not panic and must return
// normally.
func TestApplyTCPKeepaliveErrorPaths(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = l.Close() }()

	accepted := make(chan net.Conn, 1)
	go func() {
		c, _ := l.Accept()
		accepted <- c
	}()

	conn, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	srv := <-accepted
	if srv != nil {
		defer func() { _ = srv.Close() }()
	}

	tc, ok := conn.(*net.TCPConn)
	if !ok {
		t.Fatalf("dialed conn is %T; want *net.TCPConn", conn)
	}
	// Close the socket so SetKeepAlive / SetKeepAlivePeriod operate on a
	// closed fd and both return an error → exercises both Debug-log
	// branches.
	if err := tc.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// period > 0 so we skip the period<0 (disable) and period==0
	// (default) early arms and reach both setsockopt calls.
	applyTCPKeepalive(tc, 17, quietLogger())
}
