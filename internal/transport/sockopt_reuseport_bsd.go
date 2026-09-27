//go:build darwin || dragonfly || freebsd || netbsd || openbsd

package transport

import "syscall"

// soReusePort is SO_REUSEPORT on macOS and the BSDs (0x200). Using Linux's
// 15 here set a different option and silently left the port unshared, so a
// second dhs instance on the same UDP port failed with "address already in
// use" on macOS.
const soReusePort = syscall.SO_REUSEPORT
