//go:build windows

package transport

// errMessageTooLong is the error THIS OS reports for an oversized datagram
// read, so a common test can drive the isMessageTooLong arm on every host.
// Winsock fails the read with WSAEMSGSIZE; see sockopt_windows.go.
var errMessageTooLong error = wsaEMsgSize
