package rollcall

import (
	"context"
	"fmt"
	"time"

	"dhs/internal/snell-rollcall/codec"
	"dhs/internal/snell-rollcall/session"
)

// What a slot number reaches depends on what the device enumerated, and
// assuming makes fourteen of a controller's fifteen nodes unreachable.
//
// A frame enumerates the ports of one unit: the cards in its slots. A router
// controller enumerates units, each a node in its own right — the matrices, the
// tielines engine, the panel driver that serves the routing interface — every
// one of them at port zero. Both arrive as the same message, and only the
// addresses inside tell them apart.
//
// So a slot is a position in that enumeration and its address is whatever the
// device put there. The neutral model wants slots numbered nought upwards
// without gaps, which is what a position gives; the protocol wants a full
// address, which is what the entry gives. Neither has to be guessed.

// nodeTable is a device's enumerated nodes, in the order it listed them.
type nodeTable struct {
	// addrs is the address of each slot, indexed by slot number.
	addrs []codec.Address

	// info keeps what the enumeration said about each node, so a slot read
	// does not have to ask again for something it was already told.
	info []codec.DeviceInfo
}

// nodes returns the device's node table, walking it once and keeping it.
//
// The walk is a device-list request against the gateway's own unit, which is
// what both kinds of device answer: a frame lists its ports, a controller lists
// the units it fronts. A proxy answers neither, and enumerate says what to do
// about that.
func (p *Plugin) nodes(ctx context.Context) (*nodeTable, error) {
	l, err := p.conn()
	if err != nil {
		return nil, err
	}

	p.mu.RLock()
	cached := p.nodeCache
	p.mu.RUnlock()
	if cached != nil {
		return cached, nil
	}

	list, err := p.enumerate(ctx, l)
	if err != nil {
		return nil, err
	}

	t := buildNodeTable(list, l.gateway)
	p.appendFarSide(ctx, t)
	p.appendFarPorts(ctx, l, t)

	p.mu.Lock()
	if p.nodeCache == nil {
		p.nodeCache = t
		p.log.Debug("rollcall: enumerated nodes", "count", len(t.addrs))
	} else {
		t = p.nodeCache
	}
	p.mu.Unlock()
	return t, nil
}

// enumerate asks a device for its nodes by whichever service answers.
//
// Two shapes exist and the difference is not cosmetic. A frame or a controller
// answers a port list: spec 7.6, the ports contained within a unit. A proxy or
// a bridge contains no ports at all — it advertises Map and nothing else, and
// spec 7.5 says a map server's node list is the map, which is the list of
// devices on its segment.
//
// The port list is tried first because it cannot be decided from the service
// flags. The vendor Centra advertises Map and not Ports, and answers a port
// list anyway with the fifteen units it fronts; selecting on the flag would
// regress every device that works today. A proxy, asked the same question,
// does not refuse — it says nothing at all and the caller waits out its own
// timeout, which is what made this look like an unreachable device rather than
// a question asked of the wrong service.
//
// Enumeration is cached for the life of the connection, so the fallback is
// paid once.
func (p *Plugin) enumerate(ctx context.Context, l *link) ([]codec.DeviceInfo, error) {
	// The probe gets half of whatever time is left, so that failing it leaves
	// enough to ask the other way. A proxy does not refuse a port list, it
	// ignores it, and an unbounded probe therefore spends the caller's whole
	// deadline discovering nothing — which is what made a reachable proxy look
	// like an unreachable device.
	//
	// A caller that set no deadline asked to wait, and is left to.
	probe, cancel := halfOf(ctx)
	defer cancel()

	list, err := p.Ports(probe, l.sess.RemoteAddress().Unit)
	if err == nil && len(list) > 0 {
		return list, nil
	}

	if !l.gateway.ID.Services.Has(codec.SvcMap) {
		if err != nil {
			return nil, err
		}
		return list, nil
	}

	p.log.Debug("rollcall: no port list; reading the map instead",
		"gateway", l.gateway.Address.Device().String(),
		"type", codec.UnitTypeName(l.gateway.ID.TypeID), "err", err)

	devs, mapErr := p.Devices(ctx)
	if mapErr != nil {
		// The port list is the primary question, so its failure is the one
		// worth reporting when neither service answers.
		if err != nil {
			return nil, err
		}
		return nil, mapErr
	}
	return devs, nil
}

// halfOf returns a context holding half the time left on its parent, and a
// cancel that must be called. A parent with no deadline is returned unchanged.
//
// The arithmetic is against the wall clock rather than the injected one. A
// context deadline is real time whatever a test has done to the clock the
// protocol runs on, and measuring one with the other makes the split silently
// not happen.
func halfOf(ctx context.Context) (context.Context, context.CancelFunc) {
	deadline, ok := ctx.Deadline()
	if !ok {
		return context.WithCancel(ctx)
	}
	left := time.Until(deadline)
	if left <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, left/2)
}

// appendFarSide adds what each bridge in the table can see.
//
// A bridge publishes the equipment behind it through the net service, and it
// fills in the route as it does so: measured against the vendor proxy, every
// entry comes back carrying its substitution address, net=1000, which is what
// spec 9.31 requires of a bridge. So the addresses are used exactly as given —
// a session opened on one of them reaches across, which is also measured.
//
// Many hops, with cycle protection. An rNet is four bridges deep (spec 5.1) and
// a proxy behind a proxy is exactly what the vendor RollCall IP Proxy presents:
// its map holds a virtual node whose far side holds another virtual node whose
// far side holds the frame. So a bridge found behind a bridge is descended too,
// as a queue rather than by recursion.
//
// The measured proxy also lists a far-side entry that routes straight back to
// the bridge it was found behind. Without protection that is an unbounded list —
// the same address appended and re-descended forever, which is what a live
// discovery through the proxy actually did. Two guards stop it: an address
// already in the table is the same node reached again and is not added, and a
// bridge already descended is not descended again. A route deeper than four hops
// has nowhere left to be forwarded and is left alone.
//
// A bridge that will not answer is absorbed rather than fatal: the near side of
// the network is still worth having, and a gateway whose downstream chassis is
// unplugged is a normal thing to meet — the proxy we measured has one of those
// too, and it answers with an empty list.
func (p *Plugin) appendFarSide(ctx context.Context, t *nodeTable) {
	// Everything already listed is known: a far-side entry repeating a known
	// address is the same node reached again, not a new one.
	known := make(map[codec.Address]bool, len(t.addrs))
	for _, a := range t.addrs {
		known[a.Device()] = true
	}

	// The bridges still to descend, seeded with every one the near side listed.
	// A far side may hold more, and those are queued as they are found. A bridge
	// is queued only when it is first added to the table, so the known set that
	// stops a node being added twice is also what stops one being descended twice:
	// a cycle cannot re-enqueue an address it has already reached.
	var queue []int
	for i := range t.info {
		if t.info[i].ID.Services.Has(codec.SvcNet) {
			queue = append(queue, i)
		}
	}

	for len(queue) > 0 {
		i := queue[0]
		queue = queue[1:]

		bridge := t.addrs[i]

		// A route fills from the top nibble down and holds four hops (spec 5.1).
		// A bridge deeper than that has nowhere left to forward to, so it is not
		// descended rather than chasing a route that cannot be expressed.
		if bridge.HopCount() >= 4 {
			continue
		}

		far, err := p.netDevices(ctx, bridge)
		if err != nil {
			p.fire(EventBridgeUnreadable, fmt.Sprintf(
				"%s offers the net service and would not list what is behind it: %v",
				bridge, err))
			continue
		}
		for _, d := range far {
			if d.Address == (codec.Address{}) {
				continue
			}
			dev := reachThrough(bridge, d.Address)
			if known[dev] {
				continue
			}
			known[dev] = true
			// The address a client uses is the reachable one, not the local one
			// the far segment knows the node by.
			d.Address = dev
			t.addrs = append(t.addrs, dev)
			t.info = append(t.info, d)
			if d.ID.Services.Has(codec.SvcNet) {
				// A bridge behind a bridge: queue it so its own far side is read.
				queue = append(queue, len(t.info)-1)
			}
		}
		p.log.Debug("rollcall: read past a bridge",
			"bridge", bridge.String(), "devices", len(far))
	}
}

// reachThrough composes the address that reaches a far-side device from here,
// where bridge is the node it was found behind. The transform is codec.Compose,
// shared with the proxy provider so both sides agree on the route (spec 5.3).
func reachThrough(bridge, far codec.Address) codec.Address {
	return bridge.Compose(far)
}

// appendFarPorts adds the cards of every frame reached through a bridge.
//
// A bridge's far side lists frames, not the cards inside them: the cards are
// ports of a frame (spec 7.6), reached through the port service the same way the
// connected gateway's own cards are. The connected gateway's cards are already
// enumerated before this runs, so only the frames behind a bridge — the ones
// carrying a route — are asked here.
//
// A frame reached this way was measured to return its cards by the address its
// own segment knows them by — port on unit 0x0C with no route — so each card's
// reachable address is the frame's own route and unit with the card's port. A
// frame that advertises the port service and then will not list its cards is
// kept as a node without them rather than lost.
func (p *Plugin) appendFarPorts(ctx context.Context, l *link, t *nodeTable) {
	known := make(map[codec.Address]bool, len(t.addrs))
	for _, a := range t.addrs {
		known[a.Device()] = true
	}

	// The frames to ask, snapshot before the table grows: a card added below is
	// not itself a frame whose ports are enumerated. A frame here is a node
	// reached through a bridge (a routed address), at port zero (a frame, not one
	// of its cards), that advertises the port service and is not itself a bridge.
	// The measured IQ gateway behind the proxy advertises the port service; a far
	// node that offers only the map service is not something measured here and is
	// left as a node without cards rather than guessed at.
	var frames []codec.Address
	for i := range t.info {
		a := t.addrs[i]
		s := t.info[i].ID.Services
		if a.Net != 0 && a.Port == 0 && s.Has(codec.SvcPorts) && !s.Has(codec.SvcNet) {
			frames = append(frames, a)
		}
	}

	for _, fr := range frames {
		cards, err := p.portsAt(ctx, l, fr)
		if err != nil {
			p.fire(EventFrameUnreadable, fmt.Sprintf(
				"%s is a frame reached through a bridge and would not list its cards: %v",
				fr, err))
			continue
		}
		for _, c := range cards {
			// A card is a port of the frame: the frame's own route and unit, the
			// card's port. The address the far segment gave it carries no route
			// and would not come back. Port zero is the frame itself rather than a
			// card — and an entry with no address at all lands here too, since its
			// port is zero — so it is not added a second time.
			card := codec.Address{Net: fr.Net, Unit: fr.Unit, Port: c.Address.Port, Index: codec.IndexUnknown}
			if card.Port == 0 || known[card] {
				continue
			}
			known[card] = true
			c.Address = card
			t.addrs = append(t.addrs, card)
			t.info = append(t.info, c)
		}
		p.log.Debug("rollcall: read a frame's cards through a bridge",
			"frame", fr.String(), "cards", len(cards))
	}
}

// portsAt lists the cards of one frame, on a port session opened to it.
//
// Unlike the connected gateway's port session, this one is opened to a specific
// node through the route its address carries, and closed when the list is read:
// a far frame is enumerated once during discovery, and a session left open is
// one the frame never reclaims.
func (p *Plugin) portsAt(ctx context.Context, l *link, node codec.Address) ([]codec.DeviceInfo, error) {
	s, err := session.Call(ctx, l.sess, node.Device(), codec.SvcPorts, codec.LevelSupervisor, p.identity())
	if err != nil {
		return nil, err
	}
	defer func() { _ = s.Close() }()

	return walkDeviceList(ctx, s, codec.MsgGetDevList, []byte{node.Unit, 0})
}

// netDevices lists what one bridge can see on its other side.
func (p *Plugin) netDevices(ctx context.Context, bridge codec.Address) ([]codec.DeviceInfo, error) {
	s, err := p.netSession(ctx, bridge)
	if err != nil {
		return nil, err
	}
	return walkDeviceList(ctx, s, codec.MsgGetLocDevMap, nil)
}

// walkDeviceList collects the device records a list or map walk returns. An item
// that came back as something other than a device record is skipped — a server
// may answer one packet of a block with an acknowledgement — and one that will
// not decode stops the walk, so a garbled far side is reported rather than half
// read.
func walkDeviceList(ctx context.Context, s *session.Session, msg codec.PacketType, payload []byte) ([]codec.DeviceInfo, error) {
	var out []codec.DeviceInfo
	err := session.Walk(ctx, s, msg, payload, func(_ int, f codec.Frame) error {
		if f.Type != codec.MsgRetDevInfo {
			return nil
		}
		d, err := codec.DecodeDeviceInfo(f.Payload)
		if err != nil {
			return err
		}
		out = append(out, d)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// buildNodeTable turns an enumeration into a slot table.
func buildNodeTable(list []codec.DeviceInfo, gateway codec.DeviceInfo) *nodeTable {
	t := &nodeTable{}

	if len(list) == 0 {
		// A device that enumerated nothing still has itself, and a caller
		// asking for slot zero must reach it rather than nothing.
		t.addrs = []codec.Address{gateway.Address.Device()}
		t.info = []codec.DeviceInfo{gateway}
		return t
	}

	for _, d := range list {
		t.addrs = append(t.addrs, d.Address.Device())
		t.info = append(t.info, d)
	}
	return t
}

// address returns the address that reaches a slot, and whether the enumeration
// named it.
func (t *nodeTable) address(slot int) (codec.Address, bool) {
	if slot < 0 || slot >= len(t.addrs) {
		return codec.Address{}, false
	}
	return t.addrs[slot], true
}

// slotAddress resolves a slot number to the address that reaches it.
//
// A slot past the end of the enumeration is still addressed rather than
// refused: a gateway ages a map entry out after sixty seconds of silence, so a
// node missing from the list is one that has gone quiet rather than one that
// never existed, and a caller that knows the port number is entitled to try.
func (p *Plugin) slotAddress(ctx context.Context, slot int) (codec.Address, error) {
	if slot < 0 || slot > 0xFF {
		return codec.Address{}, fmt.Errorf("rollcall: slot %d is outside the address range", slot)
	}

	l, err := p.conn()
	if err != nil {
		return codec.Address{}, err
	}
	fallback := l.sess.RemoteAddress().Device()
	fallback.Port = uint8(slot)

	t, err := p.nodes(ctx)
	if err != nil {
		// Enumeration is a convenience, not a precondition: a device that will
		// not list its nodes can still be addressed by the port number a
		// caller already knows.
		p.log.Debug("rollcall: could not enumerate nodes; addressing by port",
			"slot", slot, "err", err)
		return fallback, nil
	}

	addr, ok := t.address(slot)
	if !ok {
		p.fire(EventUnlistedNode, fmt.Sprintf(
			"slot %d is past the %d the device listed; addressing it as %s",
			slot, len(t.addrs), fallback))
		return fallback, nil
	}
	return addr, nil
}
