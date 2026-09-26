//go:build darwin

package lldp

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"syscall"
	"time"
	"unsafe"
)

// Capture listens for LLDP frames and answers [Source] from what it has heard.
//
// macOS path: the kernel's BPF devices (/dev/bpf*), one per interface, with
// a filter so only LLDP reaches user space. Stdlib syscall only — no
// libpcap, no cgo. Opening /dev/bpf* needs root, or read access to it (the
// access_bpf group Wireshark's ChmodBPF sets up).
type Capture struct {
	// Iface limits capture to one interface. Empty means every interface
	// that is up, is not loopback and has an Ethernet address.
	Iface string
	// Window is how long Neighbors waits for frames. LLDP is announced on
	// the sender's schedule (30 s by default). Zero means 2s.
	Window time.Duration
	// Until, when set, ends the wait as soon as it reports true for the
	// neighbours heard so far. Nil waits out the whole window.
	Until func(map[string]Neighbor) bool

	// sys is the BPF + interface-table surface. Nil means the real kernel,
	// which is what production always leaves it as.
	sys *bpfCalls
}

// bpfCalls is the BPF device and the interface table behind function
// values, so the setup, reading and stopping rules are tested without root —
// which is every CI runner.
type bpfCalls struct {
	open       func(path string) (int, error)
	ioctl      func(fd int, req uint, arg unsafe.Pointer) error
	read       func(fd int, p []byte) (int, error)
	close      func(fd int) error
	interfaces func() ([]net.Interface, error)
}

func (c Capture) os() bpfCalls {
	if c.sys != nil {
		return *c.sys
	}
	return bpfCalls{
		open:       func(path string) (int, error) { return syscall.Open(path, syscall.O_RDWR, 0) },
		ioctl:      ioctl,
		read:       syscall.Read,
		close:      syscall.Close,
		interfaces: net.Interfaces,
	}
}

func ioctl(fd int, req uint, arg unsafe.Pointer) error {
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(req), uintptr(arg)); e != 0 {
		return e
	}
	return nil
}

const (
	// bpfBufLen is the kernel buffer, and so the size of every read: BPF
	// hands back whole buffers.
	bpfBufLen = 32768
	// bpfDevices bounds the search for a free /dev/bpfN.
	bpfDevices = 256
	// bpfReadTimeout bounds one blocking read, so a stop lands within it
	// rather than at the end of the window.
	bpfReadTimeout = 250 * time.Millisecond
	// bpfHdrLen is the fixed part of struct bpf_hdr: a 32-bit timeval,
	// caplen, datalen, hdrlen.
	bpfHdrLen = 18
)

// lldpProgram keeps only frames with the LLDP Ethertype.
func lldpProgram() []syscall.BpfInsn {
	return []syscall.BpfInsn{
		{Code: syscall.BPF_LD | syscall.BPF_H | syscall.BPF_ABS, K: 12},
		{Code: syscall.BPF_JMP | syscall.BPF_JEQ | syscall.BPF_K, Jt: 0, Jf: 1, K: EtherType},
		{Code: syscall.BPF_RET | syscall.BPF_K, K: 0xffff},
		{Code: syscall.BPF_RET | syscall.BPF_K, K: 0},
	}
}

// Neighbors opens a BPF device on each interface, listens for Window, and
// decodes what arrives.
func (c Capture) Neighbors(ctx context.Context) (map[string]Neighbor, error) {
	sys := c.os()
	targets, err := captureTargets(sys.interfaces, c.Iface)
	if err != nil {
		return nil, err
	}
	var (
		caps    []openCapture
		openErr error
	)
	for _, i := range targets {
		fd, err := openBPF(sys, i.Name)
		if err != nil {
			// Permission is the same for every interface: say it once,
			// now, rather than once per interface later.
			if errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM) {
				for _, oc := range caps {
					oc.close()
				}
				return nil, err
			}
			if c.Iface != "" {
				return nil, err
			}
			if openErr == nil {
				openErr = err
			}
			continue
		}
		buf := make([]byte, bpfBufLen)
		caps = append(caps, openCapture{
			iface: i.Name,
			next: func() ([][]byte, error) {
				n, err := sys.read(fd, buf)
				if err != nil {
					if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EINTR) {
						return nil, nil
					}
					return nil, err
				}
				return bpfRecords(buf[:n]), nil
			},
			close: func() { _ = sys.close(fd) },
		})
	}
	return listen(ctx, c.Window, c.Until, caps, openErr)
}

// openBPF finds a free /dev/bpfN and attaches it to iface, filtered to LLDP.
func openBPF(sys bpfCalls, iface string) (int, error) {
	fd := -1
	for n := 0; n < bpfDevices; n++ {
		f, err := sys.open(fmt.Sprintf("/dev/bpf%d", n))
		if err == nil {
			fd = f
			break
		}
		if errors.Is(err, syscall.EBUSY) {
			continue
		}
		if errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM) {
			return -1, fmt.Errorf("lldp: /dev/bpf needs root, or read access for this user "+
				"(the access_bpf group Wireshark's ChmodBPF sets up): %w", err)
		}
		return -1, fmt.Errorf("lldp: open /dev/bpf%d: %w", n, err)
	}
	if fd < 0 {
		return -1, fmt.Errorf("lldp: every /dev/bpf device is busy")
	}

	fail := func(what string, err error) (int, error) {
		_ = sys.close(fd)
		return -1, fmt.Errorf("lldp: %s on %s: %w", what, iface, err)
	}
	// The buffer size must be set before the interface is attached.
	bufLen := uint32(bpfBufLen)
	if err := sys.ioctl(fd, syscall.BIOCSBLEN, unsafe.Pointer(&bufLen)); err != nil {
		return fail("set BPF buffer", err)
	}
	var ifr [32]byte // struct ifreq: IFNAMSIZ name + union
	copy(ifr[:15], iface)
	if err := sys.ioctl(fd, syscall.BIOCSETIF, unsafe.Pointer(&ifr[0])); err != nil {
		return fail("attach BPF", err)
	}
	on := uint32(1)
	if err := sys.ioctl(fd, syscall.BIOCIMMEDIATE, unsafe.Pointer(&on)); err != nil {
		return fail("set BPF immediate mode", err)
	}
	// Promiscuous, because the LLDP group address 01-80-C2-00-00-0E is not
	// one an interface listens to on its own. Not fatal: some interfaces
	// refuse it and still deliver the group.
	_ = sys.ioctl(fd, syscall.BIOCPROMISC, nil)
	insns := lldpProgram()
	prog := syscall.BpfProgram{Len: uint32(len(insns)), Insns: &insns[0]}
	if err := sys.ioctl(fd, syscall.BIOCSETF, unsafe.Pointer(&prog)); err != nil {
		return fail("set BPF filter", err)
	}
	tv := syscall.NsecToTimeval(int64(bpfReadTimeout))
	if err := sys.ioctl(fd, syscall.BIOCSRTIMEOUT, unsafe.Pointer(&tv)); err != nil {
		return fail("set BPF read timeout", err)
	}
	return fd, nil
}

// bpfRecords splits one BPF read into the frames it carries.
//
// Each frame follows a struct bpf_hdr of bh_hdrlen bytes and is bh_caplen
// long, and the next record starts at the 4-byte-aligned end
// (BPF_WORDALIGN). A record that runs past the buffer ends the walk.
func bpfRecords(buf []byte) [][]byte {
	var out [][]byte
	for off := 0; off+bpfHdrLen <= len(buf); {
		caplen := int(binary.NativeEndian.Uint32(buf[off+8:]))
		hdrlen := int(binary.NativeEndian.Uint16(buf[off+16:]))
		start, end := off+hdrlen, off+hdrlen+caplen
		if hdrlen < bpfHdrLen || end > len(buf) {
			break
		}
		out = append(out, buf[start:end])
		off += (hdrlen + caplen + 3) &^ 3
	}
	return out
}

// Supported reports whether this build can capture LLDP locally. True on
// macOS, though /dev/bpf may still refuse this user.
func Supported() bool { return true }
