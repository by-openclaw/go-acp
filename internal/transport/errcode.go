// Code sentinels for the transport layer.
//
// Wraps the per-call-site fmt.Errorf strings with typed *errcode.Code
// instances so callers (CLI, scripts, Ansible) can dispatch via
// errors.Is(err, transport.ErrXxx) instead of grepping free-text.
// Stable wire form on stderr: "<layer>:<code>: <human message>".
//
// Per memory feedback_error_contract_cross_os the contract is
// Unix-standard 0/1/2 exit codes; transport:* maps to exit 1, except
// validation:* (caller-input failures) which maps to exit 2.
package transport

import (
	"errors"
	"net"
	"os"
	"syscall"

	"dhs/internal/errcode"
)

// classifyDialError returns the most specific transport sentinel for a
// dial-time error, falling back to ErrDialFailed for unrecognised failure
// modes. Cross-OS — Go maps Windows WSAECONNREFUSED / WSAETIMEDOUT through
// the same syscall constants on both platforms.
func classifyDialError(err error) *errcode.Code {
	if err == nil {
		return nil
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return ErrRefused
	}
	if errors.Is(err, os.ErrDeadlineExceeded) {
		return ErrTimeout
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return ErrTimeout
	}
	return ErrDialFailed
}

// Wire/network failures — exit class 1 (runtime).
var (
	// ErrDialFailed wraps a generic dial failure (OS-level). Use
	// ErrRefused or ErrTimeout when the failure mode is detectable
	// cross-OS.
	ErrDialFailed = errcode.New(errcode.LayerTransport, "dial-failed", errcode.ClassRuntime)

	// ErrRefused covers ECONNREFUSED (TCP RST on connect) cross-OS.
	// Detected via errors.Is(rawErr, syscall.ECONNREFUSED) — Go maps
	// the Windows WSAECONNREFUSED through the same syscall constant.
	ErrRefused = errcode.New(errcode.LayerTransport, "refused", errcode.ClassRuntime)

	// ErrTimeout covers deadline-exceeded on dial/read/write.
	// Detected via errors.Is(rawErr, os.ErrDeadlineExceeded) or
	// (net.Error).Timeout().
	ErrTimeout = errcode.New(errcode.LayerTransport, "timeout", errcode.ClassRuntime)

	// ErrListenFailed wraps a bind/listen failure.
	ErrListenFailed = errcode.New(errcode.LayerTransport, "listen-failed", errcode.ClassRuntime)

	// ErrNilConn is returned when a method is called on a nil
	// connection — typically a programming error.
	ErrNilConn = errcode.New(errcode.LayerTransport, "nil-conn", errcode.ClassRuntime)

	// ErrPayloadTooLarge is returned when a send violates the
	// transport's per-payload max (caller-supplied limit).
	ErrPayloadTooLarge = errcode.New(errcode.LayerTransport, "payload-too-large", errcode.ClassRuntime)

	// ErrSetDeadlineFailed wraps a SetReadDeadline / SetWriteDeadline
	// system-call failure.
	ErrSetDeadlineFailed = errcode.New(errcode.LayerTransport, "set-deadline-failed", errcode.ClassRuntime)

	// ErrWriteFailed wraps a TCP/UDP write that returned an OS-level
	// error mid-send.
	ErrWriteFailed = errcode.New(errcode.LayerTransport, "write-failed", errcode.ClassRuntime)

	// ErrShortWrite is returned when UDP reports the kernel wrote
	// fewer bytes than the payload — datagram was truncated.
	ErrShortWrite = errcode.New(errcode.LayerTransport, "short-write", errcode.ClassRuntime)

	// ErrReadFailed wraps a TCP/UDP read that returned an OS-level
	// error mid-receive.
	ErrReadFailed = errcode.New(errcode.LayerTransport, "read-failed", errcode.ClassRuntime)

	// ErrOversizedDatagram is returned when a received UDP datagram
	// exceeds the caller-supplied maxSize.
	ErrOversizedDatagram = errcode.New(errcode.LayerTransport, "oversized-datagram", errcode.ClassRuntime)

	// ErrMLENOutOfRange is returned by the TCP framer when the
	// length-prefix is below the minimum or above the configured max.
	ErrMLENOutOfRange = errcode.New(errcode.LayerTransport, "mlen-out-of-range", errcode.ClassRuntime)

	// ErrWrongConnType signals net.Dial returned a connection of an
	// unexpected concrete type (should never happen in practice).
	ErrWrongConnType = errcode.New(errcode.LayerTransport, "wrong-conn-type", errcode.ClassRuntime)

	// ErrCloseFailed wraps a connection-close OS-level failure.
	ErrCloseFailed = errcode.New(errcode.LayerTransport, "close-failed", errcode.ClassRuntime)

	// ErrCaptureCreateFailed wraps a failure to create the capture
	// file for --capture <path>.
	ErrCaptureCreateFailed = errcode.New(errcode.LayerTransport, "capture-create-failed", errcode.ClassRuntime)
)

// Caller-input validation — exit class 2 (usage). Lives in the transport
// package because they describe transport-specific inputs (host, port,
// payload, maxSize), but they carry the validation: prefix since the
// contract is "validation:* → exit 2" cross-layer.
var (
	// ErrInvalidHost is returned when the host string is empty.
	ErrInvalidHost = errcode.New(errcode.LayerValidation, "invalid-host", errcode.ClassUsage)

	// ErrInvalidPort is returned when the port is outside [1, 65535].
	ErrInvalidPort = errcode.New(errcode.LayerValidation, "invalid-port", errcode.ClassUsage)

	// ErrEmptyPayload is returned when Send is called with a
	// zero-length payload.
	ErrEmptyPayload = errcode.New(errcode.LayerValidation, "empty-payload", errcode.ClassUsage)

	// ErrInvalidMaxSize is returned when Receive is called with a
	// non-positive maxSize / maxPayload.
	ErrInvalidMaxSize = errcode.New(errcode.LayerValidation, "invalid-max-size", errcode.ClassUsage)
)
