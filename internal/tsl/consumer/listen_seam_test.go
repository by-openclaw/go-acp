package tsl

import (
	"context"
	"errors"
	"net"
	"syscall"
	"testing"
)

// TestListen_RawControlError drives the unreachable c.Control error arm in
// udpSession.listen via the rawControl seam. With the seam forced to return
// an error, net.ListenConfig.Control returns it and ListenPacket fails, so
// listen returns the wrapped "tsl: listen" error.
func TestListen_RawControlError(t *testing.T) {
	forced := errors.New("forced rawconn control failure")
	orig := rawControl
	rawControl = func(c syscall.RawConn, f func(fd uintptr)) error { return forced }
	defer func() { rawControl = orig }()

	s := newUDPSession()
	err := s.listen(context.Background(), "127.0.0.1:0", decodeV31Payload)
	if err == nil {
		t.Fatal("expected listen to fail when rawControl returns an error")
	}
	if s.conn != nil {
		t.Error("conn should be nil after failed listen")
	}
}

// TestListen_RawControlPassthrough pins the production identity behaviour
// of the rawControl seam: it forwards to c.Control and a normal bind
// succeeds.
func TestListen_RawControlPassthrough(t *testing.T) {
	if rawControl == nil {
		t.Fatal("rawControl must be non-nil (production default)")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := newUDPSession()
	if err := s.listen(ctx, "127.0.0.1:0", decodeV31Payload); err != nil {
		t.Fatalf("listen with default rawControl: %v", err)
	}
	defer func() { _ = s.close() }()
	if s.boundAddr() == nil {
		t.Error("boundAddr nil after successful listen")
	}
}

// TestListen_ConnAssertNotUDP drives the unreachable "unexpected conn type"
// arm via the listenConnAssert seam. The real ListenPacket still binds a
// *net.UDPConn, but the seam reports ok=false, so listen must close the
// PacketConn and return the type-mismatch error.
func TestListen_ConnAssertNotUDP(t *testing.T) {
	orig := listenConnAssert
	listenConnAssert = func(pc net.PacketConn) (*net.UDPConn, bool) {
		_ = pc // real conn; force the !ok path
		return nil, false
	}
	defer func() { listenConnAssert = orig }()

	s := newUDPSession()
	err := s.listen(context.Background(), "127.0.0.1:0", decodeV31Payload)
	if err == nil {
		t.Fatal("expected listen to fail when conn assert reports !ok")
	}
	if s.conn != nil {
		t.Error("conn should remain nil on the !ok path")
	}
}

// TestListen_ConnAssertPassthrough pins the production identity of the
// listenConnAssert seam: a real ListenPacket result asserts to *net.UDPConn
// with ok=true.
func TestListen_ConnAssertPassthrough(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenPacket: %v", err)
	}
	defer func() { _ = pc.Close() }()
	conn, ok := listenConnAssert(pc)
	if !ok || conn == nil {
		t.Errorf("listenConnAssert(real udp) = (%v, %v); want non-nil, true", conn, ok)
	}
}
