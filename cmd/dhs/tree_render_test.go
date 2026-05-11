package main

import (
	"bytes"
	"strings"
	"testing"

	"dhs/internal/protocol"
)

// fixtureACP2Tree returns an ACP2-shaped slice with ROOT_NODE_V2 as the
// implicit root, INPUT and OUTPUT branches, and a few leaves with kinds.
// Mirrors the live Neuron card layout.
func fixtureACP2Tree() []protocol.Object {
	return []protocol.Object{
		{ID: 1, Label: "ROOT_NODE_V2", Path: []string{"ROOT_NODE_V2"}, Kind: protocol.KindRaw, Access: 1},
		{ID: 12820, Label: "INPUT", Path: []string{"ROOT_NODE_V2", "INPUT"}, Kind: protocol.KindRaw, Access: 1},
		{ID: 15522, Label: "SDI", Path: []string{"ROOT_NODE_V2", "INPUT", "SDI"}, Kind: protocol.KindRaw, Access: 1},
		{ID: 15750, Label: "CHANNEL 01", Path: []string{"ROOT_NODE_V2", "INPUT", "SDI", "CHANNEL 01"}, Kind: protocol.KindRaw, Access: 1},
		{ID: 15758, Label: "Direction", Path: []string{"ROOT_NODE_V2", "INPUT", "SDI", "CHANNEL 01", "Direction"}, Kind: protocol.KindEnum, Access: 3,
			EnumItems: []string{"Input", "Output", "Off"},
			Value:     protocol.Value{Kind: protocol.KindEnum, Enum: 0, Str: "Input"}},
		{ID: 15765, Label: "Used", Path: []string{"ROOT_NODE_V2", "INPUT", "SDI", "CHANNEL 01", "Used"}, Kind: protocol.KindBool, Access: 1,
			Value: protocol.Value{Kind: protocol.KindBool, Bool: true}},
		{ID: 15770, Label: "Video Format", Path: []string{"ROOT_NODE_V2", "INPUT", "SDI", "CHANNEL 01", "Video Format"}, Kind: protocol.KindEnum, Access: 1,
			EnumItems: []string{"1080i59", "1080p30"},
			Value:     protocol.Value{Kind: protocol.KindEnum, Enum: 0, Str: "1080i59"}},
		{ID: 15228, Label: "OUTPUT", Path: []string{"ROOT_NODE_V2", "OUTPUT"}, Kind: protocol.KindRaw, Access: 1},
		{ID: 15231, Label: "SDI", Path: []string{"ROOT_NODE_V2", "OUTPUT", "SDI"}, Kind: protocol.KindRaw, Access: 1},
	}
}

func TestRenderTree_FullFromRoot(t *testing.T) {
	var buf bytes.Buffer
	if err := renderTree(&buf, fixtureACP2Tree(), treeRenderOpts{ASCII: true}); err != nil {
		t.Fatalf("renderTree: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"ROOT_NODE_V2", "INPUT", "OUTPUT", "CHANNEL 01", "Direction", "Used"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output:\n%s", want, out)
		}
	}
}

func TestRenderTree_FocusByPath_RendersAncestorChain(t *testing.T) {
	var buf bytes.Buffer
	err := renderTree(&buf, fixtureACP2Tree(), treeRenderOpts{
		FromPath: "INPUT.SDI.CHANNEL 01",
		ASCII:    true,
	})
	if err != nil {
		t.Fatalf("renderTree: %v", err)
	}
	out := buf.String()
	// Ancestor chain ROOT_NODE_V2 -> INPUT -> SDI -> CHANNEL 01 must be present.
	for _, want := range []string{"ROOT_NODE_V2", "INPUT", "SDI", "CHANNEL 01", "Direction", "Used", "Video Format"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in focused output:\n%s", want, out)
		}
	}
	// OUTPUT subtree must NOT render (sibling of INPUT, off the chain).
	if strings.Contains(out, "OUTPUT") {
		t.Errorf("OUTPUT must not appear when focused on INPUT subtree:\n%s", out)
	}
}

func TestRenderTree_FocusByOID_ResolvesToPath(t *testing.T) {
	var buf bytes.Buffer
	// 15750 = CHANNEL 01
	err := renderTree(&buf, fixtureACP2Tree(), treeRenderOpts{
		FromOID: "15750",
		ASCII:   true,
	})
	if err != nil {
		t.Fatalf("renderTree: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"ROOT_NODE_V2", "INPUT", "SDI", "CHANNEL 01", "Direction"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in OID-focused output:\n%s", want, out)
		}
	}
	if strings.Contains(out, "OUTPUT") {
		t.Errorf("OUTPUT must not appear when focused via OID on CHANNEL 01:\n%s", out)
	}
}

func TestRenderTree_DepthCap_LimitsDescentFromFocus(t *testing.T) {
	var buf bytes.Buffer
	// Focus on INPUT, depth 1 — should render INPUT + SDI, but NOT CHANNEL 01 below SDI.
	err := renderTree(&buf, fixtureACP2Tree(), treeRenderOpts{
		FromPath: "INPUT",
		Depth:    1,
		ASCII:    true,
	})
	if err != nil {
		t.Fatalf("renderTree: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "INPUT") {
		t.Errorf("focus node INPUT missing:\n%s", out)
	}
	if !strings.Contains(out, "SDI") {
		t.Errorf("depth 1 child SDI missing:\n%s", out)
	}
	if strings.Contains(out, "CHANNEL 01") {
		t.Errorf("depth 1 must not descend to CHANNEL 01:\n%s", out)
	}
}

func TestRenderTree_DepthCapZeroIsUnlimited(t *testing.T) {
	var buf bytes.Buffer
	if err := renderTree(&buf, fixtureACP2Tree(), treeRenderOpts{Depth: 0, ASCII: true}); err != nil {
		t.Fatalf("renderTree: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Direction") {
		t.Errorf("depth 0 should be unlimited; Direction (deep leaf) must render:\n%s", out)
	}
}

func TestRenderTree_UnknownFocusPath_ReturnsError(t *testing.T) {
	var buf bytes.Buffer
	err := renderTree(&buf, fixtureACP2Tree(), treeRenderOpts{FromPath: "DOES.NOT.EXIST"})
	if err == nil {
		t.Fatalf("expected error for unknown focus path, got nil")
	}
	if !strings.Contains(err.Error(), "DOES.NOT.EXIST") {
		t.Errorf("error should mention the bad path: %v", err)
	}
}

func TestRenderTree_UnknownFocusOID_ReturnsError(t *testing.T) {
	var buf bytes.Buffer
	err := renderTree(&buf, fixtureACP2Tree(), treeRenderOpts{FromOID: "999999"})
	if err == nil {
		t.Fatalf("expected error for unknown focus OID, got nil")
	}
}

func TestRenderTree_BothFocusFlags_ReturnsError(t *testing.T) {
	var buf bytes.Buffer
	err := renderTree(&buf, fixtureACP2Tree(), treeRenderOpts{
		FromPath: "INPUT",
		FromOID:  "12820",
	})
	if err == nil {
		t.Fatalf("expected mutually-exclusive error, got nil")
	}
}

// fixtureACP1Tree returns an ACP1-shaped slice — flat groups, empty
// Path on leaves, non-empty Group. Confirms the renderer is generic.
func fixtureACP1Tree() []protocol.Object {
	return []protocol.Object{
		{ID: 0, Label: "Frame Status", Group: "frame", Path: []string{"frame", "Frame Status"}, Kind: protocol.KindFrame, Access: 1},
		{ID: 1, Label: "Card Name", Group: "identity", Path: []string{"identity", "Card Name"}, Kind: protocol.KindString, Access: 1,
			Value: protocol.Value{Kind: protocol.KindString, Str: "MyCard"}},
		{ID: 2, Label: "Serial Number", Group: "identity", Path: []string{"identity", "Serial Number"}, Kind: protocol.KindString, Access: 1,
			Value: protocol.Value{Kind: protocol.KindString, Str: "SN42"}},
		{ID: 10, Label: "Gain", Group: "control", Path: []string{"control", "Gain"}, Kind: protocol.KindFloat, Access: 3,
			Value: protocol.Value{Kind: protocol.KindFloat, Float: -3.5}},
	}
}

func TestRenderTree_GenericAcrossProtocols_ACP1(t *testing.T) {
	var buf bytes.Buffer
	if err := renderTree(&buf, fixtureACP1Tree(), treeRenderOpts{ASCII: true}); err != nil {
		t.Fatalf("renderTree ACP1: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"frame", "identity", "control", "Card Name", "Serial Number", "Gain"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in ACP1 output:\n%s", want, out)
		}
	}
}

func TestRenderTree_FocusByPath_ACP1Group(t *testing.T) {
	var buf bytes.Buffer
	err := renderTree(&buf, fixtureACP1Tree(), treeRenderOpts{
		FromPath: "identity",
		ASCII:    true,
	})
	if err != nil {
		t.Fatalf("renderTree ACP1 focused: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Card Name") || !strings.Contains(out, "Serial Number") {
		t.Errorf("identity descendants missing:\n%s", out)
	}
	if strings.Contains(out, "Gain") {
		t.Errorf("control.Gain must not render under identity focus:\n%s", out)
	}
}

func TestRenderTree_UnicodeDefault(t *testing.T) {
	var buf bytes.Buffer
	if err := renderTree(&buf, fixtureACP1Tree(), treeRenderOpts{}); err != nil {
		t.Fatalf("renderTree: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "├") && !strings.Contains(out, "└") {
		t.Errorf("default unicode chars missing from output:\n%s", out)
	}
}
