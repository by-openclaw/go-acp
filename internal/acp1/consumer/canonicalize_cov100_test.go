package acp1

import (
	"context"
	"testing"

	"dhs/internal/acp1/codec"
	"dhs/internal/consumer"
	"dhs/internal/export/canonical"
)

// TestBuildGroupNode_NilTree covers the nil-tree early return.
func TestBuildGroupNode_NilTree(t *testing.T) {
	if n := buildGroupNode(0, "0", "slot-0", 2, "control", nil); n != nil {
		t.Fatalf("nil tree → want nil node, got %v", n)
	}
}

// TestBuildParameter_AlarmAndEmptyLabel drives buildParameter's alarm
// on/off message description arm and the empty-label "#id" identifier
// fallback, plus a trailing sub-group marker with no children (so the
// EmptyChildren() normalisation runs).
func TestBuildParameter_AlarmAndEmptyLabel(t *testing.T) {
	tree := &SlotTree{
		Objects: []consumer.Object{
			// Alarm object with both messages.
			{Group: "alarm", ID: 0, Label: "Temp", Kind: consumer.KindAlarm, Access: 0x01,
				AlarmOnMsg: "too hot", AlarmOffMsg: "ok",
				Value: consumer.Value{Kind: consumer.KindAlarm, Bool: true}},
			// Object with empty label → identifier "#1".
			{Group: "alarm", ID: 1, Label: "", Kind: consumer.KindAlarm, Access: 0x01},
			// A sub-group marker at the end with no following objects.
			{Group: "alarm", ID: 2, Label: " SECTION", Kind: consumer.KindString, SubGroupMarker: true, Access: 0x01},
		},
		ACPTypes: []codec.ObjectType{codec.TypeAlarm, codec.TypeAlarm, codec.TypeString},
	}
	node := buildGroupNode(0, "0.4", "slot-0", 4, "alarm", tree)
	if node == nil {
		t.Fatal("buildGroupNode returned nil")
	}

	var sawDesc, sawHashID, sawEmptyMarker bool
	for _, el := range node.Children {
		switch e := el.(type) {
		case *canonical.Parameter:
			if e.Description != nil && *e.Description != "" {
				sawDesc = true
			}
			if e.Identifier == "#1" {
				sawHashID = true
			}
		case *canonical.Node:
			// A trailing marker opens a section that nothing follows, so its
			// only child is the marker's own leaf — the one that carries the
			// wire id. It is never childless.
			if e.Identifier == "SECTION" && len(e.Children) == 1 {
				sawEmptyMarker = true
			}
		}
	}
	if !sawDesc {
		t.Error("alarm on/off description not emitted")
	}
	if !sawHashID {
		t.Error("empty-label parameter should get #id identifier")
	}
	if !sawEmptyMarker {
		t.Error("a trailing marker should still carry its own leaf")
	}
}

// TestCanonicalize_CtxCanceled covers the cancelled-context guard.
func TestCanonicalize_CtxCanceled(t *testing.T) {
	p := &Plugin{host: "10.0.0.1"}
	p.trees = newSlotTreeCache(8, 0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Canonicalize(ctx); err == nil {
		t.Fatal("cancelled ctx: want error")
	}
}
