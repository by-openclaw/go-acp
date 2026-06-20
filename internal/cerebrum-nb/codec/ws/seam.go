package ws

import "crypto/rand"

// Test seam for the single genuinely-unreachable defensive branch in this
// package: the crypto/rand read inside newMaskKey. It uses the same
// transparent nil-hook / real-default pattern as the seams elsewhere in
// the repo (cf. internal/tsl/{consumer,provider}/*_seam.go and
// internal/probel-sw02p/{consumer,provider}/decode_seam.go): in production
// the seam is the identity behaviour, so the guarded branch behaves exactly
// as written; a test swaps the seam to drive the otherwise-dead error arm,
// never to weaken it.
//
// Why it is unreachable in production: crypto/rand.Read on every supported
// OS reads from the kernel CSPRNG (getrandom / RtlGenRandom / arc4random)
// and is documented to always fill the buffer or never return; the
// `if err != nil` arm in writeData / writeControl therefore cannot be
// taken from a real call. The seam lets a test force that error so the
// transport's write-side error propagation is still exercised, with the
// guard left intact.

// randRead reads len(b) random bytes into b. Production behaviour is
// crypto/rand.Read; tests override to force the error return.
var randRead = func(b []byte) (int, error) {
	return rand.Read(b)
}

// plenSeam is the identity on every real frame: readFrame derives the
// payload length from a 7-bit field, an unsigned 16-bit extension, or an
// unsigned 64-bit extension whose high bit is rejected first — so it is
// never negative in production. The seam exists only so a test can drive
// the `plen < 0` defensive guard in readFrame, never to weaken it. Same
// transparent nil-default pattern as the other seams in this package.
var plenSeam = func(plen int64) int64 { return plen }
