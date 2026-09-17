package emberplus

import (
	"context"
	"dhs/internal/plugin"
	"testing"

	"dhs/internal/emberplus/codec/glow"
	"dhs/internal/export/canonical"
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

	srv := newServer(plugin.Deps{}, exp)
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

// TestEncodeNode_DTD230Fields asserts the Node encoder emits the four
// NodeContents fields introduced by DTD 2.30 (spec p.87):
//
//	[3]  isOnline          — only when false (default true)
//	[4]  schemaIdentifiers — newline-separated string
//	[5]  templateReference — RELATIVE-OID
//
// isRoot [2] is always omitted: only the conceptual tree root would set
// it, and modern viewers infer root from path=[].
func TestEncodeNode_DTD230Fields(t *testing.T) {
	schema := "com.lawo.routing/1"
	tmpl := "9.1"
	child := &canonical.Node{
		Header: canonical.Header{
			Number: 1, Identifier: "primary", Path: "router.primary", OID: "1.1",
			IsOnline: false, Access: canonical.AccessRead,
			Children: canonical.EmptyChildren(),
		},
		SchemaIdentifiers: &schema,
		TemplateReference: &tmpl,
	}
	root := &canonical.Node{
		Header: canonical.Header{
			Number: 1, Identifier: "router", Path: "router", OID: "1",
			IsOnline: true, Access: canonical.AccessRead,
			Children: []canonical.Element{child},
		},
	}
	srv := newServer(plugin.Deps{}, &canonical.Export{Root: root})

	reply, err := srv.encodeGetDirReply(srv.tree.rootEntry(), false)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	els, err := glow.DecodeRoot(reply)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(els) != 1 || els[0].Node == nil {
		t.Fatalf("want 1 Node, got %+v", els)
	}
	n := els[0].Node
	if n.IsOnline {
		t.Errorf("isOnline = true, want false (was explicit on canonical)")
	}
	if n.SchemaIdentifiers != schema {
		t.Errorf("schemaIdentifiers = %q, want %q", n.SchemaIdentifiers, schema)
	}
	if len(n.TemplateReference) != 2 || n.TemplateReference[0] != 9 || n.TemplateReference[1] != 1 {
		t.Errorf("templateReference = %v, want [9 1]", n.TemplateReference)
	}
}

// TestEncodeParameter_DTD230Fields asserts the Parameter encoder emits
// the four ParameterContents fields introduced by DTD 2.30 (spec p.85):
//
//	[9]  isOnline          — only when false (default true)
//	[16] streamDescriptor  — StreamDescription [APP 12]
//	[17] schemaIdentifiers
//	[18] templateReference
func TestEncodeParameter_DTD230Fields(t *testing.T) {
	schema := "com.lawo.gain/1"
	tmpl := "9.2"
	p := &canonical.Parameter{
		Header: canonical.Header{
			Number: 1, Identifier: "gain", Path: "router.gain", OID: "1.1",
			IsOnline: false, Access: canonical.AccessReadWrite,
			Children: canonical.EmptyChildren(),
		},
		Type:              canonical.ParamReal,
		Value:             float64(-12.0),
		StreamDescriptor:  &canonical.StreamDescriptor{Format: int(glow.StreamFmtFloat32LittleEndian), Offset: 8},
		SchemaIdentifiers: &schema,
		TemplateReference: &tmpl,
	}
	root := &canonical.Node{
		Header: canonical.Header{
			Number: 1, Identifier: "router", Path: "router", OID: "1",
			IsOnline: true, Access: canonical.AccessRead,
			Children: []canonical.Element{p},
		},
	}
	srv := newServer(plugin.Deps{}, &canonical.Export{Root: root})

	reply, err := srv.encodeGetDirReply(srv.tree.rootEntry(), false)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	els, err := glow.DecodeRoot(reply)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(els) != 1 || els[0].Parameter == nil {
		t.Fatalf("want 1 Parameter, got %+v", els)
	}
	gp := els[0].Parameter
	if gp.IsOnline {
		t.Errorf("isOnline = true, want false")
	}
	if gp.StreamDescriptor == nil {
		t.Fatalf("streamDescriptor missing")
	}
	if gp.StreamDescriptor.Format != glow.StreamFmtFloat32LittleEndian {
		t.Errorf("streamDescriptor.format = %d, want %d", gp.StreamDescriptor.Format, glow.StreamFmtFloat32LittleEndian)
	}
	if gp.StreamDescriptor.Offset != 8 {
		t.Errorf("streamDescriptor.offset = %d, want 8", gp.StreamDescriptor.Offset)
	}
	if gp.SchemaIdentifiers != schema {
		t.Errorf("schemaIdentifiers = %q, want %q", gp.SchemaIdentifiers, schema)
	}
	if len(gp.TemplateReference) != 2 || gp.TemplateReference[0] != 9 || gp.TemplateReference[1] != 2 {
		t.Errorf("templateReference = %v, want [9 2]", gp.TemplateReference)
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
	srv := newServer(plugin.Deps{}, &canonical.Export{Root: root})

	if _, err := srv.SetValue(context.Background(), "1.1", int64(99)); err != nil {
		t.Fatalf("SetValue: %v", err)
	}
	updated := srv.tree.byOID["1.1"].el.(*canonical.Parameter)
	if updated.Value != int64(99) {
		t.Errorf("tree value = %v, want 99", updated.Value)
	}
}
