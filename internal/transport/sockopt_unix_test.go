//go:build !windows

package transport

import "syscall"

// errMessageTooLong is the error THIS OS reports for an oversized datagram
// read, so a common test can drive the isMessageTooLong arm on every host.
// A Unix recv truncates instead, so on this platform the arm is only ever
// reached through the udpRead / udpReadFromUDP seams; see sockopt_unix.go.
var errMessageTooLong error = syscall.EMSGSIZE
