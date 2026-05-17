// Code sentinels for the S101 framing layer.
//
// Wraps the per-call-site fmt.Errorf strings with typed *errcode.Code
// instances so callers (CLI, scripts, Ansible) can dispatch via
// errors.Is(err, s101.ErrXxx) instead of grepping free-text.
// Stable wire form on stderr: "s101:<code>: <human message>".
//
// R1c migration — replaces the prior package-local errors.New sentinels
// (ErrBadFrame / ErrBadCRC / ErrTruncated) with typed errcode codes so
// the CLI exit dispatcher (R1h) can recognise them.
package s101

import "dhs/internal/errcode"

// All s101:* codes are runtime / wire-decode failures (Class 1, exit 1).
var (
	// ErrBadFrame is returned when a frame is missing BOF or EOF.
	ErrBadFrame = errcode.New(errcode.LayerS101, "bad-frame", errcode.ClassRuntime)

	// ErrBadCRC is returned when the CRC-16/CCITT verify fails on the
	// unescaped inner bytes. Per Ember+ Doc §S101 Framing p.94.
	ErrBadCRC = errcode.New(errcode.LayerS101, "crc-mismatch", errcode.ClassRuntime)

	// ErrTruncated is returned when a frame ends mid-header or
	// mid-content (insufficient bytes for the declared shape).
	ErrTruncated = errcode.New(errcode.LayerS101, "truncated", errcode.ClassRuntime)

	// ErrFrameTooLarge is returned when the running frame buffer grows
	// past the 64 KiB safety cap before an EOF is seen. Defends
	// against an unbounded stream of pre-frame garbage on a misbehaving
	// peer.
	ErrFrameTooLarge = errcode.New(errcode.LayerS101, "frame-too-large", errcode.ClassRuntime)

	// ErrReadFailed wraps an io.Reader error during frame scan
	// (BOF search or content collection). Distinct from EOF — EOF is
	// returned as-is for stream-close semantics.
	ErrReadFailed = errcode.New(errcode.LayerS101, "read-failed", errcode.ClassRuntime)

	// ErrWriteFailed wraps an io.Writer error during WriteFrame.
	ErrWriteFailed = errcode.New(errcode.LayerS101, "write-failed", errcode.ClassRuntime)
)
