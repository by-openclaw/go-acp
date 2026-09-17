package acp1

import (
	"testing"

	"dhs/internal/acp1/codec"
	"dhs/internal/consumer"
	"dhs/internal/export/canonical"
)

// TestBuildGroupNode_NestsSubGroups verifies the consumer models Synapse
// sub-group section headers (DOWN CONV / TRANSPARENT / …) as PARENT nodes with
// the objects that follow them nested as children — matching what a real
// controller (Cerebrum) shows — rather than a flat sibling list.
func TestBuildGroupNode_NestsSubGroups(t *testing.T) {
	tree := &SlotTree{
		Objects: []consumer.Object{
			{Group: "control", ID: 0, Label: "IO-Ctrl", Kind: consumer.KindEnum, Access: 0x03},
			{Group: "control", ID: 29, Label: "  DOWN CONV", Kind: consumer.KindString, SubGroupMarker: true, Access: 0x01},
			{Group: "control", ID: 30, Label: "Dn_CtrlA", Kind: consumer.KindEnum, Access: 0x03},
			{Group: "control", ID: 31, Label: "Dn_ArcA", Kind: consumer.KindEnum, Access: 0x03},
			{Group: "control", ID: 47, Label: "  TRANSPARENT", Kind: consumer.KindString, SubGroupMarker: true, Access: 0x01},
			{Group: "control", ID: 48, Label: "Tr_CtrlA", Kind: consumer.KindEnum, Access: 0x03},
		},
		ACPTypes: []codec.ObjectType{
			codec.TypeEnum, codec.TypeString, codec.TypeEnum, codec.TypeEnum, codec.TypeString, codec.TypeEnum,
		},
	}

	node := buildGroupNode(1, "1.2", "slot-1", 2, "control", tree)
	if node == nil {
		t.Fatal("buildGroupNode returned nil")
	}

	// Top-level control children: IO-Ctrl (leaf) + DOWN CONV (node) + TRANSPARENT (node).
	var downConv, transparent *canonical.Node
	var sawIOCtrl bool
	for _, el := range node.Children {
		switch e := el.(type) {
		case *canonical.Parameter:
			if e.Identifier == "IO-Ctrl" {
				sawIOCtrl = true
			}
		case *canonical.Node:
			switch e.Identifier {
			case "DOWN CONV":
				downConv = e
			case "TRANSPARENT":
				transparent = e
			}
		}
	}
	if !sawIOCtrl {
		t.Error("IO-Ctrl (pre-section object) should be a direct control child")
	}
	if downConv == nil {
		t.Fatal("DOWN CONV should be a parent Node under control")
	}
	if transparent == nil {
		t.Fatal("TRANSPARENT should be a parent Node under control")
	}

	// DOWN CONV nests its own marker leaf followed by its two objects. The
	// marker is carried twice on purpose: as the Node that groups the
	// section for display, and as the leaf that holds its wire id. Drop the
	// leaf and the group's ids have a hole wherever a marker sits, which is
	// what made a walk of a served copy stop at "object instance does not
	// exist".
	got := childIdentifiers(downConv)
	if len(got) != 3 || got[0] != "  DOWN CONV" || got[1] != "Dn_CtrlA" || got[2] != "Dn_ArcA" {
		t.Errorf("DOWN CONV children = %v, want [\"  DOWN CONV\" Dn_CtrlA Dn_ArcA]", got)
	}
	// The next marker opens a fresh section — Tr_CtrlA nests under TRANSPARENT,
	// not DOWN CONV.
	if tr := childIdentifiers(transparent); len(tr) != 2 || tr[0] != "  TRANSPARENT" || tr[1] != "Tr_CtrlA" {
		t.Errorf("TRANSPARENT children = %v, want [\"  TRANSPARENT\" Tr_CtrlA]", tr)
	}
	// Nested child path reflects the hierarchy.
	for _, el := range downConv.Children {
		if p, ok := el.(*canonical.Parameter); ok && p.Identifier == "Dn_CtrlA" {
			if p.Path != "slot-1.control.DOWN CONV.Dn_CtrlA" {
				t.Errorf("Dn_CtrlA path = %q, want slot-1.control.DOWN CONV.Dn_CtrlA", p.Path)
			}
		}
	}
}

func childIdentifiers(n *canonical.Node) []string {
	out := make([]string, 0, len(n.Children))
	for _, el := range n.Children {
		out = append(out, el.Common().Identifier)
	}
	return out
}

// TestBuildGroupNode_NoSubGroupTerminator verifies a single-space NO_SUB_GROUP
// marker closes an open section back to group top-level.
func TestBuildGroupNode_NoSubGroupTerminator(t *testing.T) {
	tree := &SlotTree{
		Objects: []consumer.Object{
			{Group: "control", ID: 0, Label: " DOWN CONV", Kind: consumer.KindString, SubGroupMarker: true, Access: 0x01},
			{Group: "control", ID: 1, Label: "Dn_CtrlA", Kind: consumer.KindEnum, Access: 0x03},
			{Group: "control", ID: 2, Label: " ", Kind: consumer.KindEnum, SubGroupMarker: true, Access: 0x01}, // NO_SUB_GROUP
			{Group: "control", ID: 3, Label: "TopLevelAgain", Kind: consumer.KindEnum, Access: 0x03},
		},
		ACPTypes: []codec.ObjectType{codec.TypeString, codec.TypeEnum, codec.TypeEnum, codec.TypeEnum},
	}
	node := buildGroupNode(1, "1.2", "slot-1", 2, "control", tree)
	// TopLevelAgain must be a direct control child (not nested under DOWN CONV).
	var topLevel bool
	for _, el := range node.Children {
		if p, ok := el.(*canonical.Parameter); ok && p.Identifier == "TopLevelAgain" {
			topLevel = true
		}
	}
	if !topLevel {
		t.Error("after NO_SUB_GROUP, TopLevelAgain should be a direct control child")
	}
}
