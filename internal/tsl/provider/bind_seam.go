package tsl

import (
	"net"
	"syscall"

	"dhs/internal/transport"
)

// Test seams for udpSender.bind's defensive branches that a live socket
// cannot reach. Transparent nil/identity-default pattern (cf.
// internal/probel-sw02p/provider/decode_seam.go): in production every hook
// is nil, so bind behaves EXACTLY as originally written — the short-circuit
// setsockopt sequence, the c.Control dispatch-error guard, and the
// *net.UDPConn type assertion are all preserved verbatim. Tests install a
// hook to drive an otherwise-dead defensive arm, never to weaken it.
//
// Why each arm is unreachable on a live socket:
//   - SetSocketReuseAddr / SetSocketBroadcast never fail on a freshly-opened
//     valid UDP fd (the kernel accepts both options on every supported OS).
//   - syscall.RawConn.Control never errors during ListenPacket (the fd is
//     valid and open).
//   - net.ListenConfig.ListenPacket(ctx, "udp", ...) always yields a
//     *net.UDPConn, so the type-assertion !ok arm cannot be observed.

// sockReuseAddrHook / sockBroadcastHook replace the individual setsockopt
// calls; nil in production (the real transport funcs run).
var (
	sockReuseAddrHook func(fd uintptr) error
	sockBroadcastHook func(fd uintptr) error
)

func sockReuseAddr(fd uintptr) error {
	if sockReuseAddrHook != nil {
		return sockReuseAddrHook(fd)
	}
	return transport.SetSocketReuseAddr(fd)
}

func sockBroadcast(fd uintptr) error {
	if sockBroadcastHook != nil {
		return sockBroadcastHook(fd)
	}
	return transport.SetSocketBroadcast(fd)
}

// dispatchControlErrHook, when non-nil, makes the c.Control call return an
// error WITHOUT running the closure — driving bind's dispatch-error guard.
// Nil in production.
var dispatchControlErrHook func() error

func dispatchControl(c syscall.RawConn, f func(uintptr)) error {
	if dispatchControlErrHook != nil {
		return dispatchControlErrHook()
	}
	return c.Control(f)
}

// assertUDPConnHook, when non-nil and returning false, forces bind's
// conn-type assertion onto its failure arm. Nil in production.
var assertUDPConnHook func() bool

func assertUDPConn(pc net.PacketConn) (*net.UDPConn, bool) {
	if assertUDPConnHook != nil && !assertUDPConnHook() {
		return nil, false
	}
	conn, ok := pc.(*net.UDPConn)
	return conn, ok
}

// rawControlSeam returns the ListenConfig.Control function. Structurally
// identical to the original inline closure (short-circuit on the first
// setsockopt error, then return opErr), with the three unreachable arms
// routed through the seams above. Identity-to-production when all hooks
// are nil.
func rawControlSeam() func(network, address string, c syscall.RawConn) error {
	return func(_, _ string, c syscall.RawConn) error {
		var opErr error
		if err := dispatchControl(c, func(fd uintptr) {
			if e := sockReuseAddr(fd); e != nil {
				opErr = e
				return
			}
			opErr = sockBroadcast(fd)
		}); err != nil {
			return err
		}
		return opErr
	}
}
