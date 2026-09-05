//go:build windows

package transport

import (
	"errors"
	"syscall"
)

// isMessageTooLong reports whether a UDP read failed because the datagram
// was larger than the buffer.
//
// The platforms disagree, and the difference is invisible until you run on
// both. A Unix recv TRUNCATES an oversized datagram and reports the short
// length, so transport detects it by reading into maxSize+1 bytes and
// noticing n > maxSize. Windows does not truncate: wsarecv fails outright
// with WSAEMSGSIZE. Without this, the same malformed packet surfaces as
// ErrOversizedDatagram on rocky9 and ErrReadFailed on winsrv — one contract
// with two meanings, which is worse than either.
func isMessageTooLong(err error) bool {
	return errors.Is(err, wsaEMsgSize)
}

// wsaEMsgSize is Winsock's WSAEMSGSIZE, "a message sent on a datagram socket
// was larger than the internal message buffer". Go's syscall package does
// not name it on Windows, so the value is spelled out here — one documented
// constant beats a dependency for one constant.
const wsaEMsgSize = syscall.Errno(10040)

// setReuseAddr enables SO_REUSEADDR on a bound UDP socket so multiple
// processes can listen on the same port and all receive broadcast
// traffic. Windows semantics: SO_REUSEADDR alone is sufficient for
// multi-receiver broadcast UDP; no SO_REUSEPORT equivalent is needed.
func setReuseAddr(fd uintptr) error {
	return syscall.SetsockoptInt(syscall.Handle(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
}

// SetSocketReuseAddr is the exported wrapper for cross-package use (e.g.
// TSL consumer session). Behaviour matches setReuseAddr.
func SetSocketReuseAddr(fd uintptr) error {
	return setReuseAddr(fd)
}

// SetSocketBroadcast enables SO_BROADCAST on a UDP socket, allowing
// sends to the limited broadcast address 255.255.255.255. Exported so
// the acp1 discover code can pass it into net.Dialer.Control.
func SetSocketBroadcast(fd uintptr) error {
	return syscall.SetsockoptInt(syscall.Handle(fd), syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1)
}
