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
	byIndex, wantIdx, err := interfaceIndex(c.Iface)
	if err != nil {
		return nil, err
	}

	// SOCK_DGRAM, not SOCK_RAW: the kernel strips the Ethernet header and
	// hands over the LLDPDU, which is exactly what Decode wants.
	fd, err := syscall.Socket(syscall.AF_PACKET, syscall.SOCK_DGRAM, int(htons(EtherType)))
	if err != nil {
		if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) {
			return nil, fmt.Errorf("lldp: AF_PACKET socket needs CAP_NET_RAW "+
				"(setcap cap_net_raw+ep on this binary, or run as root): %w", err)
		}
		return nil, fmt.Errorf("lldp: AF_PACKET socket: %w", err)
	}
	defer func() { _ = syscall.Close(fd) }()

	if c.Iface != "" {
		if err := syscall.Bind(fd, &syscall.SockaddrLinklayer{
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
		if err := syscall.SetsockoptTimeval(fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &tv); err != nil {
			return out, fmt.Errorf("lldp: set read timeout: %w", err)
		}
		n, from, err := syscall.Recvfrom(fd, buf, 0)
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
func interfaceIndex(want string) (map[int]string, int, error) {
	ifs, err := net.Interfaces()
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
