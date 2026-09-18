package rollcall

import (
	"context"
	"testing"

	"dhs/internal/snell-rollcall/codec"
)

// TestWalkANodeWithNoMenuReturnsEmpty covers the #1095 guard: a node that
// advertises neither the menu nor the control service (a proxy's bridge node)
// is walked to an empty tree rather than surfacing the NACK its far end
// correctly sends for a service it does not offer.
func TestWalkANodeWithNoMenuReturnsEmpty(t *testing.T) {
	// A bridge (SvcNet) whose far side is one node offering only the map
	// service — no menu, no control. It enumerates as a node, but there is
	// nothing on it to walk.
	menuless := farDevice(0x1000, 0x08, "RollNet segment")
	menuless.ID.Services = codec.SvcMap
	h := newHarness(t, func(d *device) {
		d.ports = 1
		d.services |= codec.SvcNet
		d.farByBridge = map[codec.Address][]codec.DeviceInfo{
			{Unit: gatewayAddr.Unit, Port: 0, Index: codec.IndexUnknown}: {menuless},
		}
	})
	ctx := context.Background()

	info, err := h.plugin.GetDeviceInfo(ctx)
	if err != nil {
		t.Fatalf("GetDeviceInfo: %v", err)
	}

	// Find the slot of the menuless far node and walk it: empty, no error.
	walkedEmpty := false
	for slot := 0; slot < info.NumSlots; slot++ {
		si, err := h.plugin.GetSlotInfo(ctx, slot)
		if err != nil {
			continue
		}
		if si.Identity["services"] != codec.SvcMap.String() {
			continue
		}
		objs, err := h.plugin.Walk(ctx, slot)
		if err != nil {
			t.Fatalf("walking the menuless node errored instead of returning empty: %v", err)
		}
		if len(objs) != 0 {
			t.Errorf("the menuless node walked %d objects, want none", len(objs))
		}
		walkedEmpty = true
	}
	if !walkedEmpty {
		t.Fatal("no menuless node was found to walk")
	}
}
