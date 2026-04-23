package emberplus

import (
	"context"
	"testing"

	"acp/internal/export/canonical"
	"acp/internal/emberplus/codec/glow"
)

// TestRoundTrip_NodeWithParameter asserts the provider encoder and the
// consumer decoder agree on the wire shape for a trivial tree:
//
//	router (Node OID=1) → Gain (Parameter OID=1.1, Integer 42, RW)
//
// The encoder's GetDirectory reply for the root must decode back through
// the consumer's glow.DecodeRoot into a tree with the same identifiers,
// OIDs, access bits, and value.
func TestRoundTrip_NodeWithParameter(t *testing.T) {
	desc := "test gain"
	gain := &canonical.Parameter{
		Header: canonical.Header{
			Number:      1,
			Identifier:  "gain",
			Path:        "router.gain",
			OID:         "1.1",
			Description: &desc,
			IsOnline:    true,
			Access:      canonical.AccessReadWrite,
			Children:    canonical.EmptyChildren(),
		},
		Type:  canonical.ParamInteger,
		Value: int64(42),
	}
	root := &canonical.Node{
		Header: canonical.Header{
			Number:     1,
			Identifier: "router",
			Path:       "router",
			OID:        "1",
			IsOnline:   true,
			Access:     canonical.AccessRead,
			Children:   []canonical.Element{gain},
		},
	}
	exp := &canonical.Export{Root: root}

	srv := newServer(nil, exp)
	if srv.tree == nil {
		t.Fatal("tree failed to build")
	}

	// Request bareRoot=false: after initial root reply, consumer issues
	// GetDirectory on path=[1] → we reply with children flat.
	reply, err := srv.encodeGetDirReply(srv.tree.rootEntry(), false)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	els, err := glow.DecodeRoot(reply)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Spec pattern: GetDirectory on path=[1] returns children flat at the
	// RootElementCollection level. Here the only child is the "gain"
	// QualifiedParameter, so we expect exactly one element and it to be
	// a Parameter at path [1,1].
	if len(els) != 1 {
		t.Fatalf("want 1 element, got %d: %+v", len(els), els)
	}
	p := els[0].Parameter
	if p == nil {
		t.Fatalf("expected a QualifiedParameter, got %+v", els[0])
	}
	if p.Identifier != "gain" {
		t.Errorf("param identifier = %q, want gain", p.Identifier)
	}
	if p.Value != int64(42) {
		t.Errorf("param value = %v (%T), want 42", p.Value, p.Value)
	}
	if p.Access != glow.AccessReadWrite {
		t.Errorf("param access = %d, want %d", p.Access, glow.AccessReadWrite)
	}
	if p.Type != glow.ParamTypeInteger {
		t.Errorf("param type = %d, want %d", p.Type, glow.ParamTypeInteger)
	}
	// Path should be [1,1]
	if len(p.Path) != 2 || p.Path[0] != 1 || p.Path[1] != 1 {
		t.Errorf("param path = %v, want [1 1]", p.Path)
	}
}

func TestSetValueBroadcast_UpdatesTree(t *testing.T) {
	p := &canonical.Parameter{
		Header: canonical.Header{
			Number: 1, Identifier: "gain", Path: "router.gain", OID: "1.1",
			IsOnline: true, Access: canonical.AccessReadWrite,
			Children: canonical.EmptyChildren(),
		},
		Type:  canonical.ParamInteger,
		Value: int64(0),
	}
	root := &canonical.Node{
		Header: canonical.Header{
			Number: 1, Identifier: "router", Path: "router", OID: "1",
			IsOnline: true, Access: canonical.AccessRead,
			Children: []canonical.Element{p},
		},
	}
	srv := newServer(nil, &canonical.Export{Root: root})

	if _, err := srv.SetValue(context.Background(), "1.1", int64(99)); err != nil {
		t.Fatalf("SetValue: %v", err)
	}
	updated := srv.tree.byOID["1.1"].el.(*canonical.Parameter)
	if updated.Value != int64(99) {
		t.Errorf("tree value = %v, want 99", updated.Value)
	}
}
