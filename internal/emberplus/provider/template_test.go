package emberplus

import (
	"dhs/internal/plugin"
	"testing"

	"dhs/internal/emberplus/codec/glow"
	"dhs/internal/export/canonical"
)

// TestEncodeQualifiedTemplate_ParameterPrototype builds a server with a
// single Template whose prototype is a Parameter and asserts the
// round-trip wire shape (path + non-qualified Parameter prototype +
// description) matches what the consumer decodes.
//
// Spec p.84: QualifiedTemplate [APPLICATION 25] carries CTX[0] path,
// CTX[1] TemplateElement CHOICE, CTX[2] description; the inner element
// uses its own APP tag with CTX[0]=number.
func TestEncodeQualifiedTemplate_ParameterPrototype(t *testing.T) {
	desc := "Gain prototype"
	proto := &canonical.Parameter{
		Header: canonical.Header{
			Number: 1, Identifier: "gainProto", Path: "templates.gainProto",
			OID: "9.1", IsOnline: true, Access: canonical.AccessReadWrite,
			Children: canonical.EmptyChildren(),
		},
		Type:    canonical.ParamReal,
		Value:   float64(0.0),
		Default: float64(0.0),
	}
	te := &canonical.TemplateEntry{
		Number:      1,
		OID:         "9.1",
		Identifier:  "gainProto",
		Description: &desc,
		Template:    proto,
	}
	root := &canonical.Node{
		Header: canonical.Header{
			Number: 1, Identifier: "router", Path: "router", OID: "1",
			IsOnline: true, Access: canonical.AccessRead,
			Children: canonical.EmptyChildren(),
		},
	}
	srv := newServer(plugin.Deps{}, &canonical.Export{Root: root, Templates: []*canonical.TemplateEntry{te}})
	if srv.tree == nil {
		t.Fatal("tree failed to build")
	}

	reply, err := srv.encodeGetDirReply(srv.tree.rootEntry(), false)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	els, err := glow.DecodeRoot(reply)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Root has no children; only the template should appear in the reply.
	if len(els) != 1 {
		t.Fatalf("want 1 element (Template), got %d: %+v", len(els), els)
	}
	tmpl := els[0].Template
	if tmpl == nil {
		t.Fatalf("expected a Template, got %+v", els[0])
	}
	if !tmpl.Qualified {
		t.Errorf("Qualified flag = false, want true (APP[25])")
	}
	if len(tmpl.Path) != 2 || tmpl.Path[0] != 9 || tmpl.Path[1] != 1 {
		t.Errorf("Template Path = %v, want [9 1]", tmpl.Path)
	}
	if tmpl.Description != desc {
		t.Errorf("Description = %q, want %q", tmpl.Description, desc)
	}
	if tmpl.Element == nil || tmpl.Element.Parameter == nil {
		t.Fatalf("Template.Element.Parameter missing — got %+v", tmpl.Element)
	}
	gp := tmpl.Element.Parameter
	if gp.Number != 1 {
		t.Errorf("inner Parameter.Number = %d, want 1 (non-qualified CTX[0])", gp.Number)
	}
	if gp.Identifier != "gainProto" {
		t.Errorf("inner Parameter.Identifier = %q, want gainProto", gp.Identifier)
	}
	if gp.Type != glow.ParamTypeReal {
		t.Errorf("inner Parameter.Type = %d, want %d (real)", gp.Type, glow.ParamTypeReal)
	}
}

// TestEncodeQualifiedTemplate_NodePrototype mirrors the Parameter test
// for a Node prototype — every TemplateElement CHOICE arm must round-trip.
func TestEncodeQualifiedTemplate_NodePrototype(t *testing.T) {
	schema := "com.lawo.routing/1"
	proto := &canonical.Node{
		Header: canonical.Header{
			Number: 2, Identifier: "matrixProto", Path: "templates.matrixProto",
			OID: "9.2", IsOnline: true, Access: canonical.AccessRead,
			Children: canonical.EmptyChildren(),
		},
		SchemaIdentifiers: &schema,
	}
	te := &canonical.TemplateEntry{
		Number: 2, OID: "9.2", Identifier: "matrixProto", Template: proto,
	}
	root := &canonical.Node{
		Header: canonical.Header{
			Number: 1, Identifier: "router", Path: "router", OID: "1",
			IsOnline: true, Access: canonical.AccessRead,
			Children: canonical.EmptyChildren(),
		},
	}
	srv := newServer(plugin.Deps{}, &canonical.Export{Root: root, Templates: []*canonical.TemplateEntry{te}})

	reply, err := srv.encodeGetDirReply(srv.tree.rootEntry(), false)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	els, err := glow.DecodeRoot(reply)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(els) != 1 || els[0].Template == nil || els[0].Template.Element == nil {
		t.Fatalf("template missing or empty: %+v", els)
	}
	n := els[0].Template.Element.Node
	if n == nil {
		t.Fatalf("Template.Element.Node missing")
	}
	if n.Number != 2 || n.Identifier != "matrixProto" {
		t.Errorf("inner Node = number %d id %q, want 2 matrixProto", n.Number, n.Identifier)
	}
	if n.SchemaIdentifiers != schema {
		t.Errorf("inner Node.SchemaIdentifiers = %q, want %q", n.SchemaIdentifiers, schema)
	}
}

// TestRootReply_MixesChildrenAndTemplates asserts that when the root
// node carries both children and templates, the GetDirectory reply at
// root level returns them as siblings in one RootElementCollection.
func TestRootReply_MixesChildrenAndTemplates(t *testing.T) {
	param := &canonical.Parameter{
		Header: canonical.Header{
			Number: 1, Identifier: "gain", Path: "router.gain", OID: "1.1",
			IsOnline: true, Access: canonical.AccessReadWrite,
			Children: canonical.EmptyChildren(),
		},
		Type: canonical.ParamInteger, Value: int64(0),
	}
	root := &canonical.Node{
		Header: canonical.Header{
			Number: 1, Identifier: "router", Path: "router", OID: "1",
			IsOnline: true, Access: canonical.AccessRead,
			Children: []canonical.Element{param},
		},
	}
	te := &canonical.TemplateEntry{
		Number: 1, OID: "9.1", Identifier: "gainProto",
		Template: &canonical.Parameter{
			Header: canonical.Header{
				Number: 1, Identifier: "gainProto", IsOnline: true,
				Access: canonical.AccessReadWrite, Children: canonical.EmptyChildren(),
			},
			Type: canonical.ParamReal, Value: float64(0),
		},
	}
	srv := newServer(plugin.Deps{}, &canonical.Export{Root: root, Templates: []*canonical.TemplateEntry{te}})

	reply, err := srv.encodeGetDirReply(srv.tree.rootEntry(), false)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	els, err := glow.DecodeRoot(reply)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(els) != 2 {
		t.Fatalf("want 2 root items (Parameter + Template), got %d: %+v", len(els), els)
	}
	// Children come before templates per encoder order.
	if els[0].Parameter == nil {
		t.Errorf("els[0] should be Parameter; got %+v", els[0])
	}
	if els[1].Template == nil {
		t.Errorf("els[1] should be Template; got %+v", els[1])
	}
}
