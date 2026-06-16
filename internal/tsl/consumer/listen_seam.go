package tsl

import (
	"net"
	"syscall"
)

// Test seams for two defensive branches in udpSession.listen that the
// production code path cannot reach because the OS / stdlib pre-guarantee
// the inputs. They use the same transparent nil-hook / real-default
// pattern as the decode seams elsewhere in the repo (cf.
// internal/probel-sw02p/{consumer,provider}/decode_seam.go): in production
// every seam is the identity behaviour, so the guarded branches behave
// exactly as written; a test swaps the seam to drive the otherwise-dead
// defensive arm, never to weaken it.
//
// Why each seam exists:
//
//   - rawControl / controlErrHook: net.ListenConfig.Control receives a
//     syscall.RawConn whose Control method only errors when the
//     underlying fd is already invalid. For a socket the runtime is in
//     the middle of binding that never happens, so the
//     `if err := c.Control(...); err != nil { return err }` arm in listen
//     is unreachable from a real bind. rawControl defaults to calling
//     c.Control directly; a test substitutes a function returning an error
//     to drive that arm.
//
//   - listenConnAssert: lc.ListenPacket(ctx, "udp", addr) always returns a
//     *net.UDPConn for the "udp" network, so the `conn, ok := pc.(*net.
//     UDPConn); if !ok` guard in listen can never take the false branch.
//     listenConnAssert defaults to that exact type assertion; a test swaps
//     it to return ok=false and drive the unexpected-conn-type arm.

// rawControl invokes c.Control(f) in production. Overridable in tests to
// force the c.Control error return.
var rawControl = func(c syscall.RawConn, f func(fd uintptr)) error {
	return c.Control(f)
}

// listenConnAssert performs the *net.UDPConn type assertion on the
// PacketConn returned by ListenPacket. Production behaviour is the plain
// assertion; tests override to drive the !ok arm.
var listenConnAssert = func(pc net.PacketConn) (*net.UDPConn, bool) {
	conn, ok := pc.(*net.UDPConn)
	return conn, ok
}
