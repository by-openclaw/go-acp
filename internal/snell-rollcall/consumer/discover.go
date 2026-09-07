package rollcall

import (
	"context"
	"fmt"

	"dhs/internal/consumer"
	"dhs/internal/snell-rollcall/codec"
	"dhs/internal/snell-rollcall/session"
)

// GetDeviceInfo returns what the gateway said about itself during the
// handshake, plus how many ports it has.
//
// The port count is what the neutral model calls the slot count, and it costs
// a device-list walk to learn, so it is counted once and kept.
func (p *Plugin) GetDeviceInfo(ctx context.Context) (consumer.DeviceInfo, error) {
	l, err := p.conn()
	if err != nil {
		return consumer.DeviceInfo{}, err
	}

	slots, err := p.slotCount(ctx, l)
	if err != nil {
		return consumer.DeviceInfo{}, err
	}

	return consumer.DeviceInfo{
		IP:              p.addr,
		Port:            p.port,
		NumSlots:        slots,
		ProtocolVersion: int(l.gateway.ProtocolVersion),
	}, nil
}

// Devices returns every unit the gateway knows about.
//
// A RollCall gateway keeps a map of what it has heard announce itself, and
// walking that map is discovery. The map ages entries out after sixty seconds
// without an announcement, so a unit missing from it is a unit that has gone
// quiet rather than one that never existed.
func (p *Plugin) Devices(ctx context.Context) ([]codec.DeviceInfo, error) {
	l, err := p.conn()
	if err != nil {
		return nil, err
	}

	// The map service has its own session, and it is addressed to the gateway
	// directly rather than through the slot table: the table is built from
	// this very walk, and asking it for an address here is how a lookup
	// becomes a loop.
	s, err := p.mapSession(ctx)
	if err != nil {
		return nil, err
	}

	var out []codec.DeviceInfo
	err = session.Walk(ctx, s, codec.MsgGetLocDevMap, nil, func(_ int, f codec.Frame) error {
		if f.Type != codec.MsgRetDevInfo {
			return nil
		}
		info, err := codec.DecodeDeviceInfo(f.Payload)
		if err != nil {
			return err
		}
		out = append(out, info)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("rollcall: device map: %w", err)
	}

	if len(out) == 0 {
		// A gateway with no map still has itself.
		out = append(out, l.gateway)
	}
	return out, nil
}

// Ports returns what one unit enumerates: the cards in a frame's slots, or on
// a controller the units it fronts.
func (p *Plugin) Ports(ctx context.Context, unit uint8) ([]codec.DeviceInfo, error) {
	l, err := p.conn()
	if err != nil {
		return nil, err
	}

	// The map session again, for the same reasons as the walk above.
	s, err := p.mapSession(ctx)
	if err != nil {
		return nil, err
	}
	_ = l

	var out []codec.DeviceInfo
	err = session.Walk(ctx, s, codec.MsgGetDevList, []byte{unit, 0},
		func(_ int, f codec.Frame) error {
			if f.Type != codec.MsgRetDevInfo {
				return nil
			}
			info, err := codec.DecodeDeviceInfo(f.Payload)
			if err != nil {
				return err
			}
			out = append(out, info)
			return nil
		})
	if err != nil {
		return nil, fmt.Errorf("rollcall: port list for unit %02X: %w", unit, err)
	}
	return out, nil
}

// slotCount returns how many ports the gateway has.
//
// It takes the link rather than looking it up again: its only caller has just
// done that, and checking twice would leave a branch that cannot be reached
// except by a disconnection landing in the gap between them.
func (p *Plugin) slotCount(ctx context.Context, l *link) (int, error) {
	t, err := p.nodes(ctx)
	if err != nil {
		return 0, err
	}
	_ = l
	return len(t.addrs), nil
}

// GetSlotInfo returns the identity and state of one port.
//
// A port's identity is the card in it: its type id names the product, and its
// version plus that id is what identifies a device model, which is why both
// are reported rather than only the name.
func (p *Plugin) GetSlotInfo(ctx context.Context, slot int) (consumer.SlotInfo, error) {
	if slot < 0 || slot > 0xFF {
		return consumer.SlotInfo{}, fmt.Errorf("rollcall: slot %d is outside the port range", slot)
	}

	s, err := p.session(ctx, slot)
	if err != nil {
		return consumer.SlotInfo{}, err
	}

	idReply, err := s.Do(ctx, codec.MsgGetID, nil)
	if err != nil {
		return consumer.SlotInfo{}, err
	}
	id, err := codec.DecodeID(idReply.Payload)
	if err != nil {
		return consumer.SlotInfo{}, fmt.Errorf("rollcall: slot %d identity: %w", slot, err)
	}

	statReply, err := s.Do(ctx, codec.MsgGetStat, nil)
	if err != nil {
		return consumer.SlotInfo{}, err
	}
	st, err := codec.DecodeUnitStatus(statReply.Payload)
	if err != nil {
		return consumer.SlotInfo{}, fmt.Errorf("rollcall: slot %d status: %w", slot, err)
	}

	info := consumer.SlotInfo{
		Slot:     slot,
		Status:   slotStatus(st.Status),
		State:    slotState(st.Status),
		IsOnline: st.Status.Has(codec.StatusPresent),
		LiveAt:   p.clk.Now(),
		Identity: map[string]string{
			// The address is the node itself. A slot number is a position in
			// the device's own enumeration rather than a place in a frame, so
			// on a controller it names nothing on its own: the routing
			// interface is slot 14 on one model and slot 5 on another, and
			// only the address says which node was reached. It costs nothing
			// to report — the session already knows who it is talking to.
			// The session index is dropped: it is an artefact of this
			// conversation rather than of the node, it differs on every
			// reconnection, and leaving it in would make two walks of one card
			// look like two device models.
			"address":  s.Peer().Device().String(),
			"name":     id.Name,
			"type":     codec.UnitTypeName(id.TypeID),
			"type_id":  fmt.Sprint(id.TypeID),
			"version":  id.Version.String(),
			"services": id.Services.String(),
		},
	}
	if ty, ok := codec.LookupUnitType(id.TypeID); ok {
		info.Identity["product"] = ty.Enum
		info.Identity["category"] = ty.Category.String()
	}
	return info, nil
}

// slotStatus is the numeric the neutral model keeps beside the state.
//
// Both are filled in rather than one: callers read the state, exports and the
// command line read the numeric, and a slot that reports one without the other
// shows up as an empty slot in whichever of them was left out.
func slotStatus(s codec.Status) consumer.SlotStatus {
	if !s.Has(codec.StatusPresent) {
		return consumer.SlotNoCard
	}
	return consumer.SlotPresent
}

// slotState maps a unit's status flags onto the neutral slot state.
//
// A unit reports whether it is present and whether it is under local control.
// It has no notion of a card that is booting or removed, so those states are
// never produced rather than guessed at: a port that is not present is empty,
// and that is all the protocol says.
func slotState(s codec.Status) consumer.SlotState {
	switch {
	case !s.Has(codec.StatusPresent):
		return consumer.SlotStateNoCard
	case s.Has(codec.StatusLocal):
		// Present, but taking orders from its own front panel rather than
		// from us. The neutral model has no word for that, and "present" is
		// the honest one: it is there and it is working.
		return consumer.SlotStatePresent
	default:
		return consumer.SlotStatePresent
	}
}
