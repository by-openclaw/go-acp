package acp1

import (
	"context"
	"container/list"
	"testing"

	"acp/internal/export/canonical"
	"acp/internal/protocol"
)

// TestCanonicalize_Empty covers the fresh-Plugin case: no Connect,
// no trees cached, Canonicalize must emit a device root with an empty
// children[] array (NOT a nil children — the JSON output must be
// `[]` not `null`).
func TestCanonicalize_Empty(t *testing.T) {
	p := &Plugin{}
	exp, err := p.Canonicalize(context.Background())
	if err != nil {
		t.Fatalf("canonicalize: %v", err)
	}
	if exp == nil || exp.Root == nil {
		t.Fatalf("nil export or root")
	}
	root, ok := exp.Root.(*canonical.Node)
	if !ok {
		t.Fatalf("root type = %T, want *canonical.Node", exp.Root)
	}
	if root.OID != "1" {
		t.Errorf("root OID = %q, want 1", root.OID)
	}
	if len(root.Children) != 0 {
		t.Errorf("root.Children should be empty, got %d", len(root.Children))
	}
	if root.Children == nil {
		t.Errorf("root.Children must be non-nil empty slice (JSON [] not null)")
	}
}

// TestCanonicalize_SlotTree covers the main path: one slot walked
// with a handful of objects across identity / control / status groups.
// Assertions check the 3-deep structure (device -> slot -> group ->
// parameter) and that per-kind type mapping is correct.
func TestCanonicalize_SlotTree(t *testing.T) {
	tree := &SlotTree{
		Slot:     0,
		BootMode: 0,
		Objects: []protocol.Object{
			{Slot: 0, Group: "identity", ID: 0, Label: "Card name",
				Kind: protocol.KindString, Access: 0x01,
				Value: protocol.Value{Kind: protocol.KindString, Str: "RRS18"},
				MaxLen: 8},
			{Slot: 0, Group: "control", ID: 7, Label: "GainA",
				Kind: protocol.KindFloat, Access: 0x03,
				Min: float64(0), Max: float64(150), Step: float64(1), Def: float64(100),
				Unit: "%",
				Value: protocol.Value{Kind: protocol.KindFloat, Float: 42.5}},
			{Slot: 0, Group: "status", ID: 6, Label: "Temp_Left",
				Kind: protocol.KindInt, Access: 0x01,
				Min: int64(-50), Max: int64(150),
				Unit: "C",
				Value: protocol.Value{Kind: protocol.KindInt, Int: 37}},
			{Slot: 0, Group: "control", ID: 4, Label: "Broadcasts",
				Kind: protocol.KindEnum, Access: 0x03,
				EnumItems: []string{"Off", "On", "Auto"},
				Value: protocol.Value{Kind: protocol.KindEnum, Enum: 2}},
		},
		ACPTypes: []ObjectType{TypeString, TypeFloat, TypeInteger, TypeEnum},
	}

	p := &Plugin{host: "10.6.239.113"}
	p.trees = newSlotTreeCache(32, 0)
	// Inject via internal call — Put is the public path but only accepts
	// one tree; we bypass to avoid TTL work in tests.
	p.trees.entries = map[int]*list.Element{}
	p.trees.order = list.New()
	entry := &cacheEntry{slot: 0, tree: tree}
	el := p.trees.order.PushFront(entry)
	p.trees.entries[0] = el

	exp, err := p.Canonicalize(context.Background())
	if err != nil {
		t.Fatalf("canonicalize: %v", err)
	}

	root := exp.Root.(*canonical.Node)
	if root.Identifier != "10.6.239.113" {
		t.Errorf("root identifier = %q, want %q", root.Identifier, "10.6.239.113")
	}
	if len(root.Children) != 1 {
		t.Fatalf("want 1 slot child, got %d", len(root.Children))
	}

	slot0, ok := root.Children[0].(*canonical.Node)
	if !ok {
		t.Fatalf("slot child type = %T", root.Children[0])
	}
	if slot0.Identifier != "slot-0" {
		t.Errorf("slot identifier = %q", slot0.Identifier)
	}

	// Expect identity + control + status groups (3 populated, alarm +
	// file are empty so skipped).
	if len(slot0.Children) != 3 {
		t.Fatalf("want 3 group children, got %d (names: %v)", len(slot0.Children), groupNames(slot0.Children))
	}

	names := groupNames(slot0.Children)
	if names[0] != "identity" || names[1] != "control" || names[2] != "status" {
		t.Errorf("group order = %v, want identity/control/status", names)
	}

	// Identity contains 1 Parameter (Card name) typed string with
	// maxLen hint in format.
	ident := slot0.Children[0].(*canonical.Node)
	if len(ident.Children) != 1 {
		t.Fatalf("identity len = %d, want 1", len(ident.Children))
	}
	card := ident.Children[0].(*canonical.Parameter)
	if card.Type != canonical.ParamString {
		t.Errorf("card type = %q, want string", card.Type)
	}
	if v, ok := card.Value.(string); !ok || v != "RRS18" {
		t.Errorf("card value = %v (%T), want \"RRS18\"", card.Value, card.Value)
	}
	if card.Format == nil || *card.Format != "maxLen=8" {
		t.Errorf("card format = %v, want *maxLen=8", card.Format)
	}
	if card.Access != canonical.AccessRead {
		t.Errorf("card access = %q, want read", card.Access)
	}

	// Control contains GainA (float, rw) and Broadcasts (enum, rw).
	control := slot0.Children[1].(*canonical.Node)
	if len(control.Children) != 2 {
		t.Fatalf("control len = %d, want 2", len(control.Children))
	}
	// sortByNumber ordering: Broadcasts (id=4) before GainA (id=7).
	broadcasts := control.Children[0].(*canonical.Parameter)
	if broadcasts.Type != canonical.ParamEnum {
		t.Errorf("broadcasts type = %q, want enum", broadcasts.Type)
	}
	if len(broadcasts.EnumMap) != 3 {
		t.Errorf("broadcasts enumMap len = %d, want 3", len(broadcasts.EnumMap))
	}
	if broadcasts.EnumMap[1].Key != "On" {
		t.Errorf("broadcasts.EnumMap[1].Key = %q, want On", broadcasts.EnumMap[1].Key)
	}

	gain := control.Children[1].(*canonical.Parameter)
	if gain.Type != canonical.ParamReal {
		t.Errorf("gain type = %q, want real", gain.Type)
	}
	if gain.Access != canonical.AccessReadWrite {
		t.Errorf("gain access = %q, want readWrite", gain.Access)
	}
	if gain.Minimum != float64(0) {
		t.Errorf("gain minimum = %v, want 0", gain.Minimum)
	}
	if gain.Unit == nil || *gain.Unit != "%" {
		t.Errorf("gain unit = %v, want *%%", gain.Unit)
	}
}

func TestAccessString(t *testing.T) {
	cases := []struct {
		bits uint8
		want string
	}{
		{0, canonical.AccessNone},
		{0x01, canonical.AccessRead},
		{0x02, canonical.AccessWrite},
		{0x03, canonical.AccessReadWrite},
		{0x07, canonical.AccessReadWrite}, // read+write+setDef collapses to rw
	}
	for _, c := range cases {
		if got := accessString(c.bits); got != c.want {
			t.Errorf("accessString(%#x) = %q, want %q", c.bits, got, c.want)
		}
	}
}

func groupNames(children []canonical.Element) []string {
	out := make([]string, 0, len(children))
	for _, c := range children {
		out = append(out, c.Common().Identifier)
	}
	return out
}
