package acp1

import (
	"testing"

	"dhs/internal/acp1/codec"
	"dhs/internal/export/canonical"
)

func TestBroadcastsEnabled_NoGateObject_PermissiveDefault(t *testing.T) {
	s := newTestServer(t)
	if !s.tree.broadcastsEnabled() {
		t.Fatal("default tree without (slot=0, control, id=4) should report enabled=true")
	}
}

func TestBroadcastsEnabled_EnumOn(t *testing.T) {
	tr := &tree{
		entries: map[objectKey]*entry{
			{slot: 0, group: codec.GroupControl, id: 4}: {
				key:     objectKey{slot: 0, group: codec.GroupControl, id: 4},
				acpType: codec.TypeEnum,
				access:  3,
				param: &canonical.Parameter{
					Type: "enum",
					EnumMap: []canonical.EnumEntry{
						{Key: "Off", Value: 0},
						{Key: "On", Value: 1},
					},
					Value: int64(1),
				},
			},
		},
		slots: map[uint8]*slotCounts{},
	}
	if !tr.broadcastsEnabled() {
		t.Fatal("enum value=1 (On) should report enabled=true")
	}
}

func TestBroadcastsEnabled_EnumOff(t *testing.T) {
	tr := &tree{
		entries: map[objectKey]*entry{
			{slot: 0, group: codec.GroupControl, id: 4}: {
				param: &canonical.Parameter{
					Type: "enum",
					EnumMap: []canonical.EnumEntry{
						{Key: "Off", Value: 0},
						{Key: "On", Value: 1},
					},
					Value: int64(0),
				},
			},
		},
	}
	if tr.broadcastsEnabled() {
		t.Fatal("enum value=0 (Off) should report enabled=false")
	}
}

func TestBroadcastsEnabled_BareNumeric(t *testing.T) {
	cases := []struct {
		name string
		v    any
		want bool
	}{
		{"int64-zero", int64(0), false},
		{"int64-one", int64(1), true},
		{"uint64-zero", uint64(0), false},
		{"uint64-one", uint64(1), true},
		{"int-zero", int(0), false},
		{"int-one", int(1), true},
		{"float64-zero", float64(0), false},
		{"float64-one", float64(1), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := &tree{
				entries: map[objectKey]*entry{
					{slot: 0, group: codec.GroupControl, id: 4}: {
						param: &canonical.Parameter{Value: tc.v},
					},
				},
			}
			if got := tr.broadcastsEnabled(); got != tc.want {
				t.Fatalf("broadcastsEnabled() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestBroadcastsEnabled_NilParam(t *testing.T) {
	tr := &tree{
		entries: map[objectKey]*entry{
			{slot: 0, group: codec.GroupControl, id: 4}: {
				key:     objectKey{slot: 0, group: codec.GroupControl, id: 4},
				acpType: codec.TypeEnum,
				access:  3,
				// param: nil
			},
		},
	}
	if !tr.broadcastsEnabled() {
		t.Fatal("nil param should fall back to permissive default")
	}
}

func TestBroadcastAnnounce_GatedSilently_WhenOff(t *testing.T) {
	// Build a tree that has the gate object set to Off, then verify
	// broadcastAnnounce produces no output to TCP fan-out (no panic on
	// nil bcast / no exception).
	s := newTestServer(t)
	s.tree.entries[objectKey{slot: 0, group: codec.GroupControl, id: 4}] = &entry{
		param: &canonical.Parameter{Value: int64(0)},
	}

	reg := newTCPSessionRegistry(s.logger)
	send := make(chan []byte, 4)
	reg.register("10.0.0.1", nil, send)
	s.tcpRegistry = reg

	ann := &codec.Message{
		MTID: 0, MType: codec.MTypeReply, MAddr: 1,
		MCode:    byte(codec.MethodSetValue),
		ObjGroup: codec.GroupControl, ObjID: 0,
		Value: []byte{0x00, 0x05},
	}
	s.broadcastAnnounce(ann)

	if len(send) != 0 {
		t.Fatalf("Broadcasts=Off should suppress fan-out: got %d items in send queue", len(send))
	}
}

func TestBroadcastAnnounce_FlowsThroughWhenOn(t *testing.T) {
	s := newTestServer(t)
	s.tree.entries[objectKey{slot: 0, group: codec.GroupControl, id: 4}] = &entry{
		param: &canonical.Parameter{Value: int64(1)}, // On
	}

	reg := newTCPSessionRegistry(s.logger)
	send := make(chan []byte, 4)
	reg.register("10.0.0.1", nil, send)
	s.tcpRegistry = reg

	ann := &codec.Message{
		MTID: 0, MType: codec.MTypeReply, MAddr: 1,
		MCode:    byte(codec.MethodSetValue),
		ObjGroup: codec.GroupControl, ObjID: 0,
		Value: []byte{0x00, 0x05},
	}
	s.broadcastAnnounce(ann)

	if len(send) != 1 {
		t.Fatalf("Broadcasts=On should fan-out to TCP session: got %d items, want 1", len(send))
	}
}
