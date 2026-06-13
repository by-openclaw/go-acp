package acp1

import (
	"context"
	"errors"
	"testing"

	"dhs/internal/acp1/codec"
	"dhs/internal/consumer"
)

// TestFetchObjectMeta_ErrorPaths covers the not-connected, Do-error, and
// decode-error arms of fetchObjectMeta.
func TestFetchObjectMeta_ErrorPaths(t *testing.T) {
	// Not connected.
	p := &Plugin{}
	if _, _, err := p.fetchObjectMeta(context.Background(), 0, codec.GroupControl, 5); err != consumer.ErrNotConnected {
		t.Fatalf("not connected: got %v", err)
	}

	// Do error (empty recv → io.EOF).
	p2, _, _ := newPluginWithClient(t)
	if _, _, err := p2.fetchObjectMeta(context.Background(), 0, codec.GroupControl, 5); err == nil {
		t.Fatal("Do error: want error")
	}

	// Decode error: a valid reply whose Value can't decode as an object.
	p3, ft3, m3 := newPluginWithClient(t)
	ft3.recv = [][]byte{
		buildReply(t, m3+1, codec.MTypeReply, byte(codec.MethodGetObject), codec.GroupControl, 5, []byte{0xFF}),
	}
	if _, _, err := p3.fetchObjectMeta(context.Background(), 0, codec.GroupControl, 5); err == nil {
		t.Fatal("decode error: want error")
	}
}

// TestValidateValue_ResolveAndGroupErrors covers the resolve-failure arm
// and the group-not-exist + generic meta-fetch error arms.
func TestValidateValue_ResolveAndGroupErrors(t *testing.T) {
	// Resolve failure: label-only with no walked tree.
	p, _, _ := newPluginWithClient(t)
	p.trees = newSlotTreeCache(defaultCacheConfig().MaxSize, defaultCacheConfig().TTL)
	if err := p.ValidateValue(context.Background(),
		consumer.ValueRequest{Slot: 0, Label: "Ghost", ID: -1},
		consumer.Value{Kind: consumer.KindInt, Int: 1}); !errors.Is(err, consumer.ErrValidationFailed) {
		t.Fatalf("resolve fail: want ErrValidationFailed, got %v", err)
	}

	// Group-not-exist (stat=16) → ErrObjectNotFound.
	pg, ftg, mg := newPluginWithClient(t)
	pg.trees = newSlotTreeCache(defaultCacheConfig().MaxSize, defaultCacheConfig().TTL)
	ftg.recv = [][]byte{
		buildReply(t, mg+1, codec.MTypeError, 16 /* group not exist */, codec.GroupControl, 5, nil),
	}
	if err := pg.ValidateValue(context.Background(),
		consumer.ValueRequest{Slot: 0, Group: "control", ID: 5},
		consumer.Value{Kind: consumer.KindInt, Int: 1}); !errors.Is(err, consumer.ErrObjectNotFound) {
		t.Fatalf("group-not-exist: want ErrObjectNotFound, got %v", err)
	}

	// Generic meta-fetch failure (transport error MCODE<16) →
	// ErrValidationFailed.
	pf, ftf, mf := newPluginWithClient(t)
	pf.trees = newSlotTreeCache(defaultCacheConfig().MaxSize, defaultCacheConfig().TTL)
	ftf.recv = [][]byte{
		buildReply(t, mf+1, codec.MTypeError, 3 /* transaction timeout */, codec.GroupControl, 5, nil),
	}
	if err := pf.ValidateValue(context.Background(),
		consumer.ValueRequest{Slot: 0, Group: "control", ID: 5},
		consumer.Value{Kind: consumer.KindInt, Int: 1}); !errors.Is(err, consumer.ErrValidationFailed) {
		t.Fatalf("generic meta fail: want ErrValidationFailed, got %v", err)
	}
}

// TestGetValue_ColdCacheMetaSuccess drives GetValue's cold-cache path where
// the getValue read succeeds AND the follow-up getObject meta fetch also
// succeeds, so DecodeValueBytes uses the discovered type (line ~596).
func TestGetValue_ColdCacheMetaSuccess(t *testing.T) {
	p, ft, mtid := newPluginWithClient(t)
	p.trees = newSlotTreeCache(defaultCacheConfig().MaxSize, defaultCacheConfig().TTL)
	ft.recv = [][]byte{
		// 1) getValue(control,5) → i16 value bytes.
		buildReply(t, mtid+1, codec.MTypeReply, byte(codec.MethodGetValue), codec.GroupControl, 5, []byte{0x00, 0x2A}),
		// 2) getObject(control,5) meta → Integer object.
		buildReply(t, mtid+2, codec.MTypeReply, byte(codec.MethodGetObject), codec.GroupControl, 5, integerObject(0, "Level")),
	}
	v, err := p.GetValue(context.Background(), consumer.ValueRequest{Slot: 0, Group: "control", ID: 5})
	if err != nil {
		t.Fatalf("GetValue cold-cache meta: %v", err)
	}
	if v.Kind != consumer.KindInt || v.Int != 42 {
		t.Fatalf("decoded value = %+v, want Int=42", v)
	}
}
