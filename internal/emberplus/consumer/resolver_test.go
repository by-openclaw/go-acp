package emberplus

import (
	"testing"

	"acp/internal/export/canonical"
	"acp/internal/protocol/compliance"
)

// TestResolveMatrixLabels_MultiLevel exercises the N>=2 case that the
// wire captures on 10.6.239.113 cannot reach (both TinyEmberPlus
// providers ship a single "Primary" level). The synthetic tree here
// carries two label levels — Primary and Long — under separate
// basePaths, mirroring the smh emulator's shape
// (internal/emberplus/assets/smh/emulator/ember-server/src/data-model-new.ts).
//
// Expectations under --labels=inline:
//   - TargetLabels/SourceLabels keyed by labels[i].description
//   - Both labels Node subtrees absorbed (removed from elements map)
//   - labels_absorbed fires once (per matrix, not per level)
func TestResolveMatrixLabels_MultiLevel(t *testing.T) {
	m, elements := buildMultiLevelMatrixTree()
	p := &Plugin{profile: &compliance.Profile{}}

	p.resolveMatrixLabels(m, elements, modeInline)

	if len(m.TargetLabels) != 2 {
		t.Fatalf("want 2 levels in TargetLabels, got %d (keys: %v)", len(m.TargetLabels), keysOf(m.TargetLabels))
	}
	if got, want := m.TargetLabels["Primary"]["0"], "OUT 1"; got != want {
		t.Errorf("TargetLabels[Primary][0] = %q, want %q", got, want)
	}
	if got, want := m.TargetLabels["Primary"]["1"], "OUT 2"; got != want {
		t.Errorf("TargetLabels[Primary][1] = %q, want %q", got, want)
	}
	if got, want := m.TargetLabels["Long"]["0"], "Output Main"; got != want {
		t.Errorf("TargetLabels[Long][0] = %q, want %q", got, want)
	}

	if got, want := m.SourceLabels["Primary"]["0"], "IN 1"; got != want {
		t.Errorf("SourceLabels[Primary][0] = %q, want %q", got, want)
	}
	if got, want := m.SourceLabels["Long"]["1"], "Input Backup"; got != want {
		t.Errorf("SourceLabels[Long][1] = %q, want %q", got, want)
	}

	// Absorption: the Labels Node subtrees (1.2 and 1.3) and all their
	// descendants must be removed from the elements map.
	for _, gone := range []string{"1.2", "1.2.1", "1.2.2", "1.2.1.0", "1.2.1.1", "1.2.2.0", "1.2.2.1", "1.3", "1.3.1", "1.3.2"} {
		if _, stillThere := elements[gone]; stillThere {
			t.Errorf("elements[%q] should have been absorbed and removed", gone)
		}
	}

	// Compliance event should fire exactly once per matrix.
	snap := p.profile.Snapshot()
	if got := snap[LabelsAbsorbed]; got != 1 {
		t.Errorf("labels_absorbed count = %d, want 1", got)
	}
}

// TestResolveMatrixLabels_Pointer ensures pointer mode is a no-op:
// the labels[] array stays exactly as delivered, no absorption, no
// resolved maps. Wire-faithful shape.
func TestResolveMatrixLabels_Pointer(t *testing.T) {
	m, elements := buildMultiLevelMatrixTree()
	beforeCount := len(elements)
	p := &Plugin{profile: &compliance.Profile{}}

	p.resolveMatrixLabels(m, elements, modePointer)

	if m.TargetLabels != nil {
		t.Errorf("pointer mode must not populate TargetLabels; got %v", m.TargetLabels)
	}
	if m.SourceLabels != nil {
		t.Errorf("pointer mode must not populate SourceLabels; got %v", m.SourceLabels)
	}
	if len(elements) != beforeCount {
		t.Errorf("pointer mode must not mutate elements; before=%d after=%d", beforeCount, len(elements))
	}
	if got := p.profile.Snapshot()[LabelsAbsorbed]; got != 0 {
		t.Errorf("pointer mode must not fire labels_absorbed; got %d", got)
	}
}

// TestResolveMatrixLabels_Both keeps the source subtrees in the tree
// AND populates the resolved maps. Useful for debug / round-trip
// fidelity.
func TestResolveMatrixLabels_Both(t *testing.T) {
	m, elements := buildMultiLevelMatrixTree()
	beforeCount := len(elements)
	p := &Plugin{profile: &compliance.Profile{}}

	p.resolveMatrixLabels(m, elements, modeBoth)

	if len(m.TargetLabels) != 2 {
		t.Errorf("both mode must populate TargetLabels; got %d levels", len(m.TargetLabels))
	}
	if len(elements) != beforeCount {
		t.Errorf("both mode must keep source subtrees; before=%d after=%d", beforeCount, len(elements))
	}
	if got := p.profile.Snapshot()[LabelsAbsorbed]; got != 0 {
		t.Errorf("both mode must not fire labels_absorbed; got %d", got)
	}
}

// TestResolveMatrixLabels_BasepathUnresolved covers the warn case
// where a provider ships a labels[] entry pointing at a path it
// never walked. The resolver must fire the unresolved event, skip
// that level entirely, and leave the other level intact.
func TestResolveMatrixLabels_BasepathUnresolved(t *testing.T) {
	m, elements := buildMultiLevelMatrixTree()
	// Add a third broken level pointing at 9.9.9 (not in tree).
	brokenDesc := "Engineering"
	m.Labels = append(m.Labels, canonical.MatrixLabel{
		BasePath:    "9.9.9",
		Description: &brokenDesc,
	})
	p := &Plugin{profile: &compliance.Profile{}}

	p.resolveMatrixLabels(m, elements, modeInline)

	if _, bad := m.TargetLabels[brokenDesc]; bad {
		t.Errorf("unresolved level must NOT appear in TargetLabels; got %v", m.TargetLabels[brokenDesc])
	}
	if len(m.TargetLabels) != 2 {
		t.Errorf("other two levels must still resolve; got %d", len(m.TargetLabels))
	}
	if got := p.profile.Snapshot()[MatrixLabelBasepathUnresolved]; got != 1 {
		t.Errorf("matrix_label_basepath_unresolved = %d, want 1", got)
	}
}

// TestResolveMatrixLabels_DescriptionEmpty covers the info case where
// a provider ships a labels[] entry without description. Resolver
// keys by basePath and fires the info event.
func TestResolveMatrixLabels_DescriptionEmpty(t *testing.T) {
	m, elements := buildMultiLevelMatrixTree()
	// Clear the Primary level's description — simulate a wire defect.
	m.Labels[0].Description = nil
	p := &Plugin{profile: &compliance.Profile{}}

	p.resolveMatrixLabels(m, elements, modeInline)

	// "1.2" is the basePath; it becomes the map key when description
	// is blank.
	if _, ok := m.TargetLabels["1.2"]; !ok {
		t.Errorf("empty-description level must key by basePath; got keys %v", keysOf(m.TargetLabels))
	}
	if got := p.profile.Snapshot()[MatrixLabelDescriptionEmpty]; got != 1 {
		t.Errorf("matrix_label_description_empty = %d, want 1", got)
	}
}

// TestExtractConnectionParamMap covers the two-deep crosspoint
// structure (connections/target/source/param) that spec §5.1.2
// defines and that 9092 wire-verified.
func TestExtractConnectionParamMap(t *testing.T) {
	container := buildConnectionsNode()
	got := extractConnectionParamMap(container)

	if len(got) != 3 {
		t.Fatalf("want 3 crosspoints, got %d (keys: %v)", len(got), keysOf(got))
	}
	if v := got["3.3"]["gain"]; v != int64(10) {
		t.Errorf("3.3/gain = %v, want 10", v)
	}
	if v := got["6.3"]["gain"]; v != int64(5) {
		t.Errorf("6.3/gain = %v, want 5", v)
	}
	if v := got["0.0"]["gain"]; v != int64(0) {
		t.Errorf("0.0/gain = %v, want 0", v)
	}
}

// buildMultiLevelMatrixTree returns a hand-authored element map
// mirroring what the smh emulator ships: one matrix declaring two
// label levels, each basePath pointing to a Node with
// targets (number=1) and sources (number=2) children.
func buildMultiLevelMatrixTree() (*canonical.Matrix, map[string]canonical.Element) {
	primaryDesc := "Primary"
	longDesc := "Long"
	matrix := &canonical.Matrix{
		Header: canonical.Header{
			Number: 1, Identifier: "matrix", Path: "matrix", OID: "1.1",
			IsOnline: true, Access: canonical.AccessReadWrite,
			Children: canonical.EmptyChildren(),
		},
		Type: canonical.MatrixOneToN, Mode: canonical.ModeLinear,
		TargetCount: 2, SourceCount: 2,
		Labels: []canonical.MatrixLabel{
			{BasePath: "1.2", Description: &primaryDesc},
			{BasePath: "1.3", Description: &longDesc},
		},
		Targets: []canonical.MatrixTarget{{Number: 0}, {Number: 1}},
		Sources: []canonical.MatrixSource{{Number: 0}, {Number: 1}},
	}

	buildLabels := func(prefix, tgt0, tgt1, src0, src1 string) []canonical.Element {
		return []canonical.Element{
			labelsContainer(prefix+".1", 1, "targets", []labelParam{
				{"t-0", 0, tgt0}, {"t-1", 1, tgt1},
			}),
			labelsContainer(prefix+".2", 2, "sources", []labelParam{
				{"s-0", 0, src0}, {"s-1", 1, src1},
			}),
		}
	}

	primary := &canonical.Node{
		Header: canonical.Header{
			Number: 2, Identifier: "labels-primary", Path: "labels-primary", OID: "1.2",
			IsOnline: true, Access: canonical.AccessRead,
			Children: buildLabels("1.2", "OUT 1", "OUT 2", "IN 1", "IN 2"),
		},
	}
	long := &canonical.Node{
		Header: canonical.Header{
			Number: 3, Identifier: "labels-long", Path: "labels-long", OID: "1.3",
			IsOnline: true, Access: canonical.AccessRead,
			Children: buildLabels("1.3", "Output Main", "Output Backup", "Input Primary", "Input Backup"),
		},
	}

	elements := map[string]canonical.Element{
		"1.1": matrix,
		"1.2": primary,
		"1.3": long,
	}
	// Also register every descendant so removeFromTree has entries to delete.
	for _, root := range []*canonical.Node{primary, long} {
		walk(root, func(el canonical.Element) {
			elements[el.Common().OID] = el
		})
	}
	return matrix, elements
}

type labelParam struct {
	id     string
	number int
	value  string
}

func labelsContainer(oid string, number int, ident string, params []labelParam) canonical.Element {
	children := make([]canonical.Element, 0, len(params))
	for _, p := range params {
		children = append(children, &canonical.Parameter{
			Header: canonical.Header{
				Number: p.number, Identifier: p.id, Path: ident + "." + p.id, OID: oid + "." + itoa(p.number),
				IsOnline: true, Access: canonical.AccessReadWrite,
				Children: canonical.EmptyChildren(),
			},
			Type:  canonical.ParamString,
			Value: p.value,
		})
	}
	return &canonical.Node{
		Header: canonical.Header{
			Number: number, Identifier: ident, Path: ident, OID: oid,
			IsOnline: true, Access: canonical.AccessRead, Children: children,
		},
	}
}

// buildConnectionsNode returns a connections Node matching the
// Ember+ spec p.38 shape: connections -> target -> source -> gain.
// Three crosspoints populated (3.3=10, 6.3=5, 0.0=0) to mirror the
// 9092 wire capture.
func buildConnectionsNode() *canonical.Node {
	mk := func(target, source, gain int) *canonical.Node {
		gainParam := &canonical.Parameter{
			Header: canonical.Header{
				Number: 1, Identifier: "gain", OID: "1.2.2.3." + itoa(target) + "." + itoa(source) + ".1",
				IsOnline: true, Access: canonical.AccessReadWrite, Children: canonical.EmptyChildren(),
			},
			Type: canonical.ParamInteger, Value: int64(gain),
		}
		sourceNode := &canonical.Node{
			Header: canonical.Header{
				Number: source, Identifier: "s-" + itoa(source), OID: "1.2.2.3." + itoa(target) + "." + itoa(source),
				IsOnline: true, Access: canonical.AccessRead,
				Children: []canonical.Element{gainParam},
			},
		}
		return &canonical.Node{
			Header: canonical.Header{
				Number: target, Identifier: "t-" + itoa(target), OID: "1.2.2.3." + itoa(target),
				IsOnline: true, Access: canonical.AccessRead,
				Children: []canonical.Element{sourceNode},
			},
		}
	}
	return &canonical.Node{
		Header: canonical.Header{
			Number: 3, Identifier: "connections", OID: "1.2.2.3",
			IsOnline: true, Access: canonical.AccessRead,
			Children: []canonical.Element{mk(3, 3, 10), mk(6, 3, 5), mk(0, 0, 0)},
		},
	}
}

func walk(n *canonical.Node, fn func(canonical.Element)) {
	for _, c := range n.Children {
		fn(c)
		if child, ok := c.(*canonical.Node); ok {
			walk(child, fn)
		}
	}
}

func keysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [11]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
