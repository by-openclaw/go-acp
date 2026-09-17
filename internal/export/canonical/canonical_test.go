package canonical

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// TestUnmarshalElement_Dispatch — the key-set rules from
// docs/protocols/elements/*.md: arguments → function, targets →
// matrix, type → parameter, otherwise node, checked in that order.
func TestUnmarshalElement_Dispatch(t *testing.T) {
	cases := []struct{ name, in, wantKind string }{
		{"arguments makes a function", `{"oid":"1.1","arguments":[],"result":[]}`, "function"},
		{"targets makes a matrix", `{"oid":"1.2","targets":[],"sources":[]}`, "matrix"},
		{"type makes a parameter", `{"oid":"1.3","type":"integer","value":1}`, "parameter"},
		{"nothing special makes a node", `{"oid":"1.4"}`, "node"},
		{"arguments outrank targets and type", `{"oid":"1.5","arguments":[],"targets":[],"type":"x"}`, "function"},
		{"targets outrank type", `{"oid":"1.6","targets":[],"type":"x"}`, "matrix"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			el, err := UnmarshalElement([]byte(c.in))
			if err != nil {
				t.Fatalf("UnmarshalElement: %v", err)
			}
			if el.Kind() != c.wantKind {
				t.Errorf("kind got %s, want %s", el.Kind(), c.wantKind)
			}
			if el.Common().OID == "" {
				t.Error("Common() must expose the parsed header")
			}
		})
	}
}

// TestUnmarshalElement_Errors — an unparseable element is refused,
// not coerced (root CLAUDE.md spec-strict posture), and the error
// names the type and the child index so the operator can find it.
func TestUnmarshalElement_Errors(t *testing.T) {
	cases := []struct{ name, in, wantPrefix string }{
		{"not an object", `[1]`, "peek:"},
		{"function with wrong field type", `{"arguments":[],"number":"x"}`, "function:"},
		{"matrix with wrong field type", `{"targets":[],"number":"x"}`, "matrix:"},
		{"parameter with wrong field type", `{"type":"integer","number":"x"}`, "parameter:"},
		{"node with wrong field type", `{"number":"x"}`, "node:"},
		{"function with bad child", `{"arguments":[],"children":[[1]]}`, "function: child[0]: peek:"},
		{"matrix with bad child", `{"targets":[],"children":[[1]]}`, "matrix: child[0]: peek:"},
		{"parameter with bad child", `{"type":"integer","children":[[1]]}`, "parameter: child[0]: peek:"},
		{"node with bad second child", `{"children":[{"oid":"1"},[1]]}`, "node: child[1]: peek:"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := UnmarshalElement([]byte(c.in))
			if err == nil || !strings.HasPrefix(err.Error(), c.wantPrefix) {
				t.Errorf("got %v, want prefix %q", err, c.wantPrefix)
			}
		})
	}
	t.Run("type errors stay inspectable through the wrap", func(t *testing.T) {
		var ute *json.UnmarshalTypeError
		if _, err := UnmarshalElement([]byte(`{"type":"integer","number":"x"}`)); !errors.As(err, &ute) {
			t.Errorf("want *json.UnmarshalTypeError, got %v", err)
		}
	})
}

// fullTree is a documented-shape sample: a node carrying one of each
// child type, every optional field populated, with nested children
// under each so the recursive dispatch is exercised per type.
const fullTree = `{
  "number": 1, "identifier": "root", "path": "root", "oid": "1", "description": "Root", "isOnline": true, "access": "read",
  "children": [
    {"number": 1, "identifier": "gain", "path": "root.gain", "oid": "1.1", "description": null, "isOnline": true, "access": "readWrite",
     "type": "real", "value": -6.5, "default": 0, "minimum": -90, "maximum": 12, "step": 0.5,
     "unit": "dB", "format": "%.1f", "factor": 10, "formula": "x/10", "enumeration": null,
     "enumMap": [{"key": "Off", "value": 0}, {"key": "Hidden", "value": 1, "masked": true}],
     "streamIdentifier": 7, "streamDescriptor": {"format": 12, "offset": 4},
     "templateReference": "9.1", "schemaIdentifiers": "acme.gain",
     "children": [{"number": 1, "identifier": "sub", "path": "root.gain.sub", "oid": "1.1.1", "access": "read", "type": "string", "value": "x"}]},
    {"number": 2, "identifier": "sw", "path": "root.sw", "oid": "1.2", "access": "readWrite",
     "type": "oneToN", "mode": "linear", "targetCount": 2, "sourceCount": 2,
     "maximumTotalConnects": null, "maximumConnectsPerTarget": 1, "parametersLocation": "1.3", "gainParameterNumber": 1,
     "labels": [{"basePath": "1.2.1", "description": "Primary", "nameSize": 8, "multiLine": true, "padChar": 0, "keepPadding": true}],
     "targets": [{"number": 0}, {"number": 1}], "sources": [{"number": 0}, {"number": 1}],
     "connections": [{"target": 0, "sources": [1], "operation": "absolute", "disposition": "tally", "locked": false}],
     "targetLabels": {"Primary": {"0": "OUT 1", "1": "OUT 2"}}, "sourceLabels": null,
     "targetParams": {}, "sourceParams": null, "connectionParams": null,
     "templateReference": null, "schemaIdentifiers": "acme.matrix",
     "children": [{"number": 1, "identifier": "labels", "path": "root.sw.labels", "oid": "1.2.1", "access": "read"}]},
    {"number": 3, "identifier": "reset", "path": "root.reset", "oid": "1.3", "access": "none",
     "arguments": [{"name": "hard", "type": "boolean"}], "result": [{"name": "ok", "type": "boolean"}],
     "children": [{"number": 1, "identifier": "note", "path": "root.reset.note", "oid": "1.3.1", "access": "read"}]}
  ],
  "templateReference": "9.2", "schemaIdentifiers": "acme.root"
}`

func parseFullTree(t *testing.T) (*Node, *Parameter, *Matrix, *Function) {
	t.Helper()
	el, err := UnmarshalElement([]byte(fullTree))
	if err != nil {
		t.Fatalf("UnmarshalElement: %v", err)
	}
	n, ok := el.(*Node)
	if !ok || len(n.Children) != 3 {
		t.Fatalf("want *Node with 3 children, got %T %+v", el, el)
	}
	p, ok1 := n.Children[0].(*Parameter)
	m, ok2 := n.Children[1].(*Matrix)
	f, ok3 := n.Children[2].(*Function)
	if !ok1 || !ok2 || !ok3 {
		t.Fatalf("children kinds: %T %T %T", n.Children[0], n.Children[1], n.Children[2])
	}
	return n, p, m, f
}

// TestNode_Unmarshal — header, node-only pointers, and typed children.
func TestNode_Unmarshal(t *testing.T) {
	n, _, _, _ := parseFullTree(t)
	if n.Number != 1 || n.Identifier != "root" || n.Path != "root" || n.OID != "1" || !n.IsOnline || n.Access != AccessRead {
		t.Errorf("header: %+v", n.Header)
	}
	if n.Description == nil || *n.Description != "Root" {
		t.Errorf("description: %v", n.Description)
	}
	if n.TemplateReference == nil || *n.TemplateReference != "9.2" || n.SchemaIdentifiers == nil || *n.SchemaIdentifiers != "acme.root" {
		t.Errorf("node pointers: %v %v", n.TemplateReference, n.SchemaIdentifiers)
	}
	if n.Common() != &n.Header {
		t.Error("Common() must return the embedded header")
	}
}

// TestParameter_Unmarshal — every spec p.85 field survives, JSON
// scalars become their natural Go types, null and absent both read
// as nil, and nested children are dispatched.
func TestParameter_Unmarshal(t *testing.T) {
	_, p, _, _ := parseFullTree(t)
	if p.Kind() != "parameter" || p.OID != "1.1" || p.Access != AccessReadWrite || p.Description != nil {
		t.Errorf("header: %+v", p.Header)
	}
	if p.Type != ParamReal || p.Value != float64(-6.5) || p.Default != float64(0) || p.Minimum != float64(-90) || p.Maximum != float64(12) || p.Step != float64(0.5) {
		t.Errorf("numeric fields: type=%s value=%v default=%v min=%v max=%v step=%v", p.Type, p.Value, p.Default, p.Minimum, p.Maximum, p.Step)
	}
	if *p.Unit != "dB" || *p.Format != "%.1f" || *p.Factor != 10 || *p.Formula != "x/10" || p.Enumeration != nil {
		t.Errorf("string fields: %+v", p)
	}
	if len(p.EnumMap) != 2 || p.EnumMap[1] != (EnumEntry{Key: "Hidden", Value: 1, Masked: true}) {
		t.Errorf("enumMap: %+v", p.EnumMap)
	}
	if *p.StreamIdentifier != 7 || p.StreamDescriptor == nil || *p.StreamDescriptor != (StreamDescriptor{Format: StreamIEEEFloat32BE, Offset: 4}) {
		t.Errorf("stream: %v %+v", p.StreamIdentifier, p.StreamDescriptor)
	}
	if *p.TemplateReference != "9.1" || *p.SchemaIdentifiers != "acme.gain" {
		t.Errorf("pointers: %v %v", p.TemplateReference, p.SchemaIdentifiers)
	}
	sub, ok := p.Children[0].(*Parameter)
	if !ok || sub.Value != "x" || sub.Default != nil {
		t.Errorf("nested child: %+v", p.Children)
	}
	// doc.go rule 2: a decoded leaf carries an empty children slice,
	// never nil, so re-encoding cannot turn it into "children": null.
	if sub.Children == nil || len(sub.Children) != 0 {
		t.Errorf("leaf children = %#v, want empty non-nil", sub.Children)
	}
}

// TestMatrix_Unmarshal — classification, limits, pointer-form labels
// with their name-encoding fields, declared targets/sources,
// connections, and the mode-gated inline maps (null vs {} preserved).
func TestMatrix_Unmarshal(t *testing.T) {
	_, _, m, _ := parseFullTree(t)
	if m.Kind() != "matrix" || m.OID != "1.2" || m.Type != MatrixOneToN || m.Mode != ModeLinear || m.TargetCount != 2 || m.SourceCount != 2 {
		t.Errorf("classification: %+v", m)
	}
	if m.MaximumTotalConnects != nil || *m.MaximumConnectsPerTarget != 1 || *m.ParametersLocation != "1.3" || *m.GainParameterNumber != 1 {
		t.Errorf("limits: %v %v %v %v", m.MaximumTotalConnects, m.MaximumConnectsPerTarget, m.ParametersLocation, m.GainParameterNumber)
	}
	if len(m.Labels) != 1 {
		t.Fatalf("labels: %+v", m.Labels)
	}
	l := m.Labels[0]
	if l.BasePath != "1.2.1" || *l.Description != "Primary" || l.NameSize != 8 || !l.MultiLine || l.PadChar == nil || *l.PadChar != 0 || !l.KeepPadding {
		t.Errorf("label: %+v", l)
	}
	if len(m.Targets) != 2 || m.Targets[1].Number != 1 || len(m.Sources) != 2 {
		t.Errorf("targets/sources: %+v %+v", m.Targets, m.Sources)
	}
	if len(m.Connections) != 1 || m.Connections[0].Target != 0 || len(m.Connections[0].Sources) != 1 || m.Connections[0].Operation != ConnOpAbsolute || m.Connections[0].Disposition != ConnDispTally {
		t.Errorf("connections: %+v", m.Connections)
	}
	if m.TargetLabels["Primary"]["1"] != "OUT 2" || m.SourceLabels != nil || m.TargetParams == nil || len(m.TargetParams) != 0 || m.SourceParams != nil || m.ConnectionParams != nil {
		t.Errorf("inline maps: %+v %+v %+v", m.TargetLabels, m.SourceLabels, m.TargetParams)
	}
	if m.TemplateReference != nil || *m.SchemaIdentifiers != "acme.matrix" {
		t.Errorf("pointers: %v %v", m.TemplateReference, m.SchemaIdentifiers)
	}
	if len(m.Children) != 1 || m.Children[0].Kind() != "node" || m.Children[0].Common().OID != "1.2.1" {
		t.Errorf("children: %+v", m.Children)
	}
}

// TestFunction_Unmarshal — tuple signature and (schema-permitted)
// children.
func TestFunction_Unmarshal(t *testing.T) {
	_, _, _, f := parseFullTree(t)
	if f.Kind() != "function" || f.OID != "1.3" || f.Access != AccessNone {
		t.Errorf("header: %+v", f.Header)
	}
	if len(f.Arguments) != 1 || f.Arguments[0] != (TupleItem{Name: "hard", Type: ParamBoolean}) || len(f.Result) != 1 || f.Result[0].Name != "ok" {
		t.Errorf("signature: %+v %+v", f.Arguments, f.Result)
	}
	if len(f.Children) != 1 || f.Children[0].Common().Path != "root.reset.note" {
		t.Errorf("children: %+v", f.Children)
	}
}

// TestMatrixLabel_Effective — the schema→codec defaults: NameSize 0
// defers to the request frame's fallback; a nil PadChar is ASCII
// space, and an explicit 0x00 is honoured as NUL (not "unset").
func TestMatrixLabel_Effective(t *testing.T) {
	nul := uint8(0)
	cases := []struct {
		name     string
		label    MatrixLabel
		fallback uint8
		wantSize uint8
		wantPad  uint8
	}{
		{"defaults", MatrixLabel{}, 8, 8, 0x20},
		{"explicit size wins", MatrixLabel{NameSize: 12}, 8, 12, 0x20},
		{"explicit NUL pad", MatrixLabel{PadChar: &nul}, 4, 4, 0x00},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.label.EffectiveNameSize(c.fallback); got != c.wantSize {
				t.Errorf("EffectiveNameSize got %d, want %d", got, c.wantSize)
			}
			if got := c.label.EffectivePadChar(); got != c.wantPad {
				t.Errorf("EffectivePadChar got %#x, want %#x", got, c.wantPad)
			}
		})
	}
}

// TestStreamFormatName — Ember+ spec §5.3 StreamFormat ids 0..15 and
// the "unknown" sentinel for anything outside the table.
func TestStreamFormatName(t *testing.T) {
	want := map[int]string{
		StreamUnsignedInt8: "unsignedInt8", StreamUnsignedInt16BE: "unsignedInt16BE", StreamUnsignedInt16LE: "unsignedInt16LE",
		StreamUnsignedInt32BE: "unsignedInt32BE", StreamUnsignedInt32LE: "unsignedInt32LE",
		StreamUnsignedInt64BE: "unsignedInt64BE", StreamUnsignedInt64LE: "unsignedInt64LE",
		StreamSignedInt8: "signedInt8", StreamSignedInt16BE: "signedInt16BE", StreamSignedInt16LE: "signedInt16LE",
		StreamSignedInt32BE: "signedInt32BE", StreamSignedInt32LE: "signedInt32LE",
		StreamIEEEFloat32BE: "ieeeFloat32BE", StreamIEEEFloat32LE: "ieeeFloat32LE",
		StreamIEEEFloat64BE: "ieeeFloat64BE", StreamIEEEFloat64LE: "ieeeFloat64LE",
		16: "unknown", -1: "unknown",
	}
	for id, name := range want {
		if got := StreamFormatName(id); got != name {
			t.Errorf("StreamFormatName(%d) = %q, want %q", id, got, name)
		}
	}
}

// TestTemplateEntry_Unmarshal — the embedded template is dispatched
// like any child; failures name the template by identifier.
func TestTemplateEntry_Unmarshal(t *testing.T) {
	t.Run("embedded element is typed", func(t *testing.T) {
		var te TemplateEntry
		in := `{"number":1,"oid":"9.1","identifier":"chan","description":"Channel","template":{"oid":"9.1","identifier":"chan","type":"real","value":null}}`
		if err := json.Unmarshal([]byte(in), &te); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if te.Number != 1 || te.OID != "9.1" || te.Identifier != "chan" || *te.Description != "Channel" || te.Template.Kind() != "parameter" {
			t.Errorf("entry: %+v", te)
		}
	})
	cases := []struct{ name, in, wantPrefix string }{
		{"wrong field type", `{"number":"x"}`, "json:"},
		{"template not an element", `{"identifier":"t","template":[1]}`, `template "t": peek:`},
		{"template missing", `{"identifier":"t"}`, `template "t": peek:`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var te TemplateEntry
			err := json.Unmarshal([]byte(c.in), &te)
			if err == nil || !strings.HasPrefix(err.Error(), c.wantPrefix) {
				t.Errorf("got %v, want prefix %q", err, c.wantPrefix)
			}
		})
	}
}

// TestExport_Unmarshal — the top-level envelope: root dispatched,
// templates carried, both optional.
func TestExport_Unmarshal(t *testing.T) {
	t.Run("root and templates", func(t *testing.T) {
		var x Export
		in := `{"root":{"oid":"1","targets":[]},"templates":[{"number":1,"oid":"9.1","identifier":"t","template":{"oid":"9.1"}}]}`
		if err := json.Unmarshal([]byte(in), &x); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if x.Root.Kind() != "matrix" || len(x.Templates) != 1 || x.Templates[0].Template.Kind() != "node" {
			t.Errorf("export: %+v", x)
		}
	})
	t.Run("empty envelope has no root", func(t *testing.T) {
		var x Export
		if err := json.Unmarshal([]byte(`{}`), &x); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if x.Root != nil || x.Templates != nil {
			t.Errorf("want zero export, got %+v", x)
		}
	})
	cases := []struct{ name, in, wantPrefix string }{
		{"templates not an array", `{"templates":5}`, "json:"},
		{"root not an element", `{"root":[1]}`, "root: peek:"},
		{"bad template entry surfaces", `{"templates":[{"identifier":"t","template":[1]}]}`, `template "t": peek:`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var x Export
			err := json.Unmarshal([]byte(c.in), &x)
			if err == nil || !strings.HasPrefix(err.Error(), c.wantPrefix) {
				t.Errorf("got %v, want prefix %q", err, c.wantPrefix)
			}
		})
	}
}

// TestEmptyChildren — the shared leaf slice is empty but never nil,
// so a leaf built by an exporter is distinguishable from one whose
// children were simply never assigned.
func TestEmptyChildren(t *testing.T) {
	if c := EmptyChildren(); c == nil || len(c) != 0 {
		t.Errorf("EmptyChildren() = %#v, want empty non-nil", c)
	}
}
