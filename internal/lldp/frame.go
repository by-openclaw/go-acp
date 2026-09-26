package lldp

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

// ethernetHeaderLen is destination MAC + source MAC + Ethertype.
const ethernetHeaderLen = 14

// defaultWindow is how long a capture listens when the caller gives no
// Window.
const defaultWindow = 2 * time.Second

// ethernetPayload returns the LLDPDU carried by a whole Ethernet frame, or
// false when the frame is not LLDP.
//
// macOS (BPF) and Windows (Npcap) hand over whole frames; Linux's SOCK_DGRAM
// socket has already stripped the header, so it never calls this. An 802.1Q
// tagged frame is not LLDP here: 802.1AB sends the LLDPDU untagged, and the
// capture filters on the untagged Ethertype.
func ethernetPayload(frame []byte) ([]byte, bool) {
	if len(frame) < ethernetHeaderLen {
		return nil, false
	}
	if binary.BigEndian.Uint16(frame[12:14]) != EtherType {
		return nil, false
	}
	return frame[ethernetHeaderLen:], true
}

// captureTargets is the list of interfaces a capture listens on.
//
// A named interface must exist: a typo would otherwise look exactly like an
// unplugged cable. With no name, every interface that is up, is not loopback
// and has an Ethernet address — the only ones a switch can be on the other
// end of.
func captureTargets(list func() ([]net.Interface, error), want string) ([]net.Interface, error) {
	ifs, err := list()
	if err != nil {
		return nil, fmt.Errorf("lldp: list interfaces: %w", err)
	}
	var out []net.Interface
	for _, i := range ifs {
		if want != "" {
			if i.Name == want {
				return []net.Interface{i}, nil
			}
			continue
		}
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 || len(i.HardwareAddr) != 6 {
			continue
		}
		out = append(out, i)
	}
	if want != "" {
		return nil, fmt.Errorf("lldp: no interface named %q on this host", want)
	}
	return out, nil
}

// view is what has been heard so far, shared by the readers of every
// interface a capture listens on at once.
type view struct {
	until func(map[string]Neighbor) bool

	mu   sync.Mutex
	m    map[string]Neighbor
	once sync.Once
	// done closes when until is satisfied, so every reader stops at once
	// rather than each waiting out the window.
	done chan struct{}
}

func newView(until func(map[string]Neighbor) bool) *view {
	return &view{until: until, m: map[string]Neighbor{}, done: make(chan struct{})}
}

// heard applies one received LLDPDU for iface.
//
// A frame that fails to decode is skipped, not fatal: one misbehaving switch
// must not hide the others. A shutdown LLDPDU removes the neighbour, because
// recording it would publish a switch port that is no longer attached.
func (v *view) heard(iface string, pdu []byte) {
	nb, err := Decode(pdu)
	if err != nil {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if nb.Shutdown() {
		delete(v.m, iface)
	} else {
		v.m[iface] = nb
	}
	if v.until != nil && v.until(copyNeighbors(v.m)) {
		v.once.Do(func() { close(v.done) })
	}
}

// snapshot is a copy of the view.
func (v *view) snapshot() map[string]Neighbor {
	v.mu.Lock()
	defer v.mu.Unlock()
	return copyNeighbors(v.m)
}

// openCapture is one interface's capture, already open.
type openCapture struct {
	iface string
	// next returns the whole Ethernet frames read since the last call —
	// none on a read timeout.
	next  func() ([][]byte, error)
	close func()
}

// listen reads every open capture until the window ends, ctx is cancelled or
// until is satisfied, and closes each one.
//
// openErr is the first failure to open a capture. It is the answer when
// nothing opened; otherwise it comes back alongside what the others heard,
// because one unusable adapter must not hide the switch on the next.
func listen(ctx context.Context, window time.Duration, until func(map[string]Neighbor) bool,
	caps []openCapture, openErr error) (map[string]Neighbor, error) {
	if len(caps) == 0 && openErr != nil {
		return nil, openErr
	}
	if window <= 0 {
		window = defaultWindow
	}
	runCtx, cancel := context.WithTimeout(ctx, window)
	defer cancel()

	v := newView(until)
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr = openErr
	)
	for _, c := range caps {
		wg.Add(1)
		go func(c openCapture) {
			defer wg.Done()
			defer c.close()
			for runCtx.Err() == nil {
				select {
				case <-v.done:
					return
				default:
				}
				frames, err := c.next()
				if err != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = fmt.Errorf("lldp: capture on %s: %w", c.iface, err)
					}
					mu.Unlock()
					return
				}
				for _, f := range frames {
					if pdu, ok := ethernetPayload(f); ok {
						v.heard(c.iface, pdu)
					}
				}
			}
		}(c)
	}
	wg.Wait()

	if errors.Is(ctx.Err(), context.Canceled) {
		return v.snapshot(), ctx.Err()
	}
	return v.snapshot(), firstErr
}

// CaptureInterfaces is the list of interfaces a Capture with this Iface
// listens on: the named one, or every interface that is up, is not loopback
// and has an Ethernet address. A caller uses it to report the interfaces
// where nothing was heard, not only those where something was.
func CaptureInterfaces(iface string) ([]net.Interface, error) {
	return captureTargets(net.Interfaces, iface)
}
