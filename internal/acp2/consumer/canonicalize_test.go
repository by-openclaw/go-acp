package acp2

import (
	"context"
	"testing"

	"acp/internal/export/canonical"
	"acp/internal/protocol"
)

// TestCanonicalize_Empty verifies a fresh plugin emits a root device
// Node with no slot children (no cached walks yet).
func TestCanonicalize_Empty(t *testing.T) {
	p := &Plugin{}
	out, err := p.Canonicalize(context.Background())
	if err != nil {
		t.Fatalf("Canonicalize: %v", err)
	}
	if out == nil || out.Root == nil {
		t.Fatal("expected non-nil Export and Root")
	}
	if out.Root.Common().OID != "1" {
		t.Errorf("root OID = %q, want %q", out.Root.Common().OID, "1")
	}
	if out.Root.Common().Identifier != "device" {
		t.Errorf("root Identifier = %q, want %q", out.Root.Common().Identifier, "device")
	}
	if len(out.Root.Common().Children) != 0 {
		t.Errorf("expected 0 slot children, got %d", len(out.Root.Common().Children))
	}
}

// TestCanonicalize_SingleSlot builds a two-level tree (slot → node →
// parameter), hand-crafted to avoid any wire dependency, and verifies
// the canonical shape round-trips the expected fields.
func TestCanonicalize_SingleSlot(t *testing.T) {
	tree := &WalkedTree{
		Slot: 0,
		Objects: []protocol.Object{
			{
				Slot: 0, ID: 1, Label: "ROOT_NODE_V2",
				Path:  []string{"ROOT_NODE_V2"},
				Kind:  protocol.KindRaw, Access: 1,
			},
			{
				Slot: 0, ID: 100, Label: "BOARD",
				Path:  []string{"ROOT_NODE_V2", "BOARD"},
				Kind:  protocol.KindRaw, Access: 1,
			},
			{
				Slot: 0, ID: 47431, Label: "ACP Trace",
				Path:  []string{"ROOT_NODE_V2", "BOARD", "ACP Trace"},
				Group: "BOARD",
				Kind:  protocol.KindEnum, Access: 3,
				EnumItems: []string{"Off", "On"},
				Value:     protocol.Value{Kind: protocol.KindEnum, Enum: 0, Str: "Off"},
			},
			{
				Slot: 0, ID: 3, Label: "User Label 1",
				Path:   []string{"ROOT_NODE_V2", "IDENTITY", "User Label 1"},
				Group:  "IDENTITY",
				Kind:   protocol.KindString, Access: 3, MaxLen: 17,
				Value:  protocol.Value{Kind: protocol.KindString, Str: "ACP2-OK"},
			},
		},
		ObjTypes: []ACP2ObjType{ObjTypeNode, ObjTypeNode, ObjTypeEnum, ObjTypeString},
		NumTypes: []NumberType{0, 0, 0, 0},
	}

	p := &Plugin{}
	p.trees = newWalkedTreeCache(8, 0)
	p.trees.Put(0, tree)

	out, err := p.Canonicalize(context.Background())
	if err != nil {
		t.Fatalf("Canonicalize: %v", err)
	}

	if out.Root.Common().OID != "1" {
		t.Errorf("root OID = %q, want %q", out.Root.Common().OID, "1")
	}
	if len(out.Root.Common().Children) != 1 {
		t.Fatalf("expected 1 slot child, got %d", len(out.Root.Common().Children))
	}

	slot0, ok := out.Root.Common().Children[0].(*canonical.Node)
	if !ok {
		t.Fatal("slot-0 is not *canonical.Node")
	}
	if slot0.OID != "1.1" || slot0.Identifier != "slot-0" {
		t.Errorf("slot-0 oid=%q ident=%q, want 1.1 / slot-0", slot0.OID, slot0.Identifier)
	}

	// slot-0 should contain ROOT_NODE_V2 as its only child (our fixture
	// has a single root node beneath the slot).
	if len(slot0.Children) != 1 {
		t.Fatalf("slot-0 has %d children, want 1", len(slot0.Children))
	}
	root2, ok := slot0.Children[0].(*canonical.Node)
	if !ok || root2.Identifier != "ROOT_NODE_V2" {
		t.Fatalf("slot-0 child 0 is not ROOT_NODE_V2: %T %+v", slot0.Children[0], slot0.Children[0])
	}

	// ROOT_NODE_V2 should have BOARD and IDENTITY (IDENTITY was auto-
	// materialised by ensureACP2Chain because we didn't pass a node for it).
	if len(root2.Children) != 2 {
		t.Fatalf("ROOT_NODE_V2 has %d children, want 2 (BOARD+IDENTITY)", len(root2.Children))
	}

	// Find BOARD node and verify ACP Trace.
	var board *canonical.Node
	var identity *canonical.Node
	for _, c := range root2.Children {
		n, ok := c.(*canonical.Node)
		if !ok {
			continue
		}
		switch n.Identifier {
		case "BOARD":
			board = n
		case "IDENTITY":
			identity = n
		}
	}
	if board == nil {
		t.Fatal("BOARD node not found")
	}
	if identity == nil {
		t.Fatal("IDENTITY node not found (should have been auto-materialised)")
	}

	if len(board.Children) != 1 {
		t.Fatalf("BOARD has %d children, want 1 (ACP Trace)", len(board.Children))
	}
	acpTrace, ok := board.Children[0].(*canonical.Parameter)
	if !ok {
		t.Fatalf("BOARD child is not Parameter: %T", board.Children[0])
	}
	if acpTrace.Identifier != "ACP Trace" {
		t.Errorf("ACP Trace identifier = %q", acpTrace.Identifier)
	}
	if acpTrace.Number != 47431 {
		t.Errorf("ACP Trace Number = %d, want 47431", acpTrace.Number)
	}
	if acpTrace.OID != "1.1.47431" {
		t.Errorf("ACP Trace OID = %q, want 1.1.47431", acpTrace.OID)
	}
	if acpTrace.Access != canonical.AccessReadWrite {
		t.Errorf("ACP Trace access = %q, want %q", acpTrace.Access, canonical.AccessReadWrite)
	}
	if acpTrace.Type != canonical.ParamEnum {
		t.Errorf("ACP Trace Type = %q, want %q", acpTrace.Type, canonical.ParamEnum)
	}
	if len(acpTrace.EnumMap) != 2 {
		t.Errorf("ACP Trace EnumMap entries = %d, want 2", len(acpTrace.EnumMap))
	}

	// User Label 1 under IDENTITY.
	if len(identity.Children) != 1 {
		t.Fatalf("IDENTITY has %d children, want 1 (User Label 1)", len(identity.Children))
	}
	userLabel, ok := identity.Children[0].(*canonical.Parameter)
	if !ok {
		t.Fatalf("IDENTITY child is not Parameter: %T", identity.Children[0])
	}
	if userLabel.Identifier != "User Label 1" {
		t.Errorf("identifier = %q", userLabel.Identifier)
	}
	if userLabel.Type != canonical.ParamString {
		t.Errorf("type = %q, want string", userLabel.Type)
	}
	if userLabel.Format == nil || *userLabel.Format != "maxLen=17" {
		t.Errorf("format = %v, want maxLen=17 hint", userLabel.Format)
	}
	if got, want := userLabel.Value, any("ACP2-OK"); got != want {
		t.Errorf("value = %v, want %v", got, want)
	}
}

// TestACP2AccessString covers each wire access byte → canonical string
// mapping exhaustively.
func TestACP2AccessString(t *testing.T) {
	cases := []struct {
		in   uint8
		want string
	}{
		{0, canonical.AccessNone},
		{1, canonical.AccessRead},
		{2, canonical.AccessWrite},
		{3, canonical.AccessReadWrite},
	}
	for _, c := range cases {
		if got := acp2AccessString(c.in); got != c.want {
			t.Errorf("acp2AccessString(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}
