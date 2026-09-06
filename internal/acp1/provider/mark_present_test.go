package acp1

import (
	"dhs/internal/plugin"
	"io"
	"log/slog"
	"testing"

	"dhs/internal/acp1/codec"
	"dhs/internal/export/canonical"
)

// cardSlot builds a minimal populated card slot: one identity label, enough
// for newTree to count it as a card (numIdentity > 0).
func cardSlot(num int, ident, model string) *canonical.Node {
	r := canonical.AccessRead
	return &canonical.Node{
		Header: canonical.Header{
			Number: num, Identifier: ident, Access: r,
			Children: []canonical.Element{
				&canonical.Node{
					Header: canonical.Header{
						Number: 1, Identifier: "identity", Access: r,
						Children: []canonical.Element{
							&canonical.Parameter{
								Header: canonical.Header{
									Number: 0, Identifier: "Model", Access: r,
									Children: canonical.EmptyChildren(),
								},
								Type:  canonical.ParamString,
								Value: model,
							},
						},
					},
				},
			},
		},
	}
}

func newServerFromSlots(t *testing.T, slots ...*canonical.Node) *server {
	t.Helper()
	children := make([]canonical.Element, len(slots))
	for i, sl := range slots {
		children[i] = sl
	}
	exp := &canonical.Export{Root: &canonical.Node{
		Header: canonical.Header{
			Number: 1, Identifier: "device", Access: canonical.AccessRead,
			Children: children,
		},
	}}
	return newServer(plugin.Deps{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}, exp)
}

// TestMarkTreeSlotsPresent_MultiCardFrame is the multi-card frame case: a tree
// served via --tree with several populated slots and NO frame-status object.
// MarkTreeSlotsPresent must synthesise frame-status sized to the highest slot
// and mark each populated slot present (2), leaving gaps as no_card (0). Before
// this, only the explicit manifest slot list drove frame-status, so a
// hand-authored multi-slot tree.json served an empty-looking rack.
func TestMarkTreeSlotsPresent_MultiCardFrame(t *testing.T) {
	// Slots 0, 1, 3 populated; slot 2 is an empty gap.
	s := newServerFromSlots(t,
		cardSlot(0, "slot-0", "RRS18"),
		cardSlot(1, "slot-1", "GIO-12"),
		cardSlot(3, "slot-3", "2GS110"),
	)

	if e := s.tree.entries[objectKey{slot: 0, group: codec.GroupFrame, id: 0}]; e != nil {
		t.Fatal("setup: frame-status should be absent before MarkTreeSlotsPresent")
	}

	if err := s.MarkTreeSlotsPresent(); err != nil {
		t.Fatalf("MarkTreeSlotsPresent: %v", err)
	}

	want := map[uint8]uint8{0: 2, 1: 2, 2: 0, 3: 2} // present except the gap
	for slot, st := range want {
		if got := readSlotStatus(t, s, slot); got != st {
			t.Errorf("slot %d status = %d, want %d", slot, got, st)
		}
	}
}

// TestMarkTreeSlotsPresent_EmptyFrameNoOp: a tree with no card slots must not
// synthesise a frame-status object — there is nothing present to report.
func TestMarkTreeSlotsPresent_EmptyFrameNoOp(t *testing.T) {
	s := newServerFromSlots(t)
	if err := s.MarkTreeSlotsPresent(); err != nil {
		t.Fatalf("MarkTreeSlotsPresent: %v", err)
	}
	if e := s.tree.entries[objectKey{slot: 0, group: codec.GroupFrame, id: 0}]; e != nil {
		t.Error("no cards → frame-status should not be synthesised")
	}
}
