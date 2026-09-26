//go:build windows

package lldp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// Capture listens for LLDP frames and answers [Source] from what it has heard.
//
// Windows path: Npcap. Windows' own raw sockets are IP-level and never see a
// non-IP Ethertype, so there is no stdlib way to read an LLDP frame. Npcap's
// wpcap.dll is loaded at run time from System32\Npcap with the standard
// library — no CGo, nothing linked into the binary. The operator installs
// Npcap; its licence forbids us shipping it (docs/adr/0005-deps.json,
// host_deps "npcap"). Without it, Neighbors reports [ErrCaptureUnsupported]
// and says what to install.
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

	// sys is the Npcap + interface-table surface. Nil means the real one,
	// which is what production always leaves it as.
	sys *npcapCalls
}

// npcapCalls is everything Neighbors needs from the host, behind function
// values, so the pairing, reading and stopping rules are tested without
// Npcap installed — which is every CI runner.
type npcapCalls struct {
	load        func() (npcapLib, error)
	interfaces  func() ([]net.Interface, error)
	adapterMACs func() (map[string]net.HardwareAddr, error)
}

// npcapLib is the loaded wpcap.dll.
type npcapLib interface {
	// devices lists capture device names, "\Device\NPF_{GUID}".
	devices() ([]string, error)
	// open starts a capture on one device, filtered to LLDP.
	open(dev string) (npcapHandle, error)
	release()
}

// npcapHandle is one open capture.
type npcapHandle interface {
	// next returns one whole Ethernet frame, or nil on a read timeout.
	next() ([]byte, error)
	close()
}

func (c Capture) os() npcapCalls {
	if c.sys != nil {
		return *c.sys
	}
	return npcapCalls{load: loadNpcap, interfaces: net.Interfaces, adapterMACs: adapterMACs}
}

// npcapDevicePrefix is how Npcap names a capture device for an adapter GUID.
const npcapDevicePrefix = `\Device\NPF_`

// Neighbors opens a capture on each interface, listens for Window, and
// decodes what arrives.
func (c Capture) Neighbors(ctx context.Context) (map[string]Neighbor, error) {
	sys := c.os()

	targets, err := captureTargets(sys.interfaces, c.Iface)
	if err != nil {
		return nil, err
	}
	lib, err := sys.load()
	if err != nil {
		return nil, err
	}
	defer lib.release()

	// Npcap names a device by adapter GUID, net.Interfaces by friendly
	// name. The Ethernet address is what both share.
	devs, err := lib.devices()
	if err != nil {
		return nil, err
	}
	macs, err := sys.adapterMACs()
	if err != nil {
		return nil, err
	}
	devByMAC := map[string]string{}
	for _, d := range devs {
		guid := strings.ToUpper(strings.TrimPrefix(d, npcapDevicePrefix))
		if mac, ok := macs[guid]; ok {
			devByMAC[mac.String()] = d
		}
	}
	type pair struct{ iface, dev string }
	var pairs []pair
	for _, i := range targets {
		d, ok := devByMAC[i.HardwareAddr.String()]
		if !ok {
			if c.Iface != "" {
				return nil, fmt.Errorf("lldp: Npcap has no capture device for %q", c.Iface)
			}
			continue
		}
		pairs = append(pairs, pair{i.Name, d})
	}

	var (
		caps    []openCapture
		openErr error
	)
	for _, p := range pairs {
		h, err := lib.open(p.dev)
		if err != nil {
			if openErr == nil {
				openErr = fmt.Errorf("lldp: open %s: %w", p.iface, err)
			}
			continue
		}
		caps = append(caps, openCapture{
			iface: p.iface,
			next: func() ([][]byte, error) {
				f, err := h.next()
				if f == nil {
					return nil, err
				}
				return [][]byte{f}, err
			},
			close: h.close,
		})
	}
	return listen(ctx, c.Window, c.Until, caps, openErr)
}

// Supported reports whether this build can capture LLDP locally. True on
// Windows, though Npcap still has to be installed on the host.
func Supported() bool { return true }

// ---- the real Npcap --------------------------------------------------------

const (
	pcapErrbufSize = 256
	// pcapSnaplen covers any LLDPDU: 802.1AB bounds it to one Ethernet frame.
	pcapSnaplen = 1600
	// pcapReadTimeoutMs bounds one blocking read, so a stop lands within
	// it rather than at the end of the window.
	pcapReadTimeoutMs = 250
	// loadWithAlteredSearchPath makes LoadLibraryEx resolve wpcap.dll's own
	// dependency (Packet.dll) from wpcap.dll's directory, not the
	// executable's.
	loadWithAlteredSearchPath = 0x00000008
	// errorNoData is GetAdaptersInfo's answer on a host with no adapters.
	errorNoData = syscall.Errno(232)
)

// lldpFilter is the capture filter: only LLDP reaches user space.
const lldpFilter = "ether proto 0x88cc"

// pcapIf mirrors libpcap's pcap_if_t.
type pcapIf struct {
	next        *pcapIf
	name        *byte
	description *byte
	addresses   unsafe.Pointer
	flags       uint32
}

// pcapPkthdr mirrors pcap_pkthdr. Windows' struct timeval is two 32-bit
// longs.
type pcapPkthdr struct {
	_      [2]int32 // ts
	caplen uint32
	_      uint32 // len on the wire
}

// bpfProgram mirrors struct bpf_program.
type bpfProgram struct {
	n     uint32
	insns unsafe.Pointer
}

type wpcap struct {
	dll                                  *syscall.DLL
	findalldevs, freealldevs             *syscall.Proc
	openLive, compile, setfilter, freecd *syscall.Proc
	nextEx, closeH                       *syscall.Proc
}

// loadNpcap loads System32\Npcap\wpcap.dll.
func loadNpcap() (npcapLib, error) {
	dir, err := systemDirectory()
	if err != nil {
		return nil, fmt.Errorf("lldp: find the system directory: %w", err)
	}
	path := filepath.Join(dir, "Npcap", "wpcap.dll")
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, _, callErr := syscall.NewLazyDLL("kernel32.dll").NewProc("LoadLibraryExW").
		Call(uintptr(unsafe.Pointer(p)), 0, loadWithAlteredSearchPath)
	if h == 0 {
		return nil, fmt.Errorf("lldp: Npcap is not installed (cannot load %s: %v) — install it from https://npcap.com/: %w",
			path, callErr, ErrCaptureUnsupported)
	}
	w := &wpcap{dll: &syscall.DLL{Name: path, Handle: syscall.Handle(h)}}
	for _, f := range []struct {
		name string
		p    **syscall.Proc
	}{
		{"pcap_findalldevs", &w.findalldevs},
		{"pcap_freealldevs", &w.freealldevs},
		{"pcap_open_live", &w.openLive},
		{"pcap_compile", &w.compile},
		{"pcap_setfilter", &w.setfilter},
		{"pcap_freecode", &w.freecd},
		{"pcap_next_ex", &w.nextEx},
		{"pcap_close", &w.closeH},
	} {
		proc, err := w.dll.FindProc(f.name)
		if err != nil {
			_ = w.dll.Release()
			return nil, fmt.Errorf("lldp: %s has no %s: %w", path, f.name, err)
		}
		*f.p = proc
	}
	return w, nil
}

func (w *wpcap) release() { _ = w.dll.Release() }

func (w *wpcap) devices() ([]string, error) {
	var (
		devs   *pcapIf
		errbuf [pcapErrbufSize]byte
	)
	r, _, _ := w.findalldevs.Call(uintptr(unsafe.Pointer(&devs)), uintptr(unsafe.Pointer(&errbuf[0])))
	if int32(r) != 0 {
		return nil, fmt.Errorf("lldp: pcap_findalldevs: %s", cBytes(errbuf[:]))
	}
	var out []string
	for d := devs; d != nil; d = d.next {
		out = append(out, cString(d.name))
	}
	if devs != nil {
		_, _, _ = w.freealldevs.Call(uintptr(unsafe.Pointer(devs)))
	}
	return out, nil
}

func (w *wpcap) open(dev string) (npcapHandle, error) {
	var errbuf [pcapErrbufSize]byte
	name := append([]byte(dev), 0)
	// Promiscuous: the LLDP group address 01-80-C2-00-00-0E is not one an
	// adapter listens to on its own. The filter keeps everything else in
	// the driver.
	h, _, _ := w.openLive.Call(uintptr(unsafe.Pointer(&name[0])), pcapSnaplen, 1, pcapReadTimeoutMs,
		uintptr(unsafe.Pointer(&errbuf[0])))
	if h == 0 {
		return nil, fmt.Errorf("pcap_open_live: %s", cBytes(errbuf[:]))
	}
	filter := append([]byte(lldpFilter), 0)
	var prog bpfProgram
	if r, _, _ := w.compile.Call(h, uintptr(unsafe.Pointer(&prog)), uintptr(unsafe.Pointer(&filter[0])), 1, 0xffffffff); int32(r) != 0 {
		_, _, _ = w.closeH.Call(h)
		return nil, fmt.Errorf("pcap_compile %q failed", lldpFilter)
	}
	r, _, _ := w.setfilter.Call(h, uintptr(unsafe.Pointer(&prog)))
	_, _, _ = w.freecd.Call(uintptr(unsafe.Pointer(&prog)))
	if int32(r) != 0 {
		_, _, _ = w.closeH.Call(h)
		return nil, errors.New("pcap_setfilter failed")
	}
	return &wpcapHandle{w: w, h: h}, nil
}

type wpcapHandle struct {
	w *wpcap
	h uintptr
}

func (c *wpcapHandle) next() ([]byte, error) {
	var (
		hdr  *pcapPkthdr
		data *byte
	)
	r, _, _ := c.w.nextEx.Call(c.h, uintptr(unsafe.Pointer(&hdr)), uintptr(unsafe.Pointer(&data)))
	switch int32(r) {
	case 1:
		// The driver's buffer is reused by the next call: copy out.
		return append([]byte(nil), unsafe.Slice(data, hdr.caplen)...), nil
	case 0:
		return nil, nil
	default:
		return nil, fmt.Errorf("pcap_next_ex returned %d", int32(r))
	}
}

func (c *wpcapHandle) close() { _, _, _ = c.w.closeH.Call(c.h) }

// systemDirectory is System32, from the OS rather than %SystemRoot%.
func systemDirectory() (string, error) {
	buf := make([]uint16, syscall.MAX_PATH)
	n, _, err := syscall.NewLazyDLL("kernel32.dll").NewProc("GetSystemDirectoryW").
		Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 {
		return "", err
	}
	return syscall.UTF16ToString(buf[:n]), nil
}

// adapterMACs maps each adapter's GUID ("{...}", upper case) to its Ethernet
// address.
func adapterMACs() (map[string]net.HardwareAddr, error) {
	size := uint32(unsafe.Sizeof(syscall.IpAdapterInfo{})) * 16
	for range 4 {
		buf := make([]syscall.IpAdapterInfo, int(size)/int(unsafe.Sizeof(syscall.IpAdapterInfo{}))+1)
		err := syscall.GetAdaptersInfo(&buf[0], &size)
		if errors.Is(err, syscall.ERROR_BUFFER_OVERFLOW) {
			continue
		}
		if errors.Is(err, errorNoData) {
			return map[string]net.HardwareAddr{}, nil
		}
		if err != nil {
			return nil, fmt.Errorf("lldp: GetAdaptersInfo: %w", err)
		}
		out := map[string]net.HardwareAddr{}
		for a := &buf[0]; a != nil; a = a.Next {
			n := min(int(a.AddressLength), len(a.Address))
			out[strings.ToUpper(cBytes(a.AdapterName[:]))] = append(net.HardwareAddr(nil), a.Address[:n]...)
		}
		return out, nil
	}
	return nil, errors.New("lldp: GetAdaptersInfo: the adapter list kept growing")
}

// cBytes is a NUL-terminated string held in a Go array.
func cBytes(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}

// cString is a NUL-terminated string owned by Npcap.
func cString(p *byte) string {
	if p == nil {
		return ""
	}
	n := 0
	for *(*byte)(unsafe.Add(unsafe.Pointer(p), n)) != 0 {
		n++
	}
	return string(unsafe.Slice(p, n))
}
