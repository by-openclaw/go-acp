package ws

import (
	"crypto/rand"
	"sync/atomic"
)

// Test seams for the two genuinely-unreachable defensive branches in this
// package: the crypto/rand read inside newMaskKey / the handshake nonce,
// and the negative payload-length guard in readFrame. Same transparent
// nil-hook / real-default pattern as the seams elsewhere in the repo (cf.
// internal/tsl/{consumer,provider}/*_seam.go and
// internal/probel-sw02p/{consumer,provider}/decode_seam.go): in production
// the hook is nil and the guarded branch behaves exactly as written; a test
// installs a hook to drive the otherwise-dead arm, never to weaken it.
//
// The hooks are stored in atomic.Pointer rather than plain package vars
// because frame/handshake decoding runs on per-connection goroutines: a
// test that swaps a hook must not data-race a concurrently-decoding
// goroutine (the race detector flags an unsynchronised var write-vs-read
// even though prod only ever reads). Atomic load on the read path is
// negligible and keeps -race clean. Tests install hooks via setRandRead /
// setPlenSeam (see seam_test.go), which return a restore func.
//
// Why each branch is unreachable in production:
//   - crypto/rand.Read on every supported OS reads the kernel CSPRNG
//     (getrandom / RtlGenRandom / arc4random) and always fills the buffer
//     or never returns; the `if err != nil` arms in writeData / writeControl
//     / handshake therefore cannot be taken from a real call.
//   - readFrame derives the payload length from a 7-bit field (0..125), an
//     unsigned 16-bit extension, or an unsigned 64-bit extension whose high
//     bit is rejected first — so plen is never negative on a real frame.

// randReadHook, when non-nil, replaces crypto/rand.Read. nil in production.
var randReadHook atomic.Pointer[func(b []byte) (int, error)]

// randRead reads len(b) random bytes into b. Production behaviour is
// crypto/rand.Read; a test hook can force the error return.
func randRead(b []byte) (int, error) {
	if h := randReadHook.Load(); h != nil {
		return (*h)(b)
	}
	return rand.Read(b)
}

// plenSeamHook, when non-nil, rewrites the decoded payload length so a test
// can drive readFrame's `plen < 0` guard. nil in production (identity).
var plenSeamHook atomic.Pointer[func(plen int64) int64]

// plenSeam is the identity on every real frame; the hook exists only so a
// test can drive the negative-length defensive guard, never to weaken it.
func plenSeam(plen int64) int64 {
	if h := plenSeamHook.Load(); h != nil {
		return (*h)(plen)
	}
	return plen
}
