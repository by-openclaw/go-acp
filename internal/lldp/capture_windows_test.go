//go:build windows

package lldp

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---- a fake Npcap ----------------------------------------------------------

type fakeHandle struct {
	mu     sync.Mutex
	frames [][]byte
	err    error
	closed bool
}

func (h *fakeHandle) next() ([]byte, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.frames) > 0 {
		f := h.frames[0]
		h.frames = h.frames[1:]
		return f, nil
	}
	if h.err != nil {
		return nil, h.err
	}
	// A read timeout, the way pcap_next_ex answers an idle link.
	h.mu.Unlock()
	time.Sleep(time.Millisecond)
	h.mu.Lock()
	return nil, nil
}

func (h *fakeHandle) close() {
	h.mu.Lock()
	h.closed = true
	h.mu.Unlock()
}

type fakeLib struct {
	devs     []string
	devsErr  error
	handles  map[string]*fakeHandle
	openErr  map[string]error
	released bool
}

func (l *fakeLib) devices() ([]string, error) { return l.devs, l.devsErr }
func (l *fakeLib) open(dev string) (npcapHandle, error) {
	if err := l.openErr[dev]; err != nil {
		return nil, err
	}
	return l.handles[dev], nil
}
func (l *fakeLib) release() { l.released = true }

var (
	macA = net.HardwareAddr{0x02, 0, 0, 0, 0, 0x0a}
	macB = net.HardwareAddr{0x02, 0, 0, 0, 0, 0x0b}
)

const (
	devA = `\Device\NPF_{AAAAAAAA-0000-0000-0000-000000000000}`
	devB = `\Device\NPF_{bbbbbbbb-0000-0000-0000-000000000000}`
)

func winInterfaces() []net.Interface {
	return []net.Interface{
		{Index: 1, Name: "Ethernet", Flags: net.FlagUp, HardwareAddr: macA},
		{Index: 2, Name: "Wi-Fi", Flags: net.FlagUp, HardwareAddr: macB},
		{Index: 3, Name: "Loopback", Flags: net.FlagUp | net.FlagLoopback},
		{Index: 4, Name: "Unplugged", HardwareAddr: net.HardwareAddr{2, 0, 0, 0, 0, 0x0c}},
	}
}

func winMACs() map[string]net.HardwareAddr {
	return map[string]net.HardwareAddr{
		"{AAAAAAAA-0000-0000-0000-000000000000}": macA,
		"{BBBBBBBB-0000-0000-0000-000000000000}": macB,
	}
}

func winCapture(lib *fakeLib, iface string) Capture {
	return Capture{
		Iface:  iface,
		Window: 200 * time.Millisecond,
		sys: &npcapCalls{
			load:        func() (npcapLib, error) { return lib, nil },
			interfaces:  func() ([]net.Interface, error) { return winInterfaces(), nil },
			adapterMACs: func() (map[string]net.HardwareAddr, error) { return winMACs(), nil },
		},
	}
}

func twoDevices(a, b *fakeHandle) *fakeLib {
	return &fakeLib{
		devs:    []string{devA, devB, `\Device\NPF_Loopback`},
		handles: map[string]*fakeHandle{devA: a, devB: b},
	}
}

// ---- behaviour -------------------------------------------------------------

// Npcap names devices by adapter GUID and the host by friendly name; the
// capture pairs them by Ethernet address, whatever the GUID's letter case.
func TestWindowsCapturePairsDevicesToInterfacesByMAC(t *testing.T) {
	a := &fakeHandle{frames: [][]byte{ethernetFrame(frame(t, mandatory(t)...))}}
	b := &fakeHandle{}
	lib := twoDevices(a, b)

	got, err := winCapture(lib, "").Neighbors(context.Background())
	if err != nil {
		t.Fatalf("Neighbors: %v", err)
	}
	if len(got) != 1 || got["Ethernet"].ChassisID != "00-11-22-33-44-55" {
		t.Errorf("got %+v, want the Ethernet neighbour only", got)
	}
	if !a.closed || !b.closed || !lib.released {
		t.Error("every capture and the library must be closed")
	}
}

// Frames that are not LLDP, or do not decode, are skipped; a shutdown
// LLDPDU removes the neighbour it follows.
func TestWindowsCaptureSkipsNoiseAndHonoursShutdown(t *testing.T) {
	shut := append(mandatory(t)[:2:2], ttl(0))
	a := &fakeHandle{frames: [][]byte{
		{0x01, 0x02},                           // runt
		append(ethernetFrame(nil)[:12], 0x08, 0x00), // IPv4
		ethernetFrame([]byte{0xff}),                 // not an LLDPDU
		ethernetFrame(frame(t, mandatory(t)...)),
		ethernetFrame(frame(t, shut...)),
	}}
	got, err := winCapture(twoDevices(a, &fakeHandle{}), "Ethernet").Neighbors(context.Background())
	if err != nil {
		t.Fatalf("Neighbors: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %+v, want nothing after the shutdown", got)
	}
}

// Until ends the wait as soon as it is satisfied, instead of the window.
func TestWindowsCaptureStopsWhenUntilIsSatisfied(t *testing.T) {
	a := &fakeHandle{frames: [][]byte{ethernetFrame(frame(t, mandatory(t)...))}}
	c := winCapture(twoDevices(a, &fakeHandle{}), "")
	c.Window = 10 * time.Second
	c.Until = func(m map[string]Neighbor) bool { _, ok := m["Ethernet"]; return ok }

	start := time.Now()
	got, err := c.Neighbors(context.Background())
	if err != nil || len(got) != 1 {
		t.Fatalf("got %v, %v", got, err)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("waited %s; Until should have ended it", time.Since(start))
	}
}

func TestWindowsCaptureNamedInterfaceWithoutDeviceIsAnError(t *testing.T) {
	lib := &fakeLib{devs: []string{devA}, handles: map[string]*fakeHandle{devA: {}}}
	_, err := winCapture(lib, "Wi-Fi").Neighbors(context.Background())
	if err == nil || !strings.Contains(err.Error(), `no capture device for "Wi-Fi"`) {
		t.Errorf("err = %v", err)
	}
}

func TestWindowsCaptureUnknownInterfaceIsAnError(t *testing.T) {
	_, err := winCapture(twoDevices(&fakeHandle{}, &fakeHandle{}), "eth9").Neighbors(context.Background())
	if err == nil || !strings.Contains(err.Error(), `no interface named "eth9"`) {
		t.Errorf("err = %v", err)
	}
}

// When no capture could be opened at all, that is the answer; when some
// could, their neighbours come back alongside the first failure.
func TestWindowsCaptureOpenFailures(t *testing.T) {
	boom := errors.New("access denied")

	lib := twoDevices(&fakeHandle{}, &fakeHandle{})
	lib.openErr = map[string]error{devA: boom, devB: boom}
	if _, err := winCapture(lib, "").Neighbors(context.Background()); !errors.Is(err, boom) {
		t.Errorf("all failed: err = %v", err)
	}

	b := &fakeHandle{frames: [][]byte{ethernetFrame(frame(t, mandatory(t)...))}}
	lib = twoDevices(&fakeHandle{}, b)
	lib.openErr = map[string]error{devA: boom}
	got, err := winCapture(lib, "").Neighbors(context.Background())
	if !errors.Is(err, boom) || len(got) != 1 {
		t.Errorf("one failed: got %v, %v", got, err)
	}
}

func TestWindowsCaptureReadErrorIsReported(t *testing.T) {
	boom := errors.New("adapter removed")
	a := &fakeHandle{err: boom}
	_, err := winCapture(twoDevices(a, &fakeHandle{}), "Ethernet").Neighbors(context.Background())
	if !errors.Is(err, boom) {
		t.Errorf("err = %v", err)
	}
}

func TestWindowsCaptureCancelledContextIsReported(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := winCapture(twoDevices(&fakeHandle{}, &fakeHandle{}), "")
	if _, err := c.Neighbors(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v", err)
	}
}

// Each host-side failure is returned, not swallowed into "no neighbours".
func TestWindowsCaptureHostFailures(t *testing.T) {
	boom := errors.New("boom")
	base := func() npcapCalls {
		return npcapCalls{
			load:        func() (npcapLib, error) { return twoDevices(&fakeHandle{}, &fakeHandle{}), nil },
			interfaces:  func() ([]net.Interface, error) { return winInterfaces(), nil },
			adapterMACs: func() (map[string]net.HardwareAddr, error) { return winMACs(), nil },
		}
	}
	for name, mutate := range map[string]func(*npcapCalls){
		"interfaces":  func(s *npcapCalls) { s.interfaces = func() ([]net.Interface, error) { return nil, boom } },
		"load":        func(s *npcapCalls) { s.load = func() (npcapLib, error) { return nil, boom } },
		"adapterMACs": func(s *npcapCalls) { s.adapterMACs = func() (map[string]net.HardwareAddr, error) { return nil, boom } },
		"devices": func(s *npcapCalls) {
			s.load = func() (npcapLib, error) { return &fakeLib{devsErr: boom}, nil }
		},
	} {
		t.Run(name, func(t *testing.T) {
			s := base()
			mutate(&s)
			if _, err := (Capture{sys: &s}).Neighbors(context.Background()); !errors.Is(err, boom) {
				t.Errorf("err = %v", err)
			}
		})
	}
}

func TestWindowsSupported(t *testing.T) {
	if !Supported() {
		t.Error("Windows captures through Npcap")
	}
}

func TestCBytes(t *testing.T) {
	if got := cBytes([]byte{'a', 'b', 0, 'c'}); got != "ab" {
		t.Errorf("cBytes = %q", got)
	}
	if got := cBytes([]byte{'a', 'b'}); got != "ab" {
		t.Errorf("unterminated cBytes = %q", got)
	}
	if got := cString(nil); got != "" {
		t.Errorf("cString(nil) = %q", got)
	}
	b := []byte{'x', 'y', 0}
	if got := cString(&b[0]); got != "xy" {
		t.Errorf("cString = %q", got)
	}
}

// adapterMACs reads the real adapter table; every entry must be a GUID.
func TestAdapterMACsReadsTheHost(t *testing.T) {
	m, err := adapterMACs()
	if err != nil {
		t.Fatalf("adapterMACs: %v", err)
	}
	for guid := range m {
		if !strings.HasPrefix(guid, "{") {
			t.Errorf("key %q is not a GUID", guid)
		}
	}
}

// Without Npcap the error says what to install and is the typed
// "cannot capture here" answer; with it, the library loads and lists.
func TestLoadNpcapOnThisHost(t *testing.T) {
	lib, err := loadNpcap()
	if err != nil {
		if !errors.Is(err, ErrCaptureUnsupported) || !strings.Contains(err.Error(), "npcap.com") {
			t.Errorf("err = %v, want the typed not-installed answer", err)
		}
		return
	}
	defer lib.release()
	if _, err := lib.devices(); err != nil {
		t.Errorf("devices: %v", err)
	}
}
