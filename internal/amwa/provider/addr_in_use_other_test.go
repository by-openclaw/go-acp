//go:build !windows

package provider

import "syscall"

// addrInUse is the kernel refusing a bind because something already holds
// the port. Every Unix names it the same way.
const addrInUse = syscall.EADDRINUSE
