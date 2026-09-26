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

// ethernetFrame wraps an LLDPDU in the Ethernet header BPF and Npcap hand
// over: LLDP multicast destination, any source, Ethertype 0x88CC.
func ethernetFrame(pdu []byte) []byte {
	h := []byte{0x01, 0x80, 0xc2, 0, 0, 0x0e, 0x02, 0, 0, 0, 0, 0x99, 0x88, 0xcc}
	return append(h, pdu...)
}

func TestEthernetPayload(t *testing.T) {
	pdu := frame(t, mandatory(t)...)
	got, ok := ethernetPayload(ethernetFrame(pdu))
	if !ok || string(got) != string(pdu) {
		t.Errorf("LLDP frame: ok=%v payload %x", ok, got)
	}
	for name, f := range map[string][]byte{
		"runt":   {0x01, 0x80},
		"IPv4":   append(ethernetFrame(nil)[:12], 0x08, 0x00),
		"802.1Q": append(ethernetFrame(nil)[:12], 0x81, 0x00, 0, 1, 0x88, 0xcc),
	} {
		if _, ok := ethernetPayload(f); ok {
			t.Errorf("%s: accepted as LLDP", name)
		}
	}
}

func TestCaptureTargets(t *testing.T) {
	mac := net.HardwareAddr{2, 0, 0, 0, 0, 1}
	ifs := []net.Interface{
		{Name: "en0", Flags: net.FlagUp, HardwareAddr: mac},
		{Name: "lo0", Flags: net.FlagUp | net.FlagLoopback},
		{Name: "en1", HardwareAddr: mac},   // down
		{Name: "utun0", Flags: net.FlagUp}, // no Ethernet address
		{Name: "en2", Flags: net.FlagUp, HardwareAddr: mac},
	}
	list := func() ([]net.Interface, error) { return ifs, nil }

	got, err := captureTargets(list, "")
	if err != nil || len(got) != 2 || got[0].Name != "en0" || got[1].Name != "en2" {
		t.Errorf("all: %v, %v", got, err)
	}
	// A named interface is used even when it would not be picked by
	// default: the operator asked for it.
	got, err = captureTargets(list, "en1")
	if err != nil || len(got) != 1 || got[0].Name != "en1" {
		t.Errorf("named: %v, %v", got, err)
	}
	if _, err := captureTargets(list, "eth9"); err == nil || !strings.Contains(err.Error(), `no interface named "eth9"`) {
		t.Errorf("unknown: %v", err)
	}
	boom := errors.New("boom")
	if _, err := captureTargets(func() ([]net.Interface, error) { return nil, boom }, ""); !errors.Is(err, boom) {
		t.Errorf("list failure: %v", err)
	}
}

// fakeCapture feeds frames, then reports read timeouts.
type fakeCapture struct {
	mu     sync.Mutex
	frames [][]byte
	err    error
	closed bool
}

func (f *fakeCapture) open(iface string) openCapture {
	return openCapture{
		iface: iface,
		next: func() ([][]byte, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			if len(f.frames) > 0 {
				out := f.frames
				f.frames = nil
				return out, nil
			}
			if f.err != nil {
				return nil, f.err
			}
			// A read timeout; a real one blocks for its tick.
			f.mu.Unlock()
			time.Sleep(time.Millisecond)
			f.mu.Lock()
			return nil, nil
		},
		close: func() {
			f.mu.Lock()
			f.closed = true
			f.mu.Unlock()
		},
	}
}

func TestListenMergesInterfacesAndClosesThem(t *testing.T) {
	a := &fakeCapture{frames: [][]byte{ethernetFrame(frame(t, mandatory(t)...)), {0x00}}}
	b := &fakeCapture{}
	got, err := listen(context.Background(), 50*time.Millisecond, nil,
		[]openCapture{a.open("en0"), b.open("en1")}, nil)
	if err != nil || len(got) != 1 || got["en0"].ChassisID == "" {
		t.Errorf("got %v, %v", got, err)
	}
	if !a.closed || !b.closed {
		t.Error("every capture must be closed")
	}
}

func TestListenHonoursShutdownAndSkipsBadPDUs(t *testing.T) {
	shut := append(mandatory(t)[:2:2], ttl(0))
	a := &fakeCapture{frames: [][]byte{
		ethernetFrame([]byte{0xff}),
		ethernetFrame(frame(t, mandatory(t)...)),
		ethernetFrame(frame(t, shut...)),
	}}
	got, err := listen(context.Background(), 50*time.Millisecond, nil, []openCapture{a.open("en0")}, nil)
	if err != nil || len(got) != 0 {
		t.Errorf("got %v, %v; want nothing after the shutdown", got, err)
	}
}

func TestListenStopsWhenUntilIsSatisfied(t *testing.T) {
	a := &fakeCapture{frames: [][]byte{ethernetFrame(frame(t, mandatory(t)...))}}
	b := &fakeCapture{}
	start := time.Now()
	got, err := listen(context.Background(), time.Hour,
		func(m map[string]Neighbor) bool { return len(m) == 1 },
		[]openCapture{a.open("en0"), b.open("en1")}, nil)
	if err != nil || len(got) != 1 {
		t.Fatalf("got %v, %v", got, err)
	}
	if time.Since(start) > 10*time.Second {
		t.Errorf("waited %s; Until should have ended it", time.Since(start))
	}
}

func TestListenErrors(t *testing.T) {
	boom := errors.New("boom")

	// Nothing opened: the open failure is the answer.
	if got, err := listen(context.Background(), time.Second, nil, nil, boom); got != nil || !errors.Is(err, boom) {
		t.Errorf("nothing opened: %v, %v", got, err)
	}

	// Some opened: their neighbours come back with the open failure.
	a := &fakeCapture{frames: [][]byte{ethernetFrame(frame(t, mandatory(t)...))}}
	if got, err := listen(context.Background(), 50*time.Millisecond, nil, []openCapture{a.open("en0")}, boom); len(got) != 1 || !errors.Is(err, boom) {
		t.Errorf("partly opened: %v, %v", got, err)
	}

	// A read failure names the interface and ends that reader.
	r := &fakeCapture{err: boom}
	if _, err := listen(context.Background(), time.Second, nil, []openCapture{r.open("en3")}, nil); !errors.Is(err, boom) || !strings.Contains(err.Error(), "en3") {
		t.Errorf("read failure: %v", err)
	}

	// Two failing readers: the first failure is kept.
	r1, r2 := &fakeCapture{err: boom}, &fakeCapture{err: boom}
	if _, err := listen(context.Background(), time.Second, nil, []openCapture{r1.open("x"), r2.open("y")}, nil); !errors.Is(err, boom) {
		t.Errorf("two failures: %v", err)
	}

	// A cancelled caller is told so, with what was heard.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := listen(ctx, time.Second, nil, []openCapture{(&fakeCapture{}).open("en0")}, nil); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: %v", err)
	}
}

func TestListenZeroWindowTakesTheDefault(t *testing.T) {
	a := &fakeCapture{frames: [][]byte{ethernetFrame(frame(t, mandatory(t)...))}}
	got, err := listen(context.Background(), 0, func(m map[string]Neighbor) bool { return len(m) == 1 },
		[]openCapture{a.open("en0")}, nil)
	if err != nil || len(got) != 1 {
		t.Errorf("got %v, %v", got, err)
	}
}

func TestCaptureInterfacesReadsTheHost(t *testing.T) {
	ifs, err := CaptureInterfaces("")
	if err != nil {
		t.Fatalf("CaptureInterfaces: %v", err)
	}
	for _, i := range ifs {
		if i.Flags&net.FlagLoopback != 0 {
			t.Errorf("%s is loopback", i.Name)
		}
	}
	if _, err := CaptureInterfaces("no-such-interface-0"); err == nil {
		t.Error("an unknown name must be an error")
	}
}
