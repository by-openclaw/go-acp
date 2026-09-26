//go:build windows

package provider

import "syscall"

// addrInUse is the kernel refusing a bind because something already holds
// the port.
//
// Windows answers with the Winsock errno rather than the POSIX one --
// syscall.EADDRINUSE exists on Windows but never matches, which is a
// silent way for a retry to stop retrying -- and the syscall package does
// not name WSAEADDRINUSE, so the number is written out. It is 10048, and
// has been since Winsock 2.
const addrInUse = syscall.Errno(10048)
