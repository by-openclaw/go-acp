//go:build windows

package dnssd

import (
	"errors"
	"net"
	"syscall"
	"testing"
)

// TestSetMulticastLoopback_SyscallConnError forces the SyscallConn error
// return through the syscallConn seam, covering the one arm a live socket
// never reaches (SyscallConn only fails on a socket already torn down,
// which also makes rc.Control fail first).
func TestSetMulticastLoopback_SyscallConnError(t *testing.T) {
	orig := syscallConn
	defer func() { syscallConn = orig }()
	syscallConn = func(c *net.UDPConn) (syscall.RawConn, error) {
		return nil, errors.New("syscallconn failed")
	}
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}
	defer func() { _ = c.Close() }()
	if err := setMulticastLoopback(c, true); err == nil {
		t.Error("setMulticastLoopback should surface the SyscallConn error")
	}
}

// TestSetMulticastLoopback_Windows exercises the Winsock IP_MULTICAST_LOOP
// setsockopt shim on a live socket for both on and off, and the error arm
// when the socket is already closed (SyscallConn fails). The happy calls
// guard the same-host loopback-delivery fix (a Node and a Controller on
// one Windows box must see each other's mDNS), which Go's stdlib disables
// by default on ListenMulticastUDP.
func TestSetMulticastLoopback_Windows(t *testing.T) {
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

	// Closed socket -> SyscallConn returns an error -> the shim returns it.
	if err := setMulticastLoopback(c, true); err == nil {
		t.Error("setMulticastLoopback on a closed socket should error")
	}
}
