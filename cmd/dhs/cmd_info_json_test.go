package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"dhs/internal/consumer"
)

// slotSource answers GetSlotInfo from a canned table, so a test can describe a
// frame without one existing.
type slotSource struct {
	fakePlugin
	slots map[int]consumer.SlotInfo
	errs  map[int]error
}

func (s *slotSource) GetSlotInfo(ctx context.Context, slot int) (consumer.SlotInfo, error) {
	if err := s.errs[slot]; err != nil {
		return consumer.SlotInfo{}, err
	}
	return s.slots[slot], nil
}

func TestInfoJSONCarriesTheIdentityAPluginFilledIn(t *testing.T) {
	// On a controller a slot number is a position in a list rather than a place
	// in a frame, and the address behind it is the only thing that names the
	// node. The runbook tells operators to read it out of this JSON.
	src := &slotSource{
		slots: map[int]consumer.SlotInfo{
			0: {Slot: 0, Status: consumer.SlotPresent, IsOnline: true,
				Identity: map[string]string{"type": "Vega Controller", "address": "0000-81-00:007"}},
			1: {Slot: 1, Status: consumer.SlotPresent, IsOnline: true},
		},
	}

	out := buildInfoJSON(context.Background(), src,
		consumer.DeviceInfo{IP: "10.6.250.105", Port: 2050, NumSlots: 2, ProtocolVersion: 3}, "rollcall")

	if len(out.SlotStatus) != 2 {
		t.Fatalf("%d slots described, want 2", len(out.SlotStatus))
	}
	if got := out.SlotStatus[0].Identity["address"]; got != "0000-81-00:007" {
		t.Errorf("slot 0 address = %q; the identity is what names a node", got)
	}
	if got := out.SlotStatus[0].Identity["type"]; got != "Vega Controller" {
		t.Errorf("slot 0 type = %q", got)
	}

	// A plugin that fills nothing in leaves the key out entirely, so every
	// protocol without the notion keeps the shape it had.
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded struct {
		SlotStatus []map[string]any `json:"slot_status"`
	}
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := decoded.SlotStatus[1]["identity"]; ok {
		t.Error("a slot with no identity should carry no identity key")
	}
	if _, ok := decoded.SlotStatus[0]["identity"]; !ok {
		t.Error("a slot with an identity should carry one")
	}
}

func TestInfoJSONKeepsASlotThatWouldNotAnswer(t *testing.T) {
	// A frame with one unreadable card still describes the rest, and the one
	// that failed says why rather than disappearing.
	src := &slotSource{
		slots: map[int]consumer.SlotInfo{0: {Slot: 0, Status: consumer.SlotPresent, IsOnline: true}},
		errs:  map[int]error{1: errors.New("reply timeout")},
	}

	out := buildInfoJSON(context.Background(), src,
		consumer.DeviceInfo{IP: "h", Port: 1, NumSlots: 2}, "rollcall")

	if len(out.SlotStatus) != 2 {
		t.Fatalf("%d slots described, want both", len(out.SlotStatus))
	}
	if !strings.Contains(out.SlotStatus[1].Error, "reply timeout") {
		t.Errorf("slot 1 error = %q", out.SlotStatus[1].Error)
	}
	if out.SlotStatus[1].Status != "" || out.SlotStatus[1].Identity != nil {
		t.Error("a slot that did not answer describes nothing but its error")
	}
	if out.Device != "h:1" || out.Protocol != "rollcall" {
		t.Errorf("device = %q protocol = %q", out.Device, out.Protocol)
	}
}
