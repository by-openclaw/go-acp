package acp1

import (
	"context"
	"testing"

	"dhs/internal/acp1/codec"
	"dhs/internal/consumer"
)

// rootObject builds a getObject(Root) reply body with the given per-group
// counts. Layout per spec p.21: type, num_props, access, boot_mode,
// num_identity, num_control, num_status, num_alarm, num_file.
func rootObject(numID, numCtl, numStat, numAlarm uint8) []byte {
	return []byte{0x00, 0x09, 0x01, 0x00, numID, numCtl, numStat, numAlarm, 0x00}
}

func walkPlugin(t *testing.T) (*Plugin, *fakeTransport, uint32) {
	t.Helper()
	p, ft, mtid := newPluginWithClient(t)
	p.walker = NewWalker(p.client)
	p.trees = newSlotTreeCache(defaultCacheConfig().MaxSize, defaultCacheConfig().TTL)
	return p, ft, mtid
}

func TestWalk_FullSequence(t *testing.T) {
	p, ft, mtid := walkPlugin(t)
	// root: 1 identity + 1 control.
	ft.recv = [][]byte{
		buildReply(t, mtid+1, codec.MTypeReply, byte(codec.MethodGetObject), codec.GroupRoot, 0, rootObject(1, 1, 0, 0)),
		buildReply(t, mtid+2, codec.MTypeReply, byte(codec.MethodGetObject), codec.GroupIdentity, 0, stringObject("RRS18", "Card Label")),
		buildReply(t, mtid+3, codec.MTypeReply, byte(codec.MethodGetObject), codec.GroupControl, 0, integerObject(5, "Level")),
	}
	objs, err := p.Walk(context.Background(), 1)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(objs) != 2 {
		t.Fatalf("Walk returned %d objects, want 2", len(objs))
	}
	// Second Walk hits the cache (no further wire traffic needed).
	sentBefore := len(ft.sent)
	if _, err := p.Walk(context.Background(), 1); err != nil {
		t.Fatalf("second Walk: %v", err)
	}
	if len(ft.sent) != sentBefore {
		t.Errorf("cached Walk still sent frames: before=%d after=%d", sentBefore, len(ft.sent))
	}
}

func TestWalk_SlotOutOfRange(t *testing.T) {
	p, _, _ := walkPlugin(t)
	if _, err := p.Walk(context.Background(), 99); err == nil {
		t.Error("Walk slot 99: want out-of-range error")
	}
}

func TestWalk_RootError(t *testing.T) {
	p, ft, mtid := walkPlugin(t)
	ft.recv = [][]byte{
		buildReply(t, mtid+1, codec.MTypeError, 3 /* transaction-timeout */, codec.GroupRoot, 0, nil),
	}
	if _, err := p.Walk(context.Background(), 0); err == nil {
		t.Error("Walk with root error reply: want error")
	}
}

func TestWalk_NotConnected(t *testing.T) {
	p := &Plugin{}
	if _, err := p.Walk(context.Background(), 0); err != consumer.ErrNotConnected {
		t.Fatalf("err = %v, want ErrNotConnected", err)
	}
}

// TestGetValue_TypedFromCachedTree drives the warm-cache GetValue path: a prior
// Walk populated the tree, so GetValue decodes against the cached integer type
// without a meta fetch.
func TestGetValue_TypedFromCachedTree(t *testing.T) {
	p, ft, mtid := walkPlugin(t)
	ft.recv = [][]byte{
		buildReply(t, mtid+1, codec.MTypeReply, byte(codec.MethodGetObject), codec.GroupRoot, 0, rootObject(0, 1, 0, 0)),
		buildReply(t, mtid+2, codec.MTypeReply, byte(codec.MethodGetObject), codec.GroupControl, 0, integerObject(0, "Level")),
		// getValue on control[0] → int16 = 7.
		buildReply(t, mtid+3, codec.MTypeReply, byte(codec.MethodGetValue), codec.GroupControl, 0, []byte{0x00, 0x07}),
	}
	if _, err := p.Walk(context.Background(), 0); err != nil {
		t.Fatalf("Walk: %v", err)
	}
	v, err := p.GetValue(context.Background(), consumer.ValueRequest{Slot: 0, Group: "control", ID: 0})
	if err != nil {
		t.Fatalf("GetValue: %v", err)
	}
	if v.Kind != consumer.KindInt || v.Int != 7 {
		t.Errorf("GetValue = %+v, want KindInt 7", v)
	}
}
