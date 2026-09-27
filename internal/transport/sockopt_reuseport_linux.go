//go:build linux

package transport

// soReusePort is SO_REUSEPORT on Linux (asm-generic/socket.h: 15). Go's
// syscall package does not name it for Linux, so the number is written out.
const soReusePort = 15
