package rollcall

import (
	"context"
	"sync"
	"testing"
	"time"

	"dhs/internal/consumer"
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
		// Two near bridges, each publishing its own two devices — the realistic
		// case, where a bridge fills in the route it relays by, so what is behind
		// one carries a different address from what is behind the other.
		d.farByBridge = map[codec.Address][]codec.DeviceInfo{
			{Unit: gatewayAddr.Unit, Port: 0, Index: codec.IndexUnknown}: {
				farDevice(0x1000, 0x08, "Nucleus 2"),
				farDevice(0x1000, 0x11, "Matrix 1"),
			},
			{Unit: gatewayAddr.Unit, Port: 1, Index: codec.IndexUnknown}: {
				farDevice(0x2000, 0x08, "Nucleus 3"),
				farDevice(0x2000, 0x11, "Matrix 2"),
			},
		}
	})

	info, err := h.plugin.GetDeviceInfo(context.Background())
	if err != nil {
		t.Fatalf("GetDeviceInfo: %v", err)
	}
	// Two bridges, each publishing its own two devices behind it.
	if info.NumSlots != 2+2+2 {
		t.Fatalf("%d nodes, want the near side and both far sides", info.NumSlots)
	}
}

func TestASelfReferentialBridgeDoesNotLoop(t *testing.T) {
	// Measured against the vendor RollCall IP Proxy: a bridge's far side listed
	// an entry that routed straight back to itself, so descending it returned the
	// same list again. The old one-hop walk re-appended and re-descended it until
	// its deadline ran out — a live discovery through the proxy hung on exactly
	// this. The far device advertises the net service, which is what makes the
	// walk try to read past it in the first place.
	loop := farDevice(0x1000, 0x08, "Loop")
	loop.ID.Services |= codec.SvcNet
	h := newHarness(t, func(d *device) {
		d.ports = 1
		d.services |= codec.SvcNet
		d.farSide = []codec.DeviceInfo{loop}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan struct{})
	var info consumer.DeviceInfo
	var err error
	go func() {
		info, err = h.plugin.GetDeviceInfo(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the far-side walk did not terminate on a self-referential bridge")
	}
	if err != nil {
		t.Fatalf("GetDeviceInfo: %v", err)
	}
	// The near bridge and the one node behind it, counted once.
	if info.NumSlots != 2 {
		t.Errorf("%d nodes, want the near side and the looping node once", info.NumSlots)
	}
}

func TestABridgeBehindABridgeIsFollowed(t *testing.T) {
	// The vendor proxy nests bridges: its map holds a virtual node whose far side
	// holds another virtual node whose far side holds the frame. So a bridge
	// found behind a bridge is descended too, and the node at the bottom is
	// reached.
	mid := farDevice(0x1000, 0x01, "mid bridge")
	mid.ID.Services |= codec.SvcNet
	leaf := farDevice(0x1100, 0x0C, "the frame")
	h := newHarness(t, func(d *device) {
		d.ports = 1
		d.services |= codec.SvcNet
		d.farByBridge = map[codec.Address][]codec.DeviceInfo{
			// The near bridge (port 0 of the gateway) fronts the mid bridge.
			{Unit: gatewayAddr.Unit, Port: 0, Index: codec.IndexUnknown}: {mid},
			// Which in turn fronts the frame.
			{Net: 0x1000, Unit: 0x01, Index: codec.IndexUnknown}: {leaf},
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tbl, err := h.plugin.nodes(ctx)
	if err != nil {
		t.Fatalf("enumerate: %v", err)
	}
	var foundMid, foundLeaf bool
	for _, a := range tbl.addrs {
		if a.Net == 0x1000 && a.Unit == 0x01 {
			foundMid = true
		}
		if a.Net == 0x1100 && a.Unit == 0x0C {
			foundLeaf = true
		}
	}
	if !foundMid {
		t.Errorf("the mid bridge was not enumerated: %v", tbl.addrs)
	}
	if !foundLeaf {
		t.Errorf("the frame two hops away was not reached: %v", tbl.addrs)
	}
}

// frameCard is one card as a frame lists it: a port on the frame's own unit,
// with no route, the way the vendor proxy relayed the IQ frame's cards.
func frameCard(port uint8, typeID uint16, name string) codec.DeviceInfo {
	return codec.DeviceInfo{
		ProtocolVersion: codec.ProtocolVersion,
		Address:         codec.Address{Unit: 0x0C, Port: port, Index: codec.IndexUnknown},
		ID: codec.ID{
			Services: codec.SvcMenus | codec.SvcControl | codec.SvcFile,
			TypeID:   typeID,
			Name:     name,
		},
		Status: codec.UnitStatus{Status: codec.StatusPresent},
	}
}

func TestCardsBehindAFrameReachedThroughABridge(t *testing.T) {
	// The bridge's far side lists the frame, not the cards inside it. The cards
	// are ports of the frame, reached over the port service, and each is
	// addressable at the frame's own route and unit with the card's port — which
	// is how a client of the proxy walks a card two hops out.
	frame := farDevice(0x1000, 0x0C, "the frame")
	// A gateway: map and ports both, as the IQ frame's advertises.
	frame.ID.Services = codec.SvcMenus | codec.SvcControl | codec.SvcFile | codec.SvcMap | codec.SvcPorts
	h := newHarness(t, func(d *device) {
		d.ports = 1
		d.services |= codec.SvcNet
		d.farByBridge = map[codec.Address][]codec.DeviceInfo{
			{Unit: gatewayAddr.Unit, Port: 0, Index: codec.IndexUnknown}: {frame},
		}
		// Names are kept within the fixed field a card record carries. The last
		// two entries exercise the skips: a repeat of an earlier port, and the
		// frame itself answering at port zero.
		d.cardsByFrame = map[codec.Address][]codec.DeviceInfo{
			{Net: 0x1000, Unit: 0x0C, Index: codec.IndexUnknown}: {
				frameCard(0x01, 562, "EMB.06"),
				frameCard(0x03, 562, "EMB.07"),
				frameCard(0x01, 562, "EMB.06 dup"), // deduped
				frameCard(0x00, 429, "frame"),      // port zero, skipped
			},
		}
	})

	tbl, err := h.plugin.nodes(context.Background())
	if err != nil {
		t.Fatalf("enumerate: %v", err)
	}
	var cards []codec.Address
	for _, a := range tbl.addrs {
		if a.Net == 0x1000 && a.Unit == 0x0C && a.Port != 0 {
			cards = append(cards, a)
		}
	}
	// The two real cards, each once: the repeat, the port-zero entry and the
	// entry with no address are all left out.
	if len(cards) != 2 {
		t.Fatalf("got %d cards, want 2 (01 and 03): %v", len(cards), cards)
	}
	var one, three bool
	for _, a := range cards {
		if a.Port == 0x01 {
			one = true
		}
		if a.Port == 0x03 {
			three = true
		}
	}
	if !one || !three {
		t.Errorf("the frame's cards were not reached at its route: %v", cards)
	}
}

func TestAFrameThatWillNotListItsCards(t *testing.T) {
	// A frame that advertises the port service and then refuses a session for it
	// is kept as a node — a client still sees the frame — and the refusal is
	// counted rather than swallowed.
	frame := farDevice(0x1000, 0x0C, "a mute frame")
	frame.ID.Services = codec.SvcMenus | codec.SvcControl | codec.SvcMap | codec.SvcPorts
	h := newHarness(t, func(d *device) {
		d.ports = 1
		d.services |= codec.SvcNet
		d.farByBridge = map[codec.Address][]codec.DeviceInfo{
			{Unit: gatewayAddr.Unit, Port: 0, Index: codec.IndexUnknown}: {frame},
		}
		d.framePortsRefused = map[codec.Address]bool{
			{Net: 0x1000, Unit: 0x0C, Index: codec.IndexUnknown}: true,
		}
	})

	tbl, err := h.plugin.nodes(context.Background())
	if err != nil {
		t.Fatalf("enumerate: %v", err)
	}
	var frameThere bool
	for _, a := range tbl.addrs {
		if a.Net == 0x1000 && a.Unit == 0x0C && a.Port == 0 {
			frameThere = true
		}
		if a.Port != 0 {
			t.Errorf("a card was listed for a frame that refused its port list: %s", a)
		}
	}
	if !frameThere {
		t.Errorf("the frame itself was lost: %v", tbl.addrs)
	}
	if !hasEvent(h.plugin, EventFrameUnreadable) {
		t.Error("a frame that would not list its cards went unrecorded")
	}
}

func TestReachThrough(t *testing.T) {
	for _, tc := range []struct {
		name   string
		bridge codec.Address
		far    codec.Address
		want   codec.Address
	}{
		{
			// The vendor Centra substitutes the route itself; a far address that
			// already carries one is used exactly as given.
			name:   "an address that carries a route is left alone",
			bridge: codec.Address{Unit: 0x01, Index: codec.IndexUnknown},
			far:    codec.Address{Net: 0x1000, Unit: 0x08, Index: codec.IndexUnknown},
			want:   codec.Address{Net: 0x1000, Unit: 0x08, Index: codec.IndexUnknown},
		},
		{
			// The vendor proxy does not: a far address with no route is reached by
			// crossing the bridge, whose unit becomes the first hop.
			name:   "an unrouted address behind a near bridge",
			bridge: codec.Address{Unit: 0x01, Index: codec.IndexUnknown},
			far:    codec.Address{Unit: 0x0C, Index: codec.IndexUnknown},
			want:   codec.Address{Net: 0x1000, Unit: 0x0C, Index: codec.IndexUnknown},
		},
		{
			// Behind a bridge that is itself one hop away, the new hop goes in the
			// second nibble: the frame two hops back from the proxy.
			name:   "an unrouted address behind a bridge one hop away",
			bridge: codec.Address{Net: 0x1000, Unit: 0x01, Index: codec.IndexUnknown},
			far:    codec.Address{Unit: 0x0C, Index: codec.IndexUnknown},
			want:   codec.Address{Net: 0x1100, Unit: 0x0C, Index: codec.IndexUnknown},
		},
		{
			// A route already four hops deep has no room for another, so the far
			// address is left as given rather than shifted off the top.
			name:   "an unrouted address behind a bridge at the hop limit",
			bridge: codec.Address{Net: 0x1234, Unit: 0x05, Index: codec.IndexUnknown},
			far:    codec.Address{Unit: 0x0C, Index: codec.IndexUnknown},
			want:   codec.Address{Unit: 0x0C, Index: codec.IndexUnknown},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := reachThrough(tc.bridge, tc.far)
			if got != tc.want {
				t.Errorf("reachThrough(%s, %s) = %s, want %s", tc.bridge, tc.far, got, tc.want)
			}
		})
	}
}

func TestAnUnroutedFarSideGetsItsRouteComposed(t *testing.T) {
	// The vendor RollCall IP Proxy returns a far node by the address its own
	// segment knows it by, net zero, which collides with the bridge in front of
	// it. The route is composed here so the node is reachable and distinct.
	h := newHarness(t, func(d *device) {
		d.ports = 1
		d.services |= codec.SvcNet
		d.farByBridge = map[codec.Address][]codec.DeviceInfo{
			{Unit: gatewayAddr.Unit, Port: 0, Index: codec.IndexUnknown}: {
				farDevice(0x0000, 0x0C, "the frame"),
			},
		}
	})

	tbl, err := h.plugin.nodes(context.Background())
	if err != nil {
		t.Fatalf("enumerate: %v", err)
	}
	var found bool
	for _, a := range tbl.addrs {
		// The near bridge is unit 0x08, so crossing it puts 8 in the top nibble.
		if a.Net == 0x8000 && a.Unit == 0x0C {
			found = true
		}
	}
	if !found {
		t.Errorf("the unrouted far node was not given a reachable address: %v", tbl.addrs)
	}
}

func TestARouteAtTheHopLimitIsNotDescended(t *testing.T) {
	// A route is four bridges deep at most (spec 5.1). A bridge already that deep
	// is left alone rather than chased past the end of the address.
	deep := farDevice(0x1234, 0x05, "four hops out")
	deep.ID.Services |= codec.SvcNet
	behind := farDevice(0x1235, 0x06, "one hop too far")
	h := newHarness(t, func(d *device) {
		d.ports = 1
		d.services |= codec.SvcNet
		d.farByBridge = map[codec.Address][]codec.DeviceInfo{
			{Unit: gatewayAddr.Unit, Port: 0, Index: codec.IndexUnknown}: {deep},
			{Net: 0x1234, Unit: 0x05, Index: codec.IndexUnknown}:         {behind},
		}
	})

	tbl, err := h.plugin.nodes(context.Background())
	if err != nil {
		t.Fatalf("enumerate: %v", err)
	}
	var foundDeep, foundBehind bool
	for _, a := range tbl.addrs {
		if a.Net == 0x1234 && a.Unit == 0x05 {
			foundDeep = true
		}
		if a.Net == 0x1235 && a.Unit == 0x06 {
			foundBehind = true
		}
	}
	if !foundDeep {
		t.Errorf("the four-hop bridge was not enumerated: %v", tbl.addrs)
	}
	if foundBehind {
		t.Errorf("a bridge at the hop limit was descended anyway: %v", tbl.addrs)
	}
}

func TestTwoBridgesThatListEachOtherTerminate(t *testing.T) {
	// A cycle need not be a self-loop: two bridges each naming the other is the
	// same trap one hop wider. It must terminate with each counted once.
	a := farDevice(0x1000, 0x01, "bridge A")
	a.ID.Services |= codec.SvcNet
	b := farDevice(0x1000, 0x02, "bridge B")
	b.ID.Services |= codec.SvcNet
	h := newHarness(t, func(d *device) {
		d.ports = 1
		d.services |= codec.SvcNet
		d.farByBridge = map[codec.Address][]codec.DeviceInfo{
			{Unit: gatewayAddr.Unit, Port: 0, Index: codec.IndexUnknown}: {a},
			{Net: 0x1000, Unit: 0x01, Index: codec.IndexUnknown}:         {b},
			{Net: 0x1000, Unit: 0x02, Index: codec.IndexUnknown}:         {a},
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan struct{})
	var tbl *nodeTable
	var err error
	go func() {
		tbl, err = h.plugin.nodes(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("two bridges naming each other did not terminate")
	}
	if err != nil {
		t.Fatalf("enumerate: %v", err)
	}
	// The near bridge plus A and B, each once.
	if len(tbl.addrs) != 3 {
		t.Errorf("%d nodes, want the near side and the two bridges once each: %v", len(tbl.addrs), tbl.addrs)
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

func TestAFarUnitWithPortsButNoMapIsNotDescended(t *testing.T) {
	// A Centra matrix or card behind a bridge offers the port service — its
	// levels or its channels, 123 on a card — but no map. A direct connection
	// to the Centra never lists those as nodes, so neither does the far side:
	// only the segment's gateway, which offers map and ports both, is asked
	// for its ports.
	card := farDevice(0x3000, 0x41, "IP Slot 1: 5915")
	card.ID.Services = codec.SvcMenus | codec.SvcControl | codec.SvcFile | codec.SvcPorts
	h := newHarness(t, func(d *device) {
		d.ports = 1
		d.services |= codec.SvcNet
		d.farByBridge = map[codec.Address][]codec.DeviceInfo{
			{Unit: gatewayAddr.Unit, Port: 0, Index: codec.IndexUnknown}: {card},
		}
		// Were it asked, it would list a channel; it must not be asked.
		d.cardsByFrame = map[codec.Address][]codec.DeviceInfo{
			{Net: 0x3000, Unit: 0x41, Index: codec.IndexUnknown}: {frameCard(0x01, 623, "channel 1")},
		}
	})

	tbl, err := h.plugin.nodes(context.Background())
	if err != nil {
		t.Fatalf("enumerate: %v", err)
	}
	for _, a := range tbl.addrs {
		if a.Net == 0x3000 && a.Unit == 0x41 && a.Port != 0 {
			t.Errorf("a channel of a far card was listed as a node: %s", a)
		}
	}
	if !containsAddr(tbl.addrs, codec.Address{Net: 0x3000, Unit: 0x41, Index: codec.IndexUnknown}) {
		t.Error("the far card itself is not listed")
	}
	if hasEvent(h.plugin, EventFrameUnreadable) {
		t.Error("a node that was never asked for its ports was reported as unreadable")
	}
}

// containsAddr reports whether addrs holds a, ignoring the session index.
func containsAddr(addrs []codec.Address, a codec.Address) bool {
	for _, x := range addrs {
		if x.SameDevice(a) {
			return true
		}
	}
	return false
}
