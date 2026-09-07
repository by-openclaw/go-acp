package rollcall

import (
	"context"
	"fmt"

	"dhs/internal/snell-rollcall/codec"
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
// the units it fronts.
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

	list, err := p.Ports(ctx, l.sess.RemoteAddress().Unit)
	if err != nil {
		return nil, err
	}

	t := buildNodeTable(list, l.gateway)

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
