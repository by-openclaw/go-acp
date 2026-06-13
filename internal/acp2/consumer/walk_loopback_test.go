package acp2

import (
	"context"
	"testing"

	"dhs/internal/consumer"

	"dhs/internal/acp2/codec"
)

// walk_loopback_test.go drives Plugin.Walk + the Walker DFS against the
// loopback harness. The fake tree:
//
//	obj 1 ROOT_NODE_V2 (node) → children [2, 3]
//	  obj 2 BOARD (node)       → children [4]
//	    obj 4 Card Name (string) = "CONVERT Hybrid"
//	  obj 3 Volume (number u32) = 42
//
// The walker tries obj-id 1 first (real-device convention), so the
// server roots the tree at 1.

func walkTreeServer(t *testing.T) (*fakeServer, string, int) {
	srv, host, port := newFakeServer(t)
	srv.onACP2 = func(slot uint8, req *codec.ACP2Message) *codec.ACP2Message {
		if req.Func != codec.ACP2FuncGetObject {
			return errorReply(codec.ErrProtocol)
		}
		switch req.ObjID {
		case 1:
			return objectReply(1, []codec.Property{
				u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeNode)),
				codec.MakeStringProperty(codec.PIDLabel, "ROOT_NODE_V2"),
				childrenProp(2, 3),
			})
		case 2:
			return objectReply(2, []codec.Property{
				u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeNode)),
				codec.MakeStringProperty(codec.PIDLabel, "BOARD"),
				childrenProp(4),
			})
		case 3:
			return objectReply(3, []codec.Property{
				u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeNumber)),
				codec.MakeStringProperty(codec.PIDLabel, "Volume"),
				u32Prop(codec.PIDNumberType, 0, uint32(codec.NumTypeU32)),
				codec.MakeValueProperty(codec.PIDValue, codec.NumTypeU32, []byte{0, 0, 0, 42}),
				u32Prop(codec.PIDAccess, 0, 3),
			})
		case 4:
			return objectReply(4, []codec.Property{
				u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeString)),
				codec.MakeStringProperty(codec.PIDLabel, "Card Name"),
				codec.MakeStringProperty(codec.PIDValue, "CONVERT Hybrid"),
			})
		default:
			return errorReply(codec.ErrInvalidObjID)
		}
	}
	return srv, host, port
}

func TestWalk_FullTree(t *testing.T) {
	srv, host, port := walkTreeServer(t)
	defer srv.stop()

	p := connectPlugin(t, srv, host, port)

	objs, err := p.Walk(context.Background(), 1)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(objs) != 4 {
		t.Fatalf("walked %d objects, want 4", len(objs))
	}

	byLabel := map[string]consumer.Object{}
	for _, o := range objs {
		byLabel[o.Label] = o
	}

	root, ok := byLabel["ROOT_NODE_V2"]
	if !ok {
		t.Fatal("ROOT_NODE_V2 not in walk")
	}
	if root.ID != 1 {
		t.Errorf("root ID = %d, want 1", root.ID)
	}

	vol, ok := byLabel["Volume"]
	if !ok {
		t.Fatal("Volume not walked")
	}
	if vol.Kind != consumer.KindUint || vol.Value.Uint != 42 {
		t.Errorf("Volume = %+v, want uint 42", vol.Value)
	}

	card, ok := byLabel["Card Name"]
	if !ok {
		t.Fatal("Card Name not walked")
	}
	if card.Kind != consumer.KindString || card.Value.Str != "CONVERT Hybrid" {
		t.Errorf("Card Name = %+v, want string CONVERT Hybrid", card.Value)
	}
	// Path should be ROOT_NODE_V2 → BOARD → Card Name.
	if len(card.Path) != 3 || card.Path[1] != "BOARD" {
		t.Errorf("Card Name path = %v, want [ROOT_NODE_V2 BOARD Card Name]", card.Path)
	}
}

func TestWalk_Cached(t *testing.T) {
	srv, host, port := walkTreeServer(t)
	defer srv.stop()

	p := connectPlugin(t, srv, host, port)

	first, err := p.Walk(context.Background(), 1)
	if err != nil {
		t.Fatalf("Walk 1: %v", err)
	}
	// Second Walk returns the cached tree without re-fetching.
	second, err := p.Walk(context.Background(), 1)
	if err != nil {
		t.Fatalf("Walk 2: %v", err)
	}
	if len(first) != len(second) {
		t.Errorf("cached walk len mismatch: %d vs %d", len(first), len(second))
	}
}

func TestWalk_NotConnected(t *testing.T) {
	p := &Plugin{logger: testLogger()}
	p.trees = newWalkedTreeCache(8, 0)
	if _, err := p.Walk(context.Background(), 0); err != consumer.ErrNotConnected {
		t.Errorf("err = %v, want ErrNotConnected", err)
	}
}

func TestWalk_RootFallbackToZero(t *testing.T) {
	// Server only knows obj-id 0 as the root; obj-id 1 errors. The
	// walker must fall back from 1 to 0.
	srv, host, port := newFakeServer(t)
	srv.onACP2 = func(slot uint8, req *codec.ACP2Message) *codec.ACP2Message {
		if req.Func != codec.ACP2FuncGetObject {
			return errorReply(codec.ErrProtocol)
		}
		switch req.ObjID {
		case 0:
			return objectReply(0, []codec.Property{
				u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeNode)),
				codec.MakeStringProperty(codec.PIDLabel, "ROOT"),
			})
		default:
			return errorReply(codec.ErrInvalidObjID)
		}
	}
	defer srv.stop()

	p := connectPlugin(t, srv, host, port)
	objs, err := p.Walk(context.Background(), 0)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(objs) != 1 || objs[0].Label != "ROOT" {
		t.Fatalf("walk = %+v, want single ROOT", objs)
	}
}

func TestWalk_RootError(t *testing.T) {
	// Both obj-id 1 and 0 error → Walk returns an error.
	srv, host, port := newFakeServer(t)
	srv.onACP2 = func(slot uint8, req *codec.ACP2Message) *codec.ACP2Message {
		return errorReply(codec.ErrInvalidObjID)
	}
	defer srv.stop()

	p := connectPlugin(t, srv, host, port)
	if _, err := p.Walk(context.Background(), 0); err == nil {
		t.Fatal("Walk with no valid root: want error")
	}
}

func TestIdentityProbe(t *testing.T) {
	srv, host, port := newFakeServer(t)
	srv.onACP2 = func(slot uint8, req *codec.ACP2Message) *codec.ACP2Message {
		if req.Func != codec.ACP2FuncGetObject {
			return errorReply(codec.ErrProtocol)
		}
		switch req.ObjID {
		case 1:
			return objectReply(1, []codec.Property{
				u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeNode)),
				codec.MakeStringProperty(codec.PIDLabel, "ROOT_NODE_V2"),
				childrenProp(2),
			})
		case 2:
			return objectReply(2, []codec.Property{
				u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeNode)),
				codec.MakeStringProperty(codec.PIDLabel, "IDENTITY"),
				childrenProp(10, 11),
			})
		case 10:
			return objectReply(10, []codec.Property{
				u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeString)),
				codec.MakeStringProperty(codec.PIDLabel, "Card Name"),
				codec.MakeStringProperty(codec.PIDValue, "CONVERT Hybrid"),
			})
		case 11:
			return objectReply(11, []codec.Property{
				u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeString)),
				codec.MakeStringProperty(codec.PIDLabel, "Product Version"),
				codec.MakeStringProperty(codec.PIDValue, "5.3.5"),
			})
		default:
			return errorReply(codec.ErrInvalidObjID)
		}
	}
	defer srv.stop()

	p := connectPlugin(t, srv, host, port)
	id, err := p.IdentityProbe(context.Background(), 1)
	if err != nil {
		t.Fatalf("IdentityProbe: %v", err)
	}
	if id != "CONVERT Hybrid@5.3.5" {
		t.Errorf("identity = %q, want CONVERT Hybrid@5.3.5", id)
	}
}

func TestIdentityProbe_NotConnected(t *testing.T) {
	p := &Plugin{logger: testLogger()}
	if _, err := p.IdentityProbe(context.Background(), 0); err != consumer.ErrNotConnected {
		t.Errorf("err = %v, want ErrNotConnected", err)
	}
}
