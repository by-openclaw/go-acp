package acp1

import (
	"context"
	"testing"

	"dhs/internal/acp1/codec"
	"dhs/internal/consumer"
)

// mutatePlugin wires a Plugin with a fake client and a seeded tree (Level is a
// writable Integer) so the no-arg mutating methods decode the confirmed reply
// from the cached type without a meta-fetch round-trip.
func mutatePlugin(t *testing.T) (*Plugin, *fakeTransport, uint32) {
	t.Helper()
	p, ft, mtid := newPluginWithClient(t)
	p.trees = newSlotTreeCache(defaultCacheConfig().MaxSize, defaultCacheConfig().TTL)
	p.SeedTreeFromCachedObjects(0, []consumer.Object{
		{Group: "control", ID: 0, Label: "Level", Kind: consumer.KindInt, Access: 0x03},
	})
	return p, ft, mtid
}

func TestSetIncDecDef(t *testing.T) {
	cases := []struct {
		name   string
		method byte
		call   func(p *Plugin, ctx context.Context, req consumer.ValueRequest) (consumer.Value, error)
		echo   []byte // device-confirmed int16
		want   int64
	}{
		{"inc", byte(codec.MethodSetIncValue), (*Plugin).SetIncValue, []byte{0x00, 0x06}, 6},
		{"dec", byte(codec.MethodSetDecValue), (*Plugin).SetDecValue, []byte{0x00, 0x04}, 4},
		{"def", byte(codec.MethodSetDefValue), (*Plugin).SetDefValue, []byte{0x00, 0x00}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, ft, mtid := mutatePlugin(t)
			ft.recv = [][]byte{
				buildReply(t, mtid+1, codec.MTypeReply, c.method, codec.GroupControl, 0, c.echo),
			}
			v, err := c.call(p, context.Background(), consumer.ValueRequest{Slot: 0, Group: "control", ID: 0})
			if err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}
			if v.Int != c.want {
				t.Errorf("%s confirmed = %d, want %d", c.name, v.Int, c.want)
			}
			// Exactly one request frame: the method itself (no meta fetch).
			if len(ft.sent) != 1 {
				t.Errorf("%s sent %d frames, want 1", c.name, len(ft.sent))
			}
		})
	}
}

func TestSetInc_NotConnected(t *testing.T) {
	p := &Plugin{}
	if _, err := p.SetIncValue(context.Background(), consumer.ValueRequest{Slot: 0, Group: "control", ID: 0}); err != consumer.ErrNotConnected {
		t.Fatalf("err = %v, want ErrNotConnected", err)
	}
}

func TestSetInc_ErrorReply(t *testing.T) {
	p, ft, mtid := mutatePlugin(t)
	// Device rejects inc on this object (illegal-for-type).
	ft.recv = [][]byte{
		buildReply(t, mtid+1, codec.MTypeError, 24 /* illegal method for type */, codec.GroupControl, 0, nil),
	}
	if _, err := p.SetIncValue(context.Background(), consumer.ValueRequest{Slot: 0, Group: "control", ID: 0}); err == nil {
		t.Fatal("SetIncValue with error reply: want error")
	}
}

// TestSetDef_ColdCache exercises the meta-fetch fallback when no tree is
// cached: a getObject precedes the setDef reply.
func TestSetDef_ColdCache(t *testing.T) {
	p, ft, mtid := newPluginWithClient(t) // no seeded tree
	ft.recv = [][]byte{
		buildReply(t, mtid+1, codec.MTypeReply, byte(codec.MethodSetDefValue), codec.GroupControl, 5, []byte{0x00, 0x00}),
		buildReply(t, mtid+2, codec.MTypeReply, byte(codec.MethodGetObject), codec.GroupControl, 5, integerObject(0, "Level")),
	}
	v, err := p.SetDefValue(context.Background(), consumer.ValueRequest{Slot: 0, Group: "control", ID: 5})
	if err != nil {
		t.Fatalf("SetDefValue cold cache: %v", err)
	}
	if v.Int != 0 {
		t.Errorf("confirmed = %d, want 0", v.Int)
	}
}
