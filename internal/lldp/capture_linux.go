//go:build linux

package lldp

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"syscall"
	"time"
)

// Capture listens for LLDP frames and answers [Source] from what it has heard.
//
// Linux path: an AF_PACKET socket bound to Ethertype 0x88CC. Stdlib syscall
// only — no libpcap, no cgo. It needs CAP_NET_RAW, which the dhs_capture
// Ansible role grants to the binary rather than running dhs as root.
type Capture struct {
	// Iface limits capture to one interface. Empty means every interface.
	Iface string
	// Window is how long Neighbors waits for frames. LLDP is announced on
	// the sender's schedule (30 s by default), so a short window on a quiet
	// link legitimately returns nothing; the neighbour is not gone, it just
	// has not spoken yet. Zero means 2s.
	Window time.Duration

	// sys is the operating-system surface Neighbors sits on. Nil means
	// the real calls, which is what production always leaves it as.
	sys *osCalls
}

// osCalls is the AF_PACKET socket and the interface table, behind
// function values.
//
// It exists because everything worth testing in Neighbors — which frames
// are kept, which are skipped, which errors are explained and which are
// returned bare — sits ABOVE a socket that needs CAP_NET_RAW to open.
// Without this seam that whole body is unreachable on any unprivileged
// machine, which is every CI runner we have, so the decode-and-dispatch
// rules would be pinned by nothing.
//
// A struct of functions rather than an interface: each member has one
// real implementation and one test double, so an interface would only
// add a name. A FIELD on Capture rather than a package variable: a
// package variable read inside Neighbors is shared by every test in the
// package, and this tree has already paid for that shape once under
// -race.
type osCalls struct {
	socket     func(domain, typ, proto int) (int, error)
	bind       func(fd int, sa syscall.Sockaddr) error
	setTimeout func(fd int, tv *syscall.Timeval) error
	recvfrom   func(fd int, p []byte) (int, syscall.Sockaddr, error)
	close      func(fd int) error
	interfaces func() ([]net.Interface, error)
}

// realOS is the kernel. The only value production ever uses.
var realOS = osCalls{
	socket: syscall.Socket,
	bind:   syscall.Bind,
	setTimeout: func(fd int, tv *syscall.Timeval) error {
		return syscall.SetsockoptTimeval(fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, tv)
	},
	recvfrom: func(fd int, p []byte) (int, syscall.Sockaddr, error) {
		return syscall.Recvfrom(fd, p, 0)
	},
	close:      syscall.Close,
	interfaces: net.Interfaces,
}

// os resolves the seam. Absent means the kernel.
func (c Capture) os() osCalls {
	if c.sys != nil {
		return *c.sys
	}
	return realOS
}

const (
	defaultWindow = 2 * time.Second
	// readTick bounds one blocking read so ctx cancellation lands within a
	// tick rather than at the end of the whole window.
	readTick = 250 * time.Millisecond
)

// htons puts an Ethertype in network byte order for AF_PACKET's protocol
// field, which is a host-order uint16 holding a big-endian value.
func htons(v uint16) uint16 {
	var b [2]byte
	binary.BigEndian.PutUint16(b[:], v)
	return binary.NativeEndian.Uint16(b[:])
}

// Neighbors opens the socket, listens for Window, and decodes what arrives.
//
// Frames that fail to decode are SKIPPED, not fatal: a malformed LLDPDU from
// one misbehaving switch must not hide the four that are fine.
func (c Capture) Neighbors(ctx context.Context) (map[string]Neighbor, error) {
	sys := c.os()

	byIndex, wantIdx, err := interfaceIndex(sys.interfaces, c.Iface)
	if err != nil {
		return nil, err
	}

	// SOCK_DGRAM, not SOCK_RAW: the kernel strips the Ethernet header and
	// hands over the LLDPDU, which is exactly what Decode wants.
	fd, err := sys.socket(syscall.AF_PACKET, syscall.SOCK_DGRAM, int(htons(EtherType)))
	if err != nil {
		if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) {
			return nil, fmt.Errorf("lldp: AF_PACKET socket needs CAP_NET_RAW "+
				"(setcap cap_net_raw+ep on this binary, or run as root): %w", err)
		}
		return nil, fmt.Errorf("lldp: AF_PACKET socket: %w", err)
	}
	defer func() { _ = sys.close(fd) }()

	if c.Iface != "" {
		if err := sys.bind(fd, &syscall.SockaddrLinklayer{
			Protocol: htons(EtherType),
			Ifindex:  wantIdx,
		}); err != nil {
			return nil, fmt.Errorf("lldp: bind %s: %w", c.Iface, err)
		}
	}

	window := c.Window
	if window <= 0 {
		window = defaultWindow
	}
	deadline := time.Now().Add(window)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}

	out := map[string]Neighbor{}
	buf := make([]byte, 1500)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return out, nil
		}
		if err := ctx.Err(); err != nil {
			return out, err
		}
		tv := syscall.NsecToTimeval(int64(min(remaining, readTick)))
		if err := sys.setTimeout(fd, &tv); err != nil {
			return out, fmt.Errorf("lldp: set read timeout: %w", err)
		}
		n, from, err := sys.recvfrom(fd, buf)
		if err != nil {
			if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EINTR) {
				continue
			}
			return out, fmt.Errorf("lldp: recv: %w", err)
		}
		ll, ok := from.(*syscall.SockaddrLinklayer)
		if !ok {
			continue
		}
		name := byIndex[ll.Ifindex]
		if name == "" || (c.Iface != "" && name != c.Iface) {
			continue
		}
		nb, derr := Decode(buf[:n])
		if derr != nil {
			continue
		}
		// A shutdown LLDPDU means the neighbour is leaving; recording it
		// would publish a switch port that is no longer attached.
		if nb.Shutdown() {
			delete(out, name)
			continue
		}
		out[name] = nb
	}
}

// interfaceIndex maps ifindex to name and resolves the requested interface.
//
// A name that does not exist is an ERROR rather than an empty result: a typo
// would otherwise present as "no neighbours", which looks identical to an
// unplugged cable and sends the operator to the wrong end of the link.
func interfaceIndex(list func() ([]net.Interface, error), want string) (map[int]string, int, error) {
	ifs, err := list()
	if err != nil {
		return nil, 0, fmt.Errorf("lldp: list interfaces: %w", err)
	}
	out := make(map[int]string, len(ifs))
	idx := -1
	for _, i := range ifs {
		out[i.Index] = i.Name
		if i.Name == want {
			idx = i.Index
		}
	}
	if want != "" && idx < 0 {
		return nil, 0, fmt.Errorf("lldp: no interface named %q on this host", want)
	}
	return out, idx, nil
}

// Supported reports whether this build can capture LLDP locally. True on
// Linux, though the socket may still be refused for want of CAP_NET_RAW.
func Supported() bool { return true }
