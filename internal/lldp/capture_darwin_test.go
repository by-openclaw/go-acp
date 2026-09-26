//go:build darwin

package lldp

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// bpfRecord is one record as BPF lays it out in a read: an 18-byte bpf_hdr,
// the frame, padding to a 4-byte boundary.
func bpfRecord(frame []byte) []byte {
	h := make([]byte, bpfHdrLen)
	binary.NativeEndian.PutUint32(h[8:], uint32(len(frame)))
	binary.NativeEndian.PutUint32(h[12:], uint32(len(frame)))
	binary.NativeEndian.PutUint16(h[16:], bpfHdrLen)
	out := append(h, frame...)
	for len(out)%4 != 0 {
		out = append(out, 0)
	}
	return out
}

func TestBPFRecords(t *testing.T) {
	a, b := []byte{1, 2, 3}, []byte{4, 5, 6, 7, 8}
	buf := append(bpfRecord(a), bpfRecord(b)...)
	got := bpfRecords(buf)
	if len(got) != 2 || string(got[0]) != string(a) || string(got[1]) != string(b) {
		t.Errorf("records = %x", got)
	}
	// A record claiming more than the buffer holds ends the walk.
	if got := bpfRecords(bpfRecord(a)[:bpfHdrLen+1]); len(got) != 0 {
		t.Errorf("truncated: %x", got)
	}
	// A header shorter than bpf_hdr is not a record.
	bad := bpfRecord(a)
	binary.NativeEndian.PutUint16(bad[16:], 4)
	if got := bpfRecords(bad); len(got) != 0 {
		t.Errorf("short header: %x", got)
	}
}

// ---- a fake kernel ---------------------------------------------------------

type fakeBPF struct {
	mu sync.Mutex

	openErrs []error // per /dev/bpfN, in order; past the end, open succeeds
	ioctlErr map[uint]error
	reads    map[int][][]byte // fd -> buffers to return, then EAGAIN
	readErr  error
	ifaces   []net.Interface

	opened []string
	ioctls []uint
	ifname string
	closed map[int]bool
	nextFD int
}

func (f *fakeBPF) calls() *bpfCalls {
	return &bpfCalls{
		open: func(path string) (int, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.opened = append(f.opened, path)
			if n := len(f.opened) - 1; n < len(f.openErrs) && f.openErrs[n] != nil {
				return -1, f.openErrs[n]
			}
			f.nextFD++
			return f.nextFD, nil
		},
		ioctl: func(fd int, req uint, arg unsafe.Pointer) error {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.ioctls = append(f.ioctls, req)
			if req == syscall.BIOCSETIF {
				f.ifname = strings.TrimRight(string((*[32]byte)(arg)[:16]), "\x00")
			}
			return f.ioctlErr[req]
		},
		read: func(fd int, p []byte) (int, error) {
			f.mu.Lock()
			if bufs := f.reads[fd]; len(bufs) > 0 {
				f.reads[fd] = bufs[1:]
				f.mu.Unlock()
				return copy(p, bufs[0]), nil
			}
			err := f.readErr
			f.mu.Unlock()
			if err != nil {
				return 0, err
			}
			time.Sleep(time.Millisecond)
			return 0, syscall.EAGAIN
		},
		close: func(fd int) error {
			f.mu.Lock()
			if f.closed == nil {
				f.closed = map[int]bool{}
			}
			f.closed[fd] = true
			f.mu.Unlock()
			return nil
		},
		interfaces: func() ([]net.Interface, error) { return f.ifaces, nil },
	}
}

func macInterfaces() []net.Interface {
	mac := net.HardwareAddr{2, 0, 0, 0, 0, 1}
	return []net.Interface{
		{Name: "en0", Flags: net.FlagUp, HardwareAddr: mac},
		{Name: "lo0", Flags: net.FlagUp | net.FlagLoopback},
		{Name: "en1", Flags: net.FlagUp, HardwareAddr: mac},
	}
}

func macCapture(f *fakeBPF, iface string) Capture {
	return Capture{Iface: iface, Window: 100 * time.Millisecond, sys: f.calls()}
}

// ---- behaviour -------------------------------------------------------------

func TestDarwinCaptureHearsAFrame(t *testing.T) {
	f := &fakeBPF{ifaces: macInterfaces(), reads: map[int][][]byte{
		1: {bpfRecord(ethernetFrame(frame(t, mandatory(t)...)))},
	}}
	got, err := macCapture(f, "").Neighbors(context.Background())
	if err != nil {
		t.Fatalf("Neighbors: %v", err)
	}
	if len(got) != 1 || got["en0"].ChassisID == "" {
		t.Errorf("got %+v, want en0's neighbour", got)
	}
	if !f.closed[1] || !f.closed[2] {
		t.Errorf("closed = %v, want both BPF devices closed", f.closed)
	}
}

// The buffer size is set before the interface is attached (BPF refuses it
// after), and the named interface is the one attached.
func TestDarwinOpenSequence(t *testing.T) {
	f := &fakeBPF{ifaces: macInterfaces()}
	if _, err := macCapture(f, "en1").Neighbors(context.Background()); err != nil {
		t.Fatalf("Neighbors: %v", err)
	}
	want := []uint{syscall.BIOCSBLEN, syscall.BIOCSETIF, syscall.BIOCIMMEDIATE,
		syscall.BIOCPROMISC, syscall.BIOCSETF, syscall.BIOCSRTIMEOUT}
	if len(f.ioctls) != len(want) {
		t.Fatalf("ioctls = %x, want %x", f.ioctls, want)
	}
	for i := range want {
		if f.ioctls[i] != want[i] {
			t.Errorf("ioctl %d = %x, want %x", i, f.ioctls[i], want[i])
		}
	}
	if f.ifname != "en1" {
		t.Errorf("attached to %q", f.ifname)
	}
}

// A busy device is skipped for the next one.
func TestDarwinSkipsBusyDevices(t *testing.T) {
	f := &fakeBPF{ifaces: macInterfaces(), openErrs: []error{syscall.EBUSY, syscall.EBUSY}}
	if _, err := macCapture(f, "en0").Neighbors(context.Background()); err != nil {
		t.Fatalf("Neighbors: %v", err)
	}
	if len(f.opened) != 3 || f.opened[2] != "/dev/bpf2" {
		t.Errorf("opened %v, want bpf0, bpf1 busy then bpf2", f.opened)
	}
}

// No permission is said once, with what to do about it.
func TestDarwinPermissionIsExplained(t *testing.T) {
	for _, e := range []error{syscall.EACCES, syscall.EPERM} {
		f := &fakeBPF{ifaces: macInterfaces(), openErrs: []error{e}}
		_, err := macCapture(f, "").Neighbors(context.Background())
		if !errors.Is(err, e) || !strings.Contains(err.Error(), "needs root") {
			t.Errorf("%v: err = %v", e, err)
		}
	}
	// Refused on the second interface: the first one's device is closed.
	f := &fakeBPF{ifaces: macInterfaces(), openErrs: []error{nil, syscall.EACCES}}
	if _, err := macCapture(f, "").Neighbors(context.Background()); !errors.Is(err, syscall.EACCES) {
		t.Errorf("err = %v", err)
	}
	if !f.closed[1] {
		t.Error("the device opened before the refusal must be closed")
	}
}

func TestDarwinOpenFailures(t *testing.T) {
	boom := syscall.ENXIO
	// Any other open error is reported with the device it hit.
	f := &fakeBPF{ifaces: macInterfaces(), openErrs: []error{boom}}
	if _, err := macCapture(f, "en0").Neighbors(context.Background()); !errors.Is(err, boom) || !strings.Contains(err.Error(), "/dev/bpf0") {
		t.Errorf("named: err = %v", err)
	}
	// Every device busy.
	busy := make([]error, bpfDevices)
	for i := range busy {
		busy[i] = syscall.EBUSY
	}
	f = &fakeBPF{ifaces: macInterfaces(), openErrs: busy}
	if _, err := macCapture(f, "en0").Neighbors(context.Background()); err == nil || !strings.Contains(err.Error(), "busy") {
		t.Errorf("all busy: err = %v", err)
	}
	// With every interface, one that fails does not hide the other.
	f = &fakeBPF{ifaces: macInterfaces(), openErrs: []error{boom}, reads: map[int][][]byte{
		1: {bpfRecord(ethernetFrame(frame(t, mandatory(t)...)))},
	}}
	got, err := macCapture(f, "").Neighbors(context.Background())
	if !errors.Is(err, boom) || len(got) != 1 || got["en1"].ChassisID == "" {
		t.Errorf("partial: %v, %v", got, err)
	}
}

// Each setup step that fails closes the device and names the step; a
// refused promiscuous mode is not fatal.
func TestDarwinIoctlFailures(t *testing.T) {
	boom := syscall.EINVAL
	for req, what := range map[uint]string{
		syscall.BIOCSBLEN:     "buffer",
		syscall.BIOCSETIF:     "attach",
		syscall.BIOCIMMEDIATE: "immediate",
		syscall.BIOCSETF:      "filter",
		syscall.BIOCSRTIMEOUT: "timeout",
	} {
		f := &fakeBPF{ifaces: macInterfaces(), ioctlErr: map[uint]error{req: boom}}
		_, err := macCapture(f, "en0").Neighbors(context.Background())
		if !errors.Is(err, boom) || !strings.Contains(err.Error(), what) {
			t.Errorf("%s: err = %v", what, err)
		}
		if !f.closed[1] {
			t.Errorf("%s: device left open", what)
		}
	}
	f := &fakeBPF{ifaces: macInterfaces(), ioctlErr: map[uint]error{syscall.BIOCPROMISC: boom}}
	if _, err := macCapture(f, "en0").Neighbors(context.Background()); err != nil {
		t.Errorf("promiscuous refused: err = %v", err)
	}
}

func TestDarwinReadErrors(t *testing.T) {
	f := &fakeBPF{ifaces: macInterfaces(), readErr: syscall.EINTR}
	if _, err := macCapture(f, "en0").Neighbors(context.Background()); err != nil {
		t.Errorf("EINTR is a retry, not a failure: %v", err)
	}
	f = &fakeBPF{ifaces: macInterfaces(), readErr: syscall.ENXIO}
	if _, err := macCapture(f, "en0").Neighbors(context.Background()); !errors.Is(err, syscall.ENXIO) {
		t.Errorf("err = %v", err)
	}
}

func TestDarwinUnknownInterfaceIsAnError(t *testing.T) {
	f := &fakeBPF{ifaces: macInterfaces()}
	if _, err := macCapture(f, "en9").Neighbors(context.Background()); err == nil || !strings.Contains(err.Error(), `"en9"`) {
		t.Errorf("err = %v", err)
	}
}

func TestLLDPProgramMatchesTheEthertype(t *testing.T) {
	p := lldpProgram()
	if len(p) != 4 || p[0].K != 12 || p[1].K != EtherType || p[2].K == 0 || p[3].K != 0 {
		t.Errorf("program = %+v", p)
	}
}

func TestDarwinSupported(t *testing.T) {
	if !Supported() {
		t.Error("macOS captures through BPF")
	}
}

// The real kernel: opening BPF either works (root) or is refused with the
// explained error. Either way no device is left open.
func TestDarwinRealKernel(t *testing.T) {
	_, err := Capture{Iface: "lo0", Window: 50 * time.Millisecond}.Neighbors(context.Background())
	if err != nil && !strings.Contains(err.Error(), "lldp:") {
		t.Errorf("err = %v", err)
	}
}
