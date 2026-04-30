//go:build windows

package dnssd

import (
	"net"
	"syscall"
)

// IP_MULTICAST_LOOP option name on Winsock — not exposed by stdlib
// syscall on Windows, so define it locally. Value matches the Win32
// header `<ws2ipdef.h>`.
const winIPMulticastLoop = 11

// setMulticastLoopback re-enables IP_MULTICAST_LOOP on a socket created
// by net.ListenMulticastUDP, which Go's stdlib disables by default.
// Without this, two processes on the same Windows host bound to the
// same multicast group don't see each other's packets — breaking
// same-host AMWA NMOS discovery (a Node and a Controller running on
// one machine). RFC 6762 §11 expects link-local loopback to work.
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
		sockErr = syscall.SetsockoptInt(syscall.Handle(fd), syscall.IPPROTO_IP, winIPMulticastLoop, v)
	})
	if cerr != nil {
		return cerr
	}
	return sockErr
}
