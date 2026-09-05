package emberplus

import (
	"testing"

	"dhs/internal/consumer/compliance"
	"dhs/internal/export/canonical"
)

// TestResolve_NilElements covers resolve's nil-map early return.
func TestResolve_NilElements(t *testing.T) {
	p := &Plugin{profile: &compliance.Profile{}}
	p.resolve(nil, nil, CanonicalOptions{Labels: "inline"})
}

// TestResolveMatrixLabels_BasepathNotNode covers the branch where a
// labels[] basePath resolves to a non-Node element.
func TestResolveMatrixLabels_BasepathNotNode(t *testing.T) {
	d := "L"
	m := &canonical.Matrix{Labels: []canonical.MatrixLabel{{BasePath: "1.2", Description: &d}}}
	elements := map[string]canonical.Element{
		"1.2": &canonical.Parameter{Header: canonical.Header{OID: "1.2"}}, // not a Node
	}
	p := &Plugin{profile: &compliance.Profile{}}
	p.resolveMatrixLabels(m, elements, modeInline)
	if got := p.profile.Snapshot()[MatrixLabelBasepathUnresolved]; got != 1 {
		t.Errorf("basepath-not-node should fire unresolved: got %d", got)
	}
}

// TestResolveMatrixLabels_LevelMismatch covers the mismatched() ->
// MatrixLabelLevelMismatch note path: two levels with different target
// label counts.
func TestResolveMatrixLabels_LevelMismatch(t *testing.T) {
	mkLevel := func(oid string, n int) *canonical.Node {
		kids := make([]canonical.Element, 0, n)
		for i := 0; i < n; i++ {
			kids = append(kids, &canonical.Parameter{
				Header: canonical.Header{Number: i}, Type: canonical.ParamString, Value: "x",
			})
		}
		targets := &canonical.Node{Header: canonical.Header{Number: 1, OID: oid + ".1", Children: kids}}
		return &canonical.Node{Header: canonical.Header{OID: oid, Children: []canonical.Element{targets}}}
	}
	d1, d2 := "A", "B"
	m := &canonical.Matrix{Labels: []canonical.MatrixLabel{
		{BasePath: "1.2", Description: &d1},
		{BasePath: "1.3", Description: &d2},
	}}
	elements := map[string]canonical.Element{
		"1.2": mkLevel("1.2", 2),
		"1.3": mkLevel("1.3", 3), // different count → mismatch
	}
	p := &Plugin{profile: &compliance.Profile{}}
	p.resolveMatrixLabels(m, elements, modeBoth)
	if got := p.profile.Snapshot()[MatrixLabelLevelMismatch]; got != 1 {
		t.Errorf("level mismatch should fire: got %d", got)
	}
}

// TestDeleteSubtree_Missing covers deleteSubtree's missing-OID early
// return.
func TestDeleteSubtree_Missing(t *testing.T) {
	deleteSubtree(map[string]canonical.Element{}, "9.9") // no panic, no-op
}

// TestExtractHelpers_SkipBranches drives the "child is not the expected
// type" continue branches of extractTargetSourceNodes,
// extractParamSubtrees, extractLabelMap, extractParamMap, and
// extractConnectionParamMap.
func TestExtractHelpers_SkipBranches(t *testing.T) {
	// A container Node whose children include a Parameter (skipped by
	// the *-Nodes extractors) and Nodes with numbers outside 1/2/3.
	mixed := &canonical.Node{Header: canonical.Header{Children: []canonical.Element{
		&canonical.Parameter{Header: canonical.Header{Number: 9}}, // not a Node → skip
		&canonical.Node{Header: canonical.Header{Number: 1, OID: "1"}},
		&canonical.Node{Header: canonical.Header{Number: 5, OID: "5"}}, // not 1/2/3 → ignored
	}}}
	if tg, _ := extractTargetSourceNodes(mixed); tg == nil {
		t.Error("extractTargetSourceNodes should find number=1")
	}
	if tg, _, _ := extractParamSubtrees(mixed); tg == nil {
		t.Error("extractParamSubtrees should find number=1")
	}

	// extractLabelMap: a Node child (skipped) + a non-string Parameter
	// (skipped) + a string Parameter (kept).
	lblContainer := &canonical.Node{Header: canonical.Header{Children: []canonical.Element{
		&canonical.Node{Header: canonical.Header{Number: 0}},
		&canonical.Parameter{Header: canonical.Header{Number: 1}, Value: int64(5)}, // non-string → skip
		&canonical.Parameter{Header: canonical.Header{Number: 2}, Value: "OUT"},
	}}}
	lm := extractLabelMap(lblContainer)
	if lm["2"] != "OUT" || len(lm) != 1 {
		t.Errorf("extractLabelMap = %v", lm)
	}

	// extractParamMap: a Parameter child (skipped) + a Node whose
	// children include a Node (skipped) and a Parameter (kept).
	pmContainer := &canonical.Node{Header: canonical.Header{Children: []canonical.Element{
		&canonical.Parameter{Header: canonical.Header{Number: 0}}, // not a Node → skip
		&canonical.Node{Header: canonical.Header{Number: 1, Children: []canonical.Element{
			&canonical.Node{Header: canonical.Header{Number: 0}}, // not a Parameter → skip
			&canonical.Parameter{Header: canonical.Header{Number: 1, Identifier: "gain"}, Value: int64(3)},
		}}},
		&canonical.Node{Header: canonical.Header{Number: 2}}, // empty inner → not added
	}}}
	pm := extractParamMap(pmContainer)
	if pm["1"]["gain"] != int64(3) || len(pm) != 1 {
		t.Errorf("extractParamMap = %v", pm)
	}

	// extractConnectionParamMap: a Parameter child (skipped) + a target
	// Node whose child includes a Parameter (skipped) and a source Node.
	connContainer := &canonical.Node{Header: canonical.Header{Children: []canonical.Element{
		&canonical.Parameter{Header: canonical.Header{Number: 0}}, // not a Node → skip
		&canonical.Node{Header: canonical.Header{Number: 1, Children: []canonical.Element{
			&canonical.Parameter{Header: canonical.Header{Number: 0}}, // not a Node → skip
			&canonical.Node{Header: canonical.Header{Number: 2, Children: []canonical.Element{
				&canonical.Parameter{Header: canonical.Header{Number: 1, Identifier: "gain"}, Value: int64(7)},
			}}},
		}}},
	}}}
	cm := extractConnectionParamMap(connContainer)
	if cm["1.2"]["gain"] != int64(7) {
		t.Errorf("extractConnectionParamMap = %v", cm)
	}
}

// TestInflateTemplate_DstAlreadySet covers the false-side of every
// "dst field is zero" guard in inflateTemplate by passing a dst that
// already has every field populated — the template must NOT overwrite.
func TestInflateTemplate_DstAlreadySet(t *testing.T) {
	d := "dst desc"
	u := "dstUnit"
	f := "%x"
	fa := int64(1)
	fo := "dstFormula"
	en := "dstEnum"
	// Parameter dst with everything set.
	dstParam := &canonical.Parameter{
		Header:  canonical.Header{Description: &d},
		Type:    canonical.ParamInteger,
		Default: int64(1), Minimum: int64(0), Maximum: int64(9), Step: int64(1),
		Unit: &u, Format: &f, Factor: &fa, Formula: &fo, Enumeration: &en,
		EnumMap:          []canonical.EnumEntry{{Key: "x", Value: 0}},
		StreamDescriptor: &canonical.StreamDescriptor{Format: 1},
	}
	td := "tpl desc"
	tu := "tplUnit"
	tplParam := &canonical.Parameter{
		Header:  canonical.Header{Description: &td},
		Type:    canonical.ParamReal,
		Default: int64(2), Minimum: int64(-1), Maximum: int64(99), Step: int64(2),
		Unit:             &tu,
		EnumMap:          []canonical.EnumEntry{{Key: "y", Value: 1}},
		StreamDescriptor: &canonical.StreamDescriptor{Format: 2},
	}
	if !inflateTemplate(dstParam, tplParam) {
		t.Fatal("inflateTemplate(param,param) should return true")
	}
	if dstParam.Type != canonical.ParamInteger || *dstParam.Unit != "dstUnit" || dstParam.Minimum != int64(0) {
		t.Errorf("populated dst must not be overwritten: %+v", dstParam)
	}

	// Node dst with description + children + schema already set.
	nd := "node desc"
	ns := "node-sch"
	dstNode := &canonical.Node{
		Header:            canonical.Header{Description: &nd, Children: []canonical.Element{&canonical.Parameter{}}},
		SchemaIdentifiers: &ns,
	}
	tnd := "tpl node"
	tns := "tpl-sch"
	tplNode := &canonical.Node{
		Header:            canonical.Header{Description: &tnd, Children: []canonical.Element{&canonical.Parameter{}, &canonical.Parameter{}}},
		SchemaIdentifiers: &tns,
	}
	if !inflateTemplate(dstNode, tplNode) {
		t.Fatal("inflateTemplate(node,node) should return true")
	}
	if *dstNode.Description != "node desc" || len(dstNode.Children) != 1 || *dstNode.SchemaIdentifiers != "node-sch" {
		t.Errorf("populated node dst must not be overwritten: %+v", dstNode)
	}
}

// TestInflateTemplate_AllFieldsCopied covers the copy-side of every
// "dst field is zero/nil" guard: an empty dst inherits every field from
// a fully-populated template (Parameter and Node).
func TestInflateTemplate_AllFieldsCopied(t *testing.T) {
	td := "tpl"
	tu := "dB"
	tf := "%d"
	tfa := int64(4)
	tfo := "x*2"
	te := "off\non"
	tsch := "tpl-sch"
	tplParam := &canonical.Parameter{
		Header:  canonical.Header{Description: &td},
		Type:    canonical.ParamInteger,
		Default: int64(1), Minimum: int64(0), Maximum: int64(10), Step: int64(1),
		Unit: &tu, Format: &tf, Factor: &tfa, Formula: &tfo, Enumeration: &te,
		EnumMap:           []canonical.EnumEntry{{Key: "off", Value: 0}},
		StreamDescriptor:  &canonical.StreamDescriptor{Format: 1, Offset: 2},
		SchemaIdentifiers: &tsch,
	}
	dst := &canonical.Parameter{} // every field zero/nil
	if !inflateTemplate(dst, tplParam) {
		t.Fatal("inflateTemplate(empty param, full tpl) should return true")
	}
	if dst.Type != canonical.ParamInteger || dst.Unit == nil || *dst.Unit != "dB" ||
		dst.Format == nil || dst.Factor == nil || dst.Formula == nil || dst.Enumeration == nil ||
		dst.Default == nil || dst.Minimum == nil || dst.Maximum == nil || dst.Step == nil ||
		len(dst.EnumMap) == 0 || dst.StreamDescriptor == nil || dst.Description == nil {
		t.Errorf("not all fields copied into empty dst: %+v", dst)
	}

	tnd := "tpl node"
	tnsch := "tpl-node-sch"
	tplNode := &canonical.Node{
		Header:            canonical.Header{Description: &tnd, Children: []canonical.Element{&canonical.Parameter{}}},
		SchemaIdentifiers: &tnsch,
	}
	dn := &canonical.Node{}
	if !inflateTemplate(dn, tplNode) {
		t.Fatal("inflateTemplate(empty node, full tpl) should return true")
	}
	if dn.Description == nil || len(dn.Children) == 0 || dn.SchemaIdentifiers == nil {
		t.Errorf("node fields not copied: %+v", dn)
	}
}

// TestRemoveFromTree_NoParent covers removeFromTree's missing-parent and
// root (empty parent OID) continue branches.
func TestRemoveFromTree_NoParent(t *testing.T) {
	elements := map[string]canonical.Element{
		"1":   &canonical.Node{Header: canonical.Header{OID: "1"}},   // root: parentOID == ""
		"9.9": &canonical.Node{Header: canonical.Header{OID: "9.9"}}, // parent "9" absent
	}
	removeFromTree(elements, []string{"1", "9.9"})
	if _, ok := elements["1"]; ok {
		t.Error("root OID should be deleted from the map")
	}
	if _, ok := elements["9.9"]; ok {
		t.Error("orphan OID should be deleted from the map")
	}
}
