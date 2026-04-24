//go:build !windows

package transport

import "syscall"

// setReuseAddr enables SO_REUSEADDR on a bound UDP socket. On Linux /
// macOS this alone does NOT allow multiple receivers to share a port
// for incoming broadcasts — SO_REUSEPORT is required for that. We set
// both when the kernel supports it; platforms without SO_REUSEPORT
// will return a best-effort error that we ignore.
func setReuseAddr(fd uintptr) error {
	if err := syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1); err != nil {
		return err
	}
	// Best-effort SO_REUSEPORT. Constant is platform-specific, so we
	// try the commonly used value 15 on Linux and just ignore errors on
	// platforms where it isn't defined. When the build can reach
	// golang.org/x/sys this should be swapped for unix.SO_REUSEPORT.
	const soReusePort = 15
	_ = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, soReusePort, 1)
	return nil
}

// SetSocketReuseAddr is the exported wrapper for cross-package use (e.g.
// TSL consumer session). Behaviour matches setReuseAddr.
func SetSocketReuseAddr(fd uintptr) error {
	return setReuseAddr(fd)
}

// SetSocketBroadcast enables SO_BROADCAST on a UDP socket so sends to
// the limited broadcast address 255.255.255.255 are accepted by the
// kernel. Used by acp1 discover's active probe path.
func SetSocketBroadcast(fd uintptr) error {
	return syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1)
}
