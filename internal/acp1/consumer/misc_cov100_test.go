package acp1

import (
	"context"
	"testing"

	"dhs/internal/acp1/codec"
	"dhs/internal/consumer"
	"dhs/internal/export"
)

// TestIdentityProbe_EmptyModelErrors: GetIdentity returns an empty Model
// (the identity field decodes to "") → IdentityProbe surfaces the error.
func TestIdentityProbe_EmptyModelErrors(t *testing.T) {
	p, ft, mtid := newPluginWithClient(t)
	ft.recv = [][]byte{
		// model (id 0) → empty string.
		buildReply(t, mtid+1, codec.MTypeReply, byte(codec.MethodGetObject), codec.GroupIdentity, 0, stringObject("", "Name")),
		// swrev (id 3), hwrev (id 4) — values irrelevant once Model is empty,
		// but GetIdentity reads them too.
		buildReply(t, mtid+2, codec.MTypeReply, byte(codec.MethodGetObject), codec.GroupIdentity, 3, stringObject("1", "Sw")),
		buildReply(t, mtid+3, codec.MTypeReply, byte(codec.MethodGetObject), codec.GroupIdentity, 4, stringObject("A", "Hw")),
	}
	if _, err := p.IdentityProbe(context.Background(), 1); err == nil {
		t.Fatal("empty Model: want error")
	}
}

// TestSeedFromDM_EmptySlots covers the no-slots guard.
func TestSeedFromDM_EmptySlots(t *testing.T) {
	p, _, _ := newPluginWithClient(t)
	p.trees = newSlotTreeCache(8, 0)
	if err := p.SeedFromDM(0, &export.Snapshot{}); err == nil {
		t.Fatal("empty snapshot slots: want error")
	}
}

// TestMutateNoArg_ErrorPaths covers resolve failure, Do error, and the
// fetchObjectMeta-failure → decodeByGroup fallback in mutateNoArg.
func TestMutateNoArg_ErrorPaths(t *testing.T) {
	// Resolve failure: label with no tree.
	p, _, _ := newPluginWithClient(t)
	p.trees = newSlotTreeCache(8, 0)
	if _, err := p.SetIncValue(context.Background(),
		consumer.ValueRequest{Slot: 0, Label: "Ghost", ID: -1}); err == nil {
		t.Fatal("resolve fail: want error")
	}

	// Do error: empty recv queue.
	p2, _, _ := newPluginWithClient(t)
	p2.trees = newSlotTreeCache(8, 0)
	if _, err := p2.SetIncValue(context.Background(),
		consumer.ValueRequest{Slot: 0, Group: "control", ID: 0}); err == nil {
		t.Fatal("Do error: want error")
	}

	// Cold cache: setInc reply OK, but the follow-up getObject meta-fetch
	// errors → decodeByGroup fallback (returns a value, no error).
	p3, ft3, m3 := newPluginWithClient(t)
	p3.trees = newSlotTreeCache(8, 0)
	ft3.recv = [][]byte{
		// setInc reply with a value.
		buildReply(t, m3+1, codec.MTypeReply, byte(codec.MethodSetIncValue), codec.GroupControl, 0, []byte{0x00, 0x06}),
		// getObject meta-fetch → error reply → decodeByGroup fallback.
		buildReply(t, m3+2, codec.MTypeError, 17, codec.GroupControl, 0, nil),
	}
	if _, err := p3.SetIncValue(context.Background(),
		consumer.ValueRequest{Slot: 0, Group: "control", ID: 0}); err != nil {
		t.Fatalf("cold-cache decodeByGroup fallback: %v", err)
	}
}
