package rollcall

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"dhs/internal/snell-rollcall/codec"
	"dhs/internal/snell-rollcall/session"
)

// A slot number reaches whatever the device enumerated at that position. These
// cover both shapes: a frame that lists the ports of one unit, and a controller
// that lists units, which is what the vendor Centra does and what made fourteen
// of its fifteen nodes unreachable before.

func TestNodeTableFromAFrame(t *testing.T) {
	list := []codec.DeviceInfo{
		{Address: codec.Address{Unit: 8, Port: 0, Index: codec.IndexUnknown}},
		{Address: codec.Address{Unit: 8, Port: 1, Index: codec.IndexUnknown}},
		{Address: codec.Address{Unit: 8, Port: 2, Index: codec.IndexUnknown}},
	}
	tbl := buildNodeTable(list, codec.DeviceInfo{})

	if len(tbl.addrs) != 3 {
		t.Fatalf("%d slots, want 3", len(tbl.addrs))
	}
	for slot, want := range []uint8{0, 1, 2} {
		addr, ok := tbl.address(slot)
		if !ok {
			t.Fatalf("slot %d is missing", slot)
		}
		if addr.Unit != 8 || addr.Port != want {
			t.Errorf("slot %d = %s, want unit 08 port %02X", slot, addr, want)
		}
	}
}

func TestNodeTableFromAController(t *testing.T) {
	// The shape the vendor Centra enumerates: nodes at port zero of their own
	// units. Addressing these by port would send every one of them to the same
	// place.
	list := []codec.DeviceInfo{
		{Address: codec.Address{Unit: 0x08, Index: codec.IndexUnknown},
			ID: codec.ID{Name: "Nucleus 2"}},
		{Address: codec.Address{Unit: 0x11, Index: codec.IndexUnknown},
			ID: codec.ID{Name: "Matrix 1"}},
		{Address: codec.Address{Unit: 0x81, Index: codec.IndexUnknown},
			ID: codec.ID{Name: "XY Panel"}},
	}
	tbl := buildNodeTable(list, codec.DeviceInfo{})

	for slot, want := range []uint8{0x08, 0x11, 0x81} {
		addr, ok := tbl.address(slot)
		if !ok {
			t.Fatalf("slot %d is missing", slot)
		}
		if addr.Unit != want || addr.Port != 0 {
			t.Errorf("slot %d = %s, want unit %02X port 00", slot, addr, want)
		}
	}
	if tbl.info[2].ID.Name != "XY Panel" {
		t.Errorf("slot 2 described as %q", tbl.info[2].ID.Name)
	}
}

func TestNodeTableOfADeviceThatListsNothing(t *testing.T) {
	gateway := codec.DeviceInfo{
		Address: codec.Address{Unit: 4, Port: 7, Index: codec.IndexUnknown},
		ID:      codec.ID{Name: "alone"},
	}
	tbl := buildNodeTable(nil, gateway)

	// A device that enumerated nothing still has itself, and a caller asking
	// for slot zero must reach it rather than nothing.
	addr, ok := tbl.address(0)
	if !ok {
		t.Fatal("the device is not in its own table")
	}
	if addr.Unit != 4 || addr.Port != 7 {
		t.Errorf("slot 0 = %s, want the gateway's own address", addr)
	}
	if _, ok := tbl.address(1); ok {
		t.Error("a table of one has a second slot")
	}
	if _, ok := tbl.address(-1); ok {
		t.Error("a negative slot resolved")
	}
}

func TestSlotAddressUsesTheEnumeration(t *testing.T) {
	h := newHarness(t, func(d *device) { d.ports = 3 })

	for slot, want := range []uint8{0, 1, 2} {
		addr, err := h.plugin.slotAddress(context.Background(), slot)
		if err != nil {
			t.Fatalf("slot %d: %v", slot, err)
		}
		if addr.Port != want || addr.Unit != gatewayAddr.Unit {
			t.Errorf("slot %d = %s", slot, addr)
		}
	}
}

func TestSlotAddressPastTheEnumeration(t *testing.T) {
	h := newHarness(t, func(d *device) { d.ports = 2 })

	// A gateway ages a map entry out after sixty seconds of silence, so a slot
	// missing from the list is one that has gone quiet. A caller that knows
	// the port number is entitled to try it.
	addr, err := h.plugin.slotAddress(context.Background(), 9)
	if err != nil {
		t.Fatalf("slot 9: %v", err)
	}
	if addr.Port != 9 {
		t.Errorf("slot 9 = %s, want port 09", addr)
	}
	if !hasEvent(h.plugin, EventUnlistedNode) {
		t.Error("addressing an unlisted node should be recorded")
	}
}

func TestSlotAddressOutsideTheAddressRange(t *testing.T) {
	h := newHarness(t, nil)

	for _, slot := range []int{-1, 256} {
		if _, err := h.plugin.slotAddress(context.Background(), slot); err == nil {
			t.Errorf("slot %d should not be addressable", slot)
		}
	}
}

func TestSlotAddressWithoutAnEnumeration(t *testing.T) {
	// A device that will not list its nodes is still usable: the slot number
	// is taken as a port on the gateway's own unit, which is what it meant
	// before anything enumerated anything.
	h := newHarness(t, func(d *device) { d.refuse[codec.MsgGetDevList] = true })

	addr, err := h.plugin.slotAddress(context.Background(), 3)
	if err != nil {
		t.Fatalf("slot 3: %v", err)
	}
	if addr.Port != 3 || addr.Unit != gatewayAddr.Unit {
		t.Errorf("slot 3 = %s, want port 03 on the gateway", addr)
	}
}

func TestTheEnumerationIsWalkedOnce(t *testing.T) {
	h := newHarness(t, func(d *device) { d.ports = 3 })

	first, err := h.plugin.nodes(context.Background())
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := h.plugin.nodes(context.Background())
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	// Walking it again would cost a round trip per slot on every call, and the
	// list only changes when a unit comes or goes.
	if first != second {
		t.Error("the node table was rebuilt")
	}
}

func TestTheMapSessionIsSeparateAndReused(t *testing.T) {
	h := newHarness(t, func(d *device) { d.ports = 2 })

	first, err := h.plugin.mapSession(context.Background())
	if err != nil {
		t.Fatalf("map session: %v", err)
	}
	second, err := h.plugin.mapSession(context.Background())
	if err != nil {
		t.Fatalf("map session again: %v", err)
	}
	if first != second {
		t.Error("a second map session was opened")
	}

	// It is not the control session: services are negotiated together and a
	// unit answers a request outside its session's services with InvSess.
	control, err := h.plugin.session(context.Background(), 0)
	if err != nil {
		t.Fatalf("control session: %v", err)
	}
	if control == first {
		t.Error("the map walk shares the control session")
	}
	if !first.Services().Has(codec.SvcMap) {
		t.Errorf("the map session negotiated %s", first.Services())
	}
	// Asking for the map service alone: the long-string bit buys nothing on a
	// fixed-width structure, and the vendor Centra refuses the pair.
	if first.Services().LongStrings() {
		t.Errorf("the map session asked for long strings: %s", first.Services())
	}
}

func TestAGatewayThatRefusesAMapSession(t *testing.T) {
	h := newHarness(t, func(d *device) { d.refuseMap = true })

	// Some units answer a list on any session, so the walk is worth attempting
	// on the control one rather than reporting a device with no nodes.
	s, err := h.plugin.mapSession(context.Background())
	if err != nil {
		t.Fatalf("map session: %v", err)
	}
	if s.Services().Has(codec.SvcMap) {
		t.Error("the fallback session claimed the map service")
	}

	if _, err := h.plugin.nodes(context.Background()); err != nil {
		t.Errorf("enumeration should still have worked: %v", err)
	}
}

func TestAGatewayWithNoMapServiceUsesTheControlSession(t *testing.T) {
	h := newHarness(t, func(d *device) {
		d.services = codec.SvcMenus | codec.SvcControl | codec.SvcLongStr
	})

	s, err := h.plugin.mapSession(context.Background())
	if err != nil {
		t.Fatalf("map session: %v", err)
	}
	control, err := h.plugin.session(context.Background(), 0)
	if err != nil {
		t.Fatalf("control session: %v", err)
	}
	if s != control {
		t.Error("a gateway with no map service should be asked on its control session")
	}
}

func TestServicesComeFromTheNodeThatAdvertisedThem(t *testing.T) {
	// Each node advertises for itself, and they differ: on the vendor Centra
	// the matrices offer Ports and the panel node does not. What the
	// enumeration said about a node beats what the gateway said about itself.
	h := newHarness(t, func(d *device) { d.ports = 2 })

	if _, err := h.plugin.nodes(context.Background()); err != nil {
		t.Fatalf("enumerate: %v", err)
	}

	h.plugin.mu.RLock()
	l := h.plugin.link
	h.plugin.mu.RUnlock()

	listed := codec.Address{Unit: gatewayAddr.Unit, Port: 1, Index: codec.IndexUnknown}
	if got := h.plugin.advertisedBy(listed, l); got == 0 {
		t.Error("a listed node has no advertised services")
	}

	// A node nothing described falls back to what the gateway advertised,
	// which is the only statement anybody has made about it.
	unlisted := codec.Address{Unit: 0x77, Index: codec.IndexUnknown}
	if got := h.plugin.advertisedBy(unlisted, l); got != l.gateway.ID.Services {
		t.Errorf("an unlisted node advertised %s, want the gateway's %s",
			got, l.gateway.ID.Services)
	}
}

func TestEnumerationWithoutAConnection(t *testing.T) {
	p := New(testDeps())

	if _, err := p.nodes(context.Background()); err == nil {
		t.Error("enumerating without a connection should fail")
	}
	if _, err := p.mapSession(context.Background()); err == nil {
		t.Error("opening a map session without a connection should fail")
	}
	if _, err := p.slotAddress(context.Background(), 1); err == nil {
		t.Error("resolving a slot without a connection should fail")
	}
}

func TestTheLinkClosesWhileTheMapSessionIsOpening(t *testing.T) {
	gate := make(chan struct{})
	h := newHarness(t, func(d *device) { d.gateCall = gate })

	done := make(chan error, 1)
	go func() {
		_, err := h.plugin.mapSession(context.Background())
		done <- err
	}()

	waitForCalls(t, h, 1)

	// Finished with, without closing the socket, so the call still completes
	// and the answer still arrives. The session the peer just granted has to
	// be closed rather than handed out.
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
			t.Error("a map session granted after the link finished must not be handed out")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the caller was left waiting")
	}
}

func TestConcurrentFileSessionsKeepOne(t *testing.T) {
	gate := make(chan struct{})
	h := newHarness(t, nil)

	// Enumeration first: resolving the slot walks the node list, and that
	// walk's own call would otherwise be one of the racing pair.
	if _, err := h.plugin.nodes(context.Background()); err != nil {
		t.Fatalf("enumerate: %v", err)
	}
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
			s, err := h.plugin.fileSession(context.Background(), 1)
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

	// A file service costs a session like any other, and a unit that runs out
	// stops answering anybody: the loser closes its own and takes the winner's.
	if results[0] != results[1] {
		t.Error("two callers were given two file sessions for one slot")
	}
}

// A frame and a controller both answer a port list. A proxy answers neither:
// it contains no ports at all, advertises Map and nothing else, and says
// nothing whatever to a port request rather than refusing it. These three
// cover which question gets asked of which device.

func TestEnumerationPrefersThePortList(t *testing.T) {
	// The vendor Centra advertises Map and not Ports, and answers a port list
	// anyway with the units it fronts. Choosing on the advertised flag would
	// stop asking the question that works, so the port list is tried first and
	// its answer is kept.
	h := newHarness(t, func(d *device) { d.ports = 3 })

	info, err := h.plugin.GetDeviceInfo(context.Background())
	if err != nil {
		t.Fatalf("GetDeviceInfo: %v", err)
	}
	if info.NumSlots != 3 {
		t.Errorf("%d nodes, want the three the port list named", info.NumSlots)
	}
}

func TestEnumerationFallsBackToTheMap(t *testing.T) {
	// The vendor RollCall IP Proxy answers a device enquiry and then says
	// nothing at all to a port list, because it holds no ports — it aggregates
	// whole frames under subnets and publishes them through its map. Refusing
	// stands in for that silence: the point is that the caller gets the nodes
	// rather than a timeout.
	h := newHarness(t, func(d *device) { d.refuse[codec.MsgGetDevList] = true })

	info, err := h.plugin.GetDeviceInfo(context.Background())
	if err != nil {
		t.Fatalf("a device with a map should enumerate through it: %v", err)
	}
	if info.NumSlots == 0 {
		t.Error("the map named no nodes")
	}
}

func TestEnumerationFailsWhenNeitherServiceAnswers(t *testing.T) {
	// With both ways shut the caller has to hear about it. Reporting the port
	// list's failure is deliberate: it is the question that was asked first.
	h := newHarness(t, func(d *device) {
		d.refuse[codec.MsgGetDevList] = true
		d.refuse[codec.MsgGetLocDevMap] = true
	})

	if _, err := h.plugin.GetDeviceInfo(context.Background()); err == nil {
		t.Error("a device that answers neither enumeration should be an error")
	}
}

func TestEnumerationWithoutAMapServiceKeepsThePortListError(t *testing.T) {
	// Nothing to fall back to, so the original failure stands rather than
	// being replaced by a complaint about a service the device never had.
	h := newHarness(t, func(d *device) {
		d.services &^= codec.SvcMap
		d.refuse[codec.MsgGetDevList] = true
	})

	_, err := h.plugin.GetDeviceInfo(context.Background())
	if err == nil {
		t.Fatal("a device with neither service should be an error")
	}
	if !strings.Contains(err.Error(), "port list") {
		t.Errorf("error = %v, want the port list's own failure", err)
	}
}

func TestEnumerationReportsTheMapWhenThePortListWasMerelyEmpty(t *testing.T) {
	// An empty port list is not a failure, so there is no earlier error to
	// prefer: what the caller hears about is the map's own refusal.
	h := newHarness(t, func(d *device) {
		d.ports = 0
		d.refuse[codec.MsgGetLocDevMap] = true
	})

	_, err := h.plugin.GetDeviceInfo(context.Background())
	if err == nil {
		t.Fatal("neither service answered, so this should be an error")
	}
	if !strings.Contains(err.Error(), "device map") {
		t.Errorf("error = %v, want the map's own failure", err)
	}
}

func TestEnumerationOfADeviceThatListsNothingAndHasNoMap(t *testing.T) {
	// Nothing named itself and there is nowhere else to ask. That is an empty
	// device rather than a broken one, and the gateway still stands for
	// itself so slot zero reaches something.
	h := newHarness(t, func(d *device) {
		d.ports = 0
		d.services &^= codec.SvcMap
	})

	info, err := h.plugin.GetDeviceInfo(context.Background())
	if err != nil {
		t.Fatalf("GetDeviceInfo: %v", err)
	}
	if info.NumSlots != 1 {
		t.Errorf("%d nodes, want the gateway standing for itself", info.NumSlots)
	}
}

func TestEnumerationLeavesTimeToAskTheOtherWay(t *testing.T) {
	// The vendor proxy does not refuse a port list, it ignores one. An
	// unbounded probe therefore spends the caller's whole deadline learning
	// nothing, and the fallback inherits a context that is already dead —
	// which is how a reachable proxy came to look like an unreachable device.
	h := newHarness(t, func(d *device) { d.silent[codec.MsgGetDevList] = true })

	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()

	info, err := h.plugin.GetDeviceInfo(ctx)
	if err != nil {
		t.Fatalf("the map should still have been read: %v", err)
	}
	if info.NumSlots == 0 {
		t.Error("the map named no nodes")
	}
}

func TestHalfOfADeadlineThatHasNoTimeLeft(t *testing.T) {
	// Nothing to divide. The parent is handed back so the caller fails on the
	// parent's own terms rather than on an arithmetic accident.
	deadline := time.Now()
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()

	got, cancel2 := halfOf(ctx, deadline.Add(time.Second))
	defer cancel2()

	if d, ok := got.Deadline(); !ok || !d.Equal(deadline) {
		t.Errorf("deadline = %v (set %v), want the parent's own", d, ok)
	}
}
