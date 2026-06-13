package acp1

import (
	"context"
	"testing"

	"dhs/internal/acp1/codec"
	"dhs/internal/consumer"
)

// fanoutClient is a fake clientIface that also implements announceFanout.
// It captures the registered RawEventFunc so tests can drive announcements
// straight through the Plugin's filter+wrapper without a real socket.
type fanoutClient struct {
	fn       RawEventFunc
	removed  []int
	nextH    int
}

func (f *fanoutClient) Do(ctx context.Context, req *codec.Message) (*codec.Message, error) {
	return nil, context.DeadlineExceeded
}
func (f *fanoutClient) Close() error { return nil }
func (f *fanoutClient) AddListener(fn RawEventFunc) int {
	f.fn = fn
	f.nextH++
	return f.nextH
}
func (f *fanoutClient) RemoveListener(h int) { f.removed = append(f.removed, h) }

// TestSubscribe_FanoutFilters drives the TCP/AN2 fanout filter arms: a
// slot mismatch, a group mismatch, an id mismatch (all rejected) and a
// fully-matching announcement (delivered).
func TestSubscribe_FanoutFilters(t *testing.T) {
	fc := &fanoutClient{}
	p, _, _ := newPluginWithClient(t)
	p.client = fc
	p.subHandles = map[subKey]SubHandle{}
	p.trees = newSlotTreeCache(defaultCacheConfig().MaxSize, defaultCacheConfig().TTL)
	p.SeedTreeFromCachedObjects(2, []consumer.Object{
		{Group: "control", ID: 0, Label: "Level", Kind: consumer.KindInt},
	})

	delivered := make(chan consumer.Event, 8)
	if err := p.Subscribe(consumer.ValueRequest{Slot: 2, Group: "control", ID: 0},
		func(ev consumer.Event) { delivered <- ev }); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if fc.fn == nil {
		t.Fatal("fanout listener not registered")
	}

	mk := func(slot byte, g codec.ObjGroup, id byte, val []byte) *codec.Message {
		return &codec.Message{MType: codec.MTypeAnnounce, MAddr: slot, ObjGroup: g, ObjID: id, Value: val}
	}

	// Slot mismatch → rejected (line ~830).
	fc.fn(mk(9, codec.GroupControl, 0, []byte{0, 7}))
	// Group mismatch → rejected (line ~833).
	fc.fn(mk(2, codec.GroupStatus, 0, []byte{0, 7}))
	// ID mismatch → rejected (line ~836).
	fc.fn(mk(2, codec.GroupControl, 5, []byte{0, 7}))

	select {
	case ev := <-delivered:
		t.Fatalf("non-matching announcement delivered: %+v", ev)
	default:
	}

	// Full match → delivered.
	fc.fn(mk(2, codec.GroupControl, 0, []byte{0, 7}))
	select {
	case ev := <-delivered:
		if ev.Value.Int != 7 {
			t.Errorf("delivered value = %d, want 7", ev.Value.Int)
		}
	default:
		t.Fatal("matching announcement not delivered")
	}

	// Unsubscribe routes through the fanout RemoveListener path.
	if err := p.Unsubscribe(consumer.ValueRequest{Slot: 2, Group: "control", ID: 0}); err != nil {
		t.Fatalf("Unsubscribe: %v", err)
	}
	if len(fc.removed) != 1 {
		t.Errorf("RemoveListener calls = %d, want 1", len(fc.removed))
	}
}

// TestSubscribe_WrapperDecodeError drives the wrapper's DecodeValueBytes
// error arm (line ~806): a tree object whose type can't decode the
// announced bytes falls back to a raw value.
func TestSubscribe_WrapperDecodeError(t *testing.T) {
	fc := &fanoutClient{}
	p, _, _ := newPluginWithClient(t)
	p.client = fc
	p.subHandles = map[subKey]SubHandle{}
	p.trees = newSlotTreeCache(defaultCacheConfig().MaxSize, defaultCacheConfig().TTL)
	// IPAddr object needs 4 value bytes; announce only 1 → decode error.
	p.SeedTreeFromCachedObjects(0, []consumer.Object{
		{Group: "control", ID: 1, Label: "IP", Kind: consumer.KindIPAddr},
	})

	got := make(chan consumer.Event, 1)
	if err := p.Subscribe(consumer.ValueRequest{Slot: 0, Group: "control", ID: 1},
		func(ev consumer.Event) { got <- ev }); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	fc.fn(&codec.Message{MType: codec.MTypeAnnounce, MAddr: 0, ObjGroup: codec.GroupControl, ObjID: 1, Value: []byte{0x01}})
	select {
	case ev := <-got:
		if ev.Value.Kind != consumer.KindRaw {
			t.Errorf("decode-error fallback kind = %v, want KindRaw", ev.Value.Kind)
		}
	default:
		t.Fatal("event not delivered")
	}
}
