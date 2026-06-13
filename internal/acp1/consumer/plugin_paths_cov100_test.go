package acp1

import (
	"context"
	"testing"

	"dhs/internal/acp1/codec"
	"dhs/internal/consumer"
)

// TestGetDeviceInfo_ShortValue: a frame-status reply with an empty value
// body trips the "reply too short" guard.
func TestGetDeviceInfo_ShortValue(t *testing.T) {
	p, ft, mtid := newPluginWithClient(t)
	ft.recv = [][]byte{
		buildReply(t, mtid+1, codec.MTypeReply, byte(codec.MethodGetValue), codec.GroupFrame, 0, nil),
	}
	if _, err := p.GetDeviceInfo(context.Background()); err == nil {
		t.Fatal("empty frame value: want too-short error")
	}
}

// TestGetSlotInfo_ErrorReplyAndStatus covers the error-reply guard and the
// status-byte extraction plus the walked-tree identity map.
func TestGetSlotInfo_ErrorReplyAndStatus(t *testing.T) {
	// Error reply.
	p, ft, mtid := newPluginWithClient(t)
	p.trees = newSlotTreeCache(defaultCacheConfig().MaxSize, defaultCacheConfig().TTL)
	ft.recv = [][]byte{
		buildReply(t, mtid+1, codec.MTypeError, 3, codec.GroupFrame, 0, nil),
	}
	if _, err := p.GetSlotInfo(context.Background(), 0); err == nil {
		t.Fatal("error reply: want error")
	}

	// Happy path with a status array + a cached tree so the identity map
	// gets allocated.
	p2, ft2, m2 := newPluginWithClient(t)
	p2.trees = newSlotTreeCache(defaultCacheConfig().MaxSize, defaultCacheConfig().TTL)
	p2.SeedTreeFromCachedObjects(2, []consumer.Object{
		{Group: "identity", ID: 0, Label: "Name", Kind: consumer.KindString},
	})
	// frame value = [num_slots, s0, s1, s2, ...]; slot 2 status at index 3.
	ft2.recv = [][]byte{
		buildReply(t, m2+1, codec.MTypeReply, byte(codec.MethodGetValue), codec.GroupFrame, 0,
			[]byte{4, 0, 1, 2, 3}),
	}
	info, err := p2.GetSlotInfo(context.Background(), 2)
	if err != nil {
		t.Fatalf("GetSlotInfo: %v", err)
	}
	if info.Status != consumer.SlotStatus(2) {
		t.Errorf("status = %v, want 2", info.Status)
	}
	if info.Identity == nil {
		t.Error("expected identity map allocated for walked slot")
	}
}

// TestGetValue_ResolveError: a label with no walked tree and no explicit
// (group,id) fails inside resolve.
func TestGetValue_ResolveError(t *testing.T) {
	p, _, _ := newPluginWithClient(t)
	p.trees = newSlotTreeCache(defaultCacheConfig().MaxSize, defaultCacheConfig().TTL)
	if _, err := p.GetValue(context.Background(),
		consumer.ValueRequest{Slot: 0, Label: "Ghost", ID: -1}); err == nil {
		t.Fatal("unresolvable label: want error")
	}
}

// TestSetValue_ResolveError mirrors the GetValue resolve failure.
func TestSetValue_ResolveError(t *testing.T) {
	p, _, _ := newPluginWithClient(t)
	p.trees = newSlotTreeCache(defaultCacheConfig().MaxSize, defaultCacheConfig().TTL)
	if _, err := p.SetValue(context.Background(),
		consumer.ValueRequest{Slot: 0, Label: "Ghost", ID: -1},
		consumer.Value{Kind: consumer.KindInt, Int: 1}); err == nil {
		t.Fatal("unresolvable label: want error")
	}
}

// TestSetValue_FetchMetaError: cold cache + a getObject meta-fetch that
// errors (object error reply) → the wrapped "fetch meta" error.
func TestSetValue_FetchMetaError(t *testing.T) {
	p, ft, mtid := newPluginWithClient(t)
	p.trees = newSlotTreeCache(defaultCacheConfig().MaxSize, defaultCacheConfig().TTL)
	ft.recv = [][]byte{
		buildReply(t, mtid+1, codec.MTypeError, 17, codec.GroupControl, 5, nil),
	}
	if _, err := p.SetValue(context.Background(),
		consumer.ValueRequest{Slot: 0, Group: "control", ID: 5},
		consumer.Value{Kind: consumer.KindInt, Int: 7}); err == nil {
		t.Fatal("fetch-meta failure: want error")
	}
}

// TestSetValue_EncodeError: a walked tree object whose type can't encode
// the supplied value → EncodeValueBytes error.
func TestSetValue_EncodeError(t *testing.T) {
	p, _, _ := newPluginWithClient(t)
	p.trees = newSlotTreeCache(defaultCacheConfig().MaxSize, defaultCacheConfig().TTL)
	// Seed a String object but try to set with an out-of-range string —
	// actually encode an Integer with a non-numeric string to force a
	// coercion error.
	p.SeedTreeFromCachedObjects(0, []consumer.Object{
		{Group: "control", ID: 5, Label: "Level", Kind: consumer.KindInt,
			Access: 0x03, Min: int64(0), Max: int64(10)},
	})
	if _, err := p.SetValue(context.Background(),
		consumer.ValueRequest{Slot: 0, Group: "control", ID: 5},
		consumer.Value{Kind: consumer.KindInt, Str: "not-a-number"}); err == nil {
		t.Fatal("bad encode input: want error")
	}
}

// TestFindObject_ACPTypesShort: a tree whose ACPTypes slice is shorter
// than Objects forces the defensive bound check.
func TestFindObject_ACPTypesShort(t *testing.T) {
	tree := &SlotTree{
		Objects:  []consumer.Object{{Group: "control", ID: 5, Label: "Level"}},
		ACPTypes: nil, // shorter than Objects
	}
	if _, _, found := findObject(tree, codec.GroupControl, 5); found {
		t.Fatal("missing ACPType entry: found should be false")
	}
}
