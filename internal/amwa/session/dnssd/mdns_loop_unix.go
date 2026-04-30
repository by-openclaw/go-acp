//go:build linux || darwin || freebsd || netbsd || openbsd

package dnssd

import (
	"net"
	"syscall"
)

// setMulticastLoopback re-enables IP_MULTICAST_LOOP on a socket created
// by net.ListenMulticastUDP, which Go's stdlib disables by default. With
// loopback off, two processes on the same host bound to the same
// multicast group don't see each other's packets — breaking same-host
// AMWA NMOS discovery (a Node and a Controller running on one machine).
// RFC 6762 §11 expects link-local loopback to work.
func setMulticastLoopback(c *net.UDPConn, on bool) error {
	rc, err := c.SyscallConn()
	if err != nil {
		return err
	}
	var sockErr error
	cerr := rc.Control(func(fd uintptr) {
		v := 0
		if on {
			v = 1
		}
		sockErr = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IP, syscall.IP_MULTICAST_LOOP, v)
	})
	if cerr != nil {
		return cerr
	}
	return sockErr
}
