//go:build linux || darwin || freebsd || netbsd || openbsd

package dnssd

// The twin of mdns_loop_windows_test.go. Go's stdlib turns
// IP_MULTICAST_LOOP OFF on a socket from ListenMulticastUDP, which means
// a Node and a Controller on one host never see each other's mDNS —
// so this shim is what makes same-host AMWA discovery work at all, and
// it is worth pinning on the platform it actually ships on.

import (
	"errors"
	"net"
	"syscall"
	"testing"
)

// TestSetMulticastLoopbackUnix exercises the setsockopt on a live socket
// both ways, then on one already closed — where rc.Control fails and the
// shim has to surface that rather than reporting success.
func TestSetMulticastLoopbackUnix(t *testing.T) {
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}
	if err := setMulticastLoopback(c, true); err != nil {
		t.Errorf("setMulticastLoopback(true): %v", err)
	}
	if err := setMulticastLoopback(c, false); err != nil {
		t.Errorf("setMulticastLoopback(false): %v", err)
	}
	_ = c.Close()

	if err := setMulticastLoopback(c, true); err == nil {
		t.Error("a closed socket must not report a successful setsockopt")
	}
}

// TestSetMulticastLoopbackUnix_SyscallConnError drives the one arm a live
// socket never reaches: SyscallConn itself failing, which happens only on
// a connection already torn down — and such a connection fails
// rc.Control first, so the arm is unreachable without the seam.
func TestSetMulticastLoopbackUnix_SyscallConnError(t *testing.T) {
	prev := syscallConn
	syscallConn = func(*net.UDPConn) (syscall.RawConn, error) {
		return nil, errors.New("syscallconn failed")
	}
	t.Cleanup(func() { syscallConn = prev })

	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	if err := setMulticastLoopback(c, true); err == nil {
		t.Error("setMulticastLoopback must surface the SyscallConn error")
	}
}
