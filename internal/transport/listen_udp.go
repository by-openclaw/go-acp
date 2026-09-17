package transport

import (
	"context"
	"fmt"
	"net"
	"syscall"
)

// UDPBindOptions are the socket options that must be set BEFORE bind.
//
// That timing is the whole reason this type exists rather than a
// post-bind ApplySocketOptions: SO_REUSEADDR has no effect once the socket
// is bound, so it has to go in the ListenConfig.Control window. Options
// that can be set afterwards belong in SocketOptions instead.
type UDPBindOptions struct {
	// ReuseAddr lets several processes share the port and each receive the
	// same datagrams. Set it on receivers that must coexist — a second dhs
	// watching the same broadcast, or dhs alongside a vendor tool.
	//
	// Note this is the opposite choice from ListenUDP, which deliberately
	// does NOT set it so a port already owned by something else fails loudly
	// rather than silently splitting traffic.
	ReuseAddr bool

	// Broadcast permits sends to 255.255.255.255 or a subnet broadcast.
	// Without it the kernel refuses those datagrams outright.
	Broadcast bool
}

// Seams for the arms a live socket cannot reach, kept here so they are
// written and tested ONCE rather than per protocol.
//
// Before this function existed, the osc and tsl consumers and providers each
// carried their own copy of this bind plus its own listen_seam.go /
// bind_seam.go to drive the same three dead branches to their coverage
// floors — four copies of one socket policy, and four places to fix a bug in
// it.
//
// Why each is unreachable in production:
//   - udpRawControl: syscall.RawConn.Control only errors on an already-invalid
//     fd, and the runtime is mid-bind on a fresh one.
//   - udpSetReuseAddr / udpSetBroadcast: the kernel accepts both options on a
//     newly-opened UDP fd on every supported OS.
//   - udpAssertConn: ListenPacket on a "udp" network always yields a
//     *net.UDPConn.
var (
	udpRawControl = func(c syscall.RawConn, f func(fd uintptr)) error { return c.Control(f) }
	udpSetReuse   = SetSocketReuseAddr
	udpSetBcast   = SetSocketBroadcast
	udpAssertConn = func(pc net.PacketConn) (*net.UDPConn, bool) {
		conn, ok := pc.(*net.UDPConn)
		return conn, ok
	}
)

// ListenUDPAddr binds a specific address for UDP and returns the connection.
//
// network is "udp", "udp4" or "udp6". addr is "host:port"; an empty host
// binds every interface, and port 0 lets the kernel choose — so ":0" is the
// ephemeral-local-port case a sender wants.
//
// This is the addr-based sibling of [ListenUDP], which takes a port, forces
// udp4 and binds all interfaces. Callers that need to choose the interface,
// the family, or an ephemeral port use this one.
//
// The returned conn is the caller's to close.
func ListenUDPAddr(ctx context.Context, network, addr string, opts UDPBindOptions) (*net.UDPConn, error) {
	lc := net.ListenConfig{}
	if opts.ReuseAddr || opts.Broadcast {
		// Control runs after socket creation and before bind — the only
		// window in which SO_REUSEADDR takes effect.
		lc.Control = func(_, _ string, c syscall.RawConn) error {
			var opErr error
			if err := udpRawControl(c, func(fd uintptr) {
				if opts.ReuseAddr {
					if opErr = udpSetReuse(fd); opErr != nil {
						return
					}
				}
				if opts.Broadcast {
					opErr = udpSetBcast(fd)
				}
			}); err != nil {
				return err
			}
			return opErr
		}
	}
	pc, err := lc.ListenPacket(ctx, network, addr)
	if err != nil {
		return nil, fmt.Errorf("%w: udp listen %s %s: %v", ErrListenFailed, network, addr, err)
	}
	conn, ok := udpAssertConn(pc)
	if !ok {
		_ = pc.Close()
		return nil, fmt.Errorf("%w: udp listen %s %s: unexpected conn type %T",
			ErrListenFailed, network, addr, pc)
	}
	return conn, nil
}
