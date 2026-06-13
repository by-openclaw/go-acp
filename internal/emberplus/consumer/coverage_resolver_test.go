package emberplus

import (
	"testing"

	"dhs/internal/consumer/compliance"
	"dhs/internal/export/canonical"
)

func sp(s string) *string { return &s }

// TestResolveMatrixGain drives parametersLocation resolution (Ember+
// §5.1.2): the pointed-at Node carries targets (1) / sources (2) /
// connections (3) subtrees, each holding gain Parameters. Covers the
// inline absorb branch plus extractParamSubtrees / extractParamMap.
func TestResolveMatrixGain(t *testing.T) {
	// connections subtree (two-deep) — reuse buildConnectionsNode.
	conns := buildConnectionsNode() // number=3, OID 1.2.2.3

	mkParamNode := func(number int, oid string, gain int64) *canonical.Node {
		return &canonical.Node{Header: canonical.Header{
			Number: number, Identifier: "idx", OID: oid,
			IsOnline: true, Access: canonical.AccessRead,
			Children: []canonical.Element{&canonical.Parameter{
				Header: canonical.Header{
					Number: 1, Identifier: "gain", OID: oid + ".1",
					IsOnline: true, Access: canonical.AccessReadWrite,
					Children: canonical.EmptyChildren(),
				},
				Type: canonical.ParamInteger, Value: gain,
			}},
		}}
	}

	targets := &canonical.Node{Header: canonical.Header{
		Number: 1, Identifier: "targets", OID: "1.2.2.1",
		IsOnline: true, Access: canonical.AccessRead,
		Children: []canonical.Element{mkParamNode(0, "1.2.2.1.0", 3)},
	}}
	sources := &canonical.Node{Header: canonical.Header{
		Number: 2, Identifier: "sources", OID: "1.2.2.2",
		IsOnline: true, Access: canonical.AccessRead,
		Children: []canonical.Element{mkParamNode(0, "1.2.2.2.0", 7)},
	}}
	base := &canonical.Node{Header: canonical.Header{
		Number: 2, Identifier: "paramsLoc", OID: "1.2.2",
		IsOnline: true, Access: canonical.AccessRead,
		Children: []canonical.Element{targets, sources, conns},
	}}

	m := &canonical.Matrix{
		Header: canonical.Header{
			Number: 1, Identifier: "mat", OID: "1.2.1",
			IsOnline: true, Access: canonical.AccessReadWrite,
			Children: canonical.EmptyChildren(),
		},
		Type:               canonical.MatrixNToN,
		ParametersLocation: sp("1.2.2"),
	}

	elements := map[string]canonical.Element{"1.2.1": m, "1.2.2": base}
	walk(base, func(el canonical.Element) { elements[el.Common().OID] = el })

	p := &Plugin{profile: &compliance.Profile{}}
	p.resolveMatrixGain(m, elements, modeInline)

	if got := m.TargetParams["0"]["gain"]; got != int64(3) {
		t.Errorf("TargetParams[0][gain] = %v, want 3", got)
	}
	if got := m.SourceParams["0"]["gain"]; got != int64(7) {
		t.Errorf("SourceParams[0][gain] = %v, want 7", got)
	}
	if got := m.ConnectionParams["3.3"]["gain"]; got != int64(10) {
		t.Errorf("ConnectionParams[3.3][gain] = %v, want 10", got)
	}
	// inline must absorb the base subtree.
	if _, still := elements["1.2.2"]; still {
		t.Error("inline mode must absorb parametersLocation base node")
	}
	if got := p.profile.Snapshot()[GainAbsorbed]; got != 1 {
		t.Errorf("gain_absorbed = %d, want 1", got)
	}
}

// TestResolveMatrixGain_Pointer is a no-op; TestResolveMatrixGain_Unresolved
// covers the missing-base and wrong-type warn branches.
func TestResolveMatrixGain_EdgeCases(t *testing.T) {
	// pointer mode: no-op.
	m := &canonical.Matrix{ParametersLocation: sp("1.2.2")}
	p := &Plugin{profile: &compliance.Profile{}}
	p.resolveMatrixGain(m, map[string]canonical.Element{}, modePointer)
	if m.TargetParams != nil {
		t.Error("pointer mode must not populate")
	}

	// nil parametersLocation: early return.
	m2 := &canonical.Matrix{}
	p.resolveMatrixGain(m2, map[string]canonical.Element{}, modeInline)

	// unresolved pointer (not in map): warn event.
	m3 := &canonical.Matrix{ParametersLocation: sp("9.9.9")}
	p.resolveMatrixGain(m3, map[string]canonical.Element{}, modeInline)
	if got := p.profile.Snapshot()[MatrixParametersLocationUnresolved]; got != 1 {
		t.Errorf("unresolved (missing) = %d, want 1", got)
	}

	// pointer resolves but to a non-Node (a Parameter): warn event.
	bad := &canonical.Parameter{Header: canonical.Header{OID: "1.2.2"}}
	m4 := &canonical.Matrix{ParametersLocation: sp("1.2.2")}
	p.resolveMatrixGain(m4, map[string]canonical.Element{"1.2.2": bad}, modeInline)
	if got := p.profile.Snapshot()[MatrixParametersLocationUnresolved]; got != 2 {
		t.Errorf("unresolved (wrong type) = %d, want 2", got)
	}
}

// TestResolveTemplates drives templateReference inflation (Ember+ §8)
// for a Node and a Parameter, plus the unresolved (missing ref) and
// cross-type (Node ref → Parameter template) branches. Exercises
// resolveTemplates + inflateTemplate.
func TestResolveTemplates(t *testing.T) {
	// Parameter template: type + unit copied into a value-only referrer.
	paramTpl := &canonical.Parameter{
		Header: canonical.Header{Number: 1, Identifier: "tpl-p", OID: "10.1"},
		Type:   canonical.ParamInteger,
		Unit:   sp("dB"),
		Minimum: int64(0), Maximum: int64(100),
	}
	// Node template: children + description copied.
	nodeTpl := &canonical.Node{
		Header: canonical.Header{
			Number: 2, Identifier: "tpl-n", OID: "10.2",
			Description: sp("from template"),
			Children: []canonical.Element{&canonical.Parameter{
				Header: canonical.Header{Number: 1, Identifier: "child", OID: "10.2.1"},
				Type:   canonical.ParamString, Value: "x",
			}},
		},
	}

	referrerParam := &canonical.Parameter{
		Header: canonical.Header{
			Number: 1, Identifier: "p", OID: "1.1",
			Children: canonical.EmptyChildren(),
		},
		TemplateReference: sp("10.1"),
		Value:             int64(5),
	}
	referrerNode := &canonical.Node{
		Header: canonical.Header{
			Number: 2, Identifier: "n", OID: "1.2",
			Children: canonical.EmptyChildren(),
		},
		TemplateReference: sp("10.2"),
	}
	// Cross-type: a Node referring to a Parameter template — illegal.
	crossNode := &canonical.Node{
		Header: canonical.Header{
			Number: 3, Identifier: "cross", OID: "1.3",
			Children: canonical.EmptyChildren(),
		},
		TemplateReference: sp("10.1"),
	}
	// Dangling: ref points at an OID with no template.
	danglingNode := &canonical.Node{
		Header: canonical.Header{
			Number: 4, Identifier: "dangling", OID: "1.4",
			Children: canonical.EmptyChildren(),
		},
		TemplateReference: sp("99.99"),
	}
	// No ref: skipped silently.
	plainNode := &canonical.Node{
		Header: canonical.Header{Number: 5, Identifier: "plain", OID: "1.5"},
	}

	elements := map[string]canonical.Element{
		"1.1": referrerParam, "1.2": referrerNode,
		"1.3": crossNode, "1.4": danglingNode, "1.5": plainNode,
	}
	templates := []*canonical.TemplateEntry{
		{Number: 1, OID: "10.1", Identifier: "tpl-p", Template: paramTpl},
		{Number: 2, OID: "10.2", Identifier: "tpl-n", Template: nodeTpl},
		nil, // tolerated: skipped by the t!=nil && t.OID!="" guard
		{Number: 3, OID: "", Identifier: "no-oid", Template: paramTpl},
	}

	p := &Plugin{profile: &compliance.Profile{}}
	p.resolveTemplates(elements, templates, modeInline)

	if referrerParam.Type != canonical.ParamInteger || referrerParam.Unit == nil || *referrerParam.Unit != "dB" {
		t.Errorf("param inflate failed: type=%v unit=%v", referrerParam.Type, referrerParam.Unit)
	}
	if referrerParam.Value != int64(5) {
		t.Errorf("param value must stay referrer's: %v", referrerParam.Value)
	}
	if len(referrerNode.Children) != 1 || referrerNode.Description == nil {
		t.Errorf("node inflate failed: children=%d desc=%v", len(referrerNode.Children), referrerNode.Description)
	}

	snap := p.profile.Snapshot()
	if got := snap[TemplateAbsorbed]; got != 2 {
		t.Errorf("template_absorbed = %d, want 2", got)
	}
	// crossNode (incompatible type) + danglingNode (missing) → 2 unresolved.
	if got := snap[TemplateReferenceUnresolved]; got != 2 {
		t.Errorf("template_reference_unresolved = %d, want 2", got)
	}

	// pointer mode is a no-op.
	p2 := &Plugin{profile: &compliance.Profile{}}
	pn := &canonical.Parameter{Header: canonical.Header{OID: "1.1"}, TemplateReference: sp("10.1")}
	els := map[string]canonical.Element{"1.1": pn}
	p2.resolveTemplates(els, templates, modePointer)
	if pn.Type != "" {
		t.Error("pointer mode must not inflate")
	}
}

// TestInflateTemplate_TypeMismatch covers the false-return paths where
// dst and tpl are different concrete element types, plus the Matrix dst
// (unsupported → false).
func TestInflateTemplate_TypeMismatch(t *testing.T) {
	node := &canonical.Node{Header: canonical.Header{OID: "1.1"}}
	param := &canonical.Parameter{Header: canonical.Header{OID: "10.1"}}
	if inflateTemplate(node, param) {
		t.Error("Node dst + Parameter tpl must return false")
	}
	if inflateTemplate(param, node) {
		t.Error("Parameter dst + Node tpl must return false")
	}
	mtx := &canonical.Matrix{Header: canonical.Header{OID: "1.2"}}
	if inflateTemplate(mtx, mtx) {
		t.Error("Matrix dst is unsupported → false")
	}
}
