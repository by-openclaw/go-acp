//go:build windows

package transport

import "syscall"

// setReuseAddr enables SO_REUSEADDR on a bound UDP socket so multiple
// processes can listen on the same port and all receive broadcast
// traffic. Windows semantics: SO_REUSEADDR alone is sufficient for
// multi-receiver broadcast UDP; no SO_REUSEPORT equivalent is needed.
func setReuseAddr(fd uintptr) error {
	return syscall.SetsockoptInt(syscall.Handle(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
}

// SetSocketBroadcast enables SO_BROADCAST on a UDP socket, allowing
// sends to the limited broadcast address 255.255.255.255. Exported so
// the acp1 discover code can pass it into net.Dialer.Control.
func SetSocketBroadcast(fd uintptr) error {
	return syscall.SetsockoptInt(syscall.Handle(fd), syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1)
}

// SetSocketReuseAddr is the exported wrapper for cross-package use.
// Behaviour matches the unexported setReuseAddr.
func SetSocketReuseAddr(fd uintptr) error {
	return setReuseAddr(fd)
}
