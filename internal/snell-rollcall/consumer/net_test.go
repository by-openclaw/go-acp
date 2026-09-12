package rollcall

import (
	"context"
	"sync"
	"testing"
	"time"

	"dhs/internal/snell-rollcall/codec"
	"dhs/internal/snell-rollcall/session"
)

// A bridge publishes the equipment behind it through the net service, and it
// fills in the route as it relays. Measured against the vendor RollCall IP
// Proxy: its net list came back with every address carrying net=1000, the
// substitution address, and a session opened on one of those reached the
// Centra's controller on the far side.

func farDevice(net uint16, unit uint8, name string) codec.DeviceInfo {
	return codec.DeviceInfo{
		ProtocolVersion: codec.ProtocolVersion,
		Address:         codec.Address{Net: net, Unit: unit, Index: codec.IndexUnknown},
		ID: codec.ID{
			Services: codec.SvcMenus | codec.SvcControl,
			TypeID:   623,
			Name:     name,
		},
		Status: codec.UnitStatus{Status: codec.StatusPresent},
	}
}

func TestWhatIsBehindABridgeIsEnumerated(t *testing.T) {
	h := newHarness(t, func(d *device) {
		d.ports = 2
		d.services |= codec.SvcNet
		d.farSide = []codec.DeviceInfo{
			farDevice(0x1000, 0x08, "Nucleus 2"),
			farDevice(0x1000, 0x11, "Matrix 1"),
		}
	})

	info, err := h.plugin.GetDeviceInfo(context.Background())
	if err != nil {
		t.Fatalf("GetDeviceInfo: %v", err)
	}
	// Two bridges, each publishing the same two devices behind it.
	if info.NumSlots != 2+2*2 {
		t.Fatalf("%d nodes, want the near side and both far sides", info.NumSlots)
	}
}

func TestAFarSideAddressKeepsItsRoute(t *testing.T) {
	// The route is what makes the address reachable, and it came from the
	// bridge. Dropping it would leave a node nothing can open.
	h := newHarness(t, func(d *device) {
		d.ports = 1
		d.services |= codec.SvcNet
		d.farSide = []codec.DeviceInfo{farDevice(0x1000, 0x08, "Nucleus 2")}
	})
	ctx := context.Background()

	tbl, err := h.plugin.nodes(ctx)
	if err != nil {
		t.Fatalf("enumerate: %v", err)
	}

	var found bool
	for _, a := range tbl.addrs {
		if a.Net == 0x1000 && a.Unit == 0x08 {
			found = true
		}
	}
	if !found {
		t.Errorf("no routed address in %v", tbl.addrs)
	}
}

func TestABridgeThatWillNotListWhatIsBehindIt(t *testing.T) {
	// The near side is still worth having. A gateway whose downstream chassis
	// is unplugged is a normal thing to meet — the proxy we measured has one.
	h := newHarness(t, func(d *device) {
		d.ports = 2
		d.services |= codec.SvcNet
		d.refuseNetList = true
	})

	info, err := h.plugin.GetDeviceInfo(context.Background())
	if err != nil {
		t.Fatalf("a bridge that will not answer must not lose the near side: %v", err)
	}
	if info.NumSlots != 2 {
		t.Errorf("%d nodes, want the near side", info.NumSlots)
	}
	if !hasEvent(h.plugin, EventBridgeUnreadable) {
		t.Error("a bridge that would not list its far side went unrecorded")
	}
}

func TestANodeThatIsNotABridgeIsNotAsked(t *testing.T) {
	h := newHarness(t, func(d *device) { d.ports = 2 })

	info, err := h.plugin.GetDeviceInfo(context.Background())
	if err != nil {
		t.Fatalf("GetDeviceInfo: %v", err)
	}
	if info.NumSlots != 2 {
		t.Errorf("%d nodes; a device with no net service has no far side", info.NumSlots)
	}
}

func TestAFarSideEntryWithNoAddressIsSkipped(t *testing.T) {
	// A list entry that decoded to nothing names no node, and adding it would
	// give a caller a slot that cannot be opened.
	h := newHarness(t, func(d *device) {
		d.ports = 1
		d.services |= codec.SvcNet
		d.farSide = []codec.DeviceInfo{{ProtocolVersion: codec.ProtocolVersion}}
	})

	info, err := h.plugin.GetDeviceInfo(context.Background())
	if err != nil {
		t.Fatalf("GetDeviceInfo: %v", err)
	}
	if info.NumSlots != 1 {
		t.Errorf("%d nodes, want only the near side", info.NumSlots)
	}
}

func TestOneNetSessionPerBridge(t *testing.T) {
	// Opening one costs a round trip and closing one is what a unit runs out
	// of, so a second enumeration reuses it.
	h := newHarness(t, func(d *device) {
		d.ports = 1
		d.services |= codec.SvcNet
		d.farSide = []codec.DeviceInfo{farDevice(0x1000, 0x08, "Nucleus 2")}
	})
	ctx := context.Background()

	bridge := codec.Address{Unit: gatewayAddr.Unit, Index: codec.IndexUnknown}
	first, err := h.plugin.netSession(ctx, bridge)
	if err != nil {
		t.Fatalf("netSession: %v", err)
	}
	second, err := h.plugin.netSession(ctx, bridge)
	if err != nil {
		t.Fatalf("netSession again: %v", err)
	}
	if first != second {
		t.Error("a second net session was opened on one bridge")
	}
}

func TestANetSessionWithoutAConnection(t *testing.T) {
	p := New(testDeps())
	if _, err := p.netSession(context.Background(), gatewayAddr); err == nil {
		t.Error("there is no link to open a net session on")
	}
}

func TestABridgeThatRefusesANetSession(t *testing.T) {
	// It advertises the service and will not open it, which is a deviation
	// worth counting rather than a reason to lose the near side.
	h := newHarness(t, func(d *device) {
		d.ports = 2
		d.services |= codec.SvcNet
		d.refuseNet = true
	})

	info, err := h.plugin.GetDeviceInfo(context.Background())
	if err != nil {
		t.Fatalf("GetDeviceInfo: %v", err)
	}
	if info.NumSlots != 2 {
		t.Errorf("%d nodes, want the near side", info.NumSlots)
	}
	if !hasEvent(h.plugin, EventBridgeUnreadable) {
		t.Error("a refused net session went unrecorded")
	}
}

func TestANetListItemThatIsNotADevice(t *testing.T) {
	// A server may answer one item of a block with something else. Skip it
	// rather than abandon the rest of the far side.
	h := newHarness(t, func(d *device) {
		d.ports = 1
		d.services |= codec.SvcNet
		d.farSide = []codec.DeviceInfo{
			farDevice(0x1000, 0x08, "Nucleus 2"),
			farDevice(0x1000, 0x11, "Matrix 1"),
		}
		d.oddNetItem = 0
	})

	info, err := h.plugin.GetDeviceInfo(context.Background())
	if err != nil {
		t.Fatalf("GetDeviceInfo: %v", err)
	}
	if info.NumSlots != 2 {
		t.Errorf("%d nodes, want the near side and the one entry that decoded", info.NumSlots)
	}
}

func TestANetListEntryThatWillNotDecode(t *testing.T) {
	// A garbled entry is a fault in the answer, and the far side is abandoned
	// rather than half reported. The near side survives and it is counted.
	h := newHarness(t, func(d *device) {
		d.ports = 1
		d.services |= codec.SvcNet
		d.farSide = []codec.DeviceInfo{farDevice(0x1000, 0x08, "Nucleus 2")}
		d.badNetEntry = true
	})

	info, err := h.plugin.GetDeviceInfo(context.Background())
	if err != nil {
		t.Fatalf("GetDeviceInfo: %v", err)
	}
	if info.NumSlots != 1 {
		t.Errorf("%d nodes, want the near side", info.NumSlots)
	}
	if !hasEvent(h.plugin, EventBridgeUnreadable) {
		t.Error("a garbled net list went unrecorded")
	}
}

func TestANetSessionOnALinkAlreadyFinishedWith(t *testing.T) {
	h := newHarness(t, func(d *device) { d.services |= codec.SvcNet })

	h.plugin.mu.RLock()
	l := h.plugin.link
	h.plugin.mu.RUnlock()
	l.mu.Lock()
	l.closed = true
	l.mu.Unlock()

	if _, err := h.plugin.netSession(context.Background(), gatewayAddr); err == nil {
		t.Error("a link that has been finished with must not open a session")
	}
}

func TestTheLinkClosesWhileANetSessionIsOpening(t *testing.T) {
	gate := make(chan struct{})
	h := newHarness(t, func(d *device) {
		d.services |= codec.SvcNet
		d.gateCall = gate
	})

	done := make(chan error, 1)
	go func() {
		_, err := h.plugin.netSession(context.Background(), gatewayAddr)
		done <- err
	}()

	waitForCalls(t, h, 1)

	h.plugin.mu.RLock()
	l := h.plugin.link
	h.plugin.mu.RUnlock()
	l.mu.Lock()
	l.closed = true
	l.mu.Unlock()

	close(gate)

	select {
	case err := <-done:
		if err == nil {
			t.Error("a net session granted after the link finished must not be handed out")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the caller was left waiting")
	}
}

func TestConcurrentNetSessionsKeepOne(t *testing.T) {
	gate := make(chan struct{})
	h := newHarness(t, func(d *device) { d.services |= codec.SvcNet })

	h.device.mu.Lock()
	h.device.gateCall = gate
	h.device.callsSeen = 0
	h.device.mu.Unlock()

	var wg sync.WaitGroup
	results := make([]*session.Session, 2)
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := h.plugin.netSession(context.Background(), gatewayAddr)
			if err != nil {
				t.Errorf("caller %d: %v", i, err)
				return
			}
			results[i] = s
		}()
	}

	waitForCalls(t, h, 2)
	close(gate)
	wg.Wait()

	if results[0] == nil || results[1] == nil {
		t.Fatal("both callers should have a session")
	}
	if results[0] != results[1] {
		t.Error("two callers got two net sessions on one bridge; one is leaked")
	}
}
