package manifest

import (
	"testing"

	"dhs/internal/export/canonical"
)

// TestBuildCanonicalMatrix_FullSchema pins that an Ember+ DM-cache
// matrix Object (Meta written by emberplus walker) reconstructs into
// a canonical.Matrix with every spec p.88 field preserved.
func TestBuildCanonicalMatrix_FullSchema(t *testing.T) {
	o := dmObject{
		ID: 3, Label: "matrix", OID: "1.2.3",
		Meta: map[string]any{
			"element":                  "matrix",
			"type":                     "nToN",
			"mode":                     "nonLinear",
			"targetCount":              float64(4),
			"sourceCount":              float64(4),
			"maximumTotalConnects":     float64(16),
			"maximumConnectsPerTarget": float64(4),
			"parametersLocation":       "1.2.2",
			"gainParameterNumber":      float64(1),
			"labels": []any{
				map[string]any{"basePath": "1.2.1", "description": "Primary"},
			},
			"targets": []any{float64(3), float64(6), float64(9), float64(12)},
			"sources": []any{float64(3), float64(6), float64(9), float64(12)},
			"connections": map[string]any{
				"3": map[string]any{
					"target":      float64(3),
					"sources":     []any{float64(6)},
					"operation":   "absolute",
					"disposition": "tally",
				},
			},
			"targetLabels": map[string]any{
				"Primary": map[string]any{"3": "AES-T-3", "6": "AES-T-6"},
			},
			"sourceLabels": map[string]any{
				"Primary": map[string]any{"3": "AES-S-3"},
			},
		},
	}
	m := buildCanonicalMatrix(o, "1.2.3", "router.nToN.matrix")
	if m.Type != canonical.MatrixNToN {
		t.Errorf("type = %q, want %q", m.Type, canonical.MatrixNToN)
	}
	if m.TargetCount != 4 || m.SourceCount != 4 {
		t.Errorf("counts = %d/%d, want 4/4", m.TargetCount, m.SourceCount)
	}
	if m.MaximumTotalConnects == nil || *m.MaximumTotalConnects != 16 {
		t.Errorf("MaximumTotalConnects = %v, want 16", m.MaximumTotalConnects)
	}
	if m.ParametersLocation == nil || *m.ParametersLocation != "1.2.2" {
		t.Errorf("ParametersLocation = %v", m.ParametersLocation)
	}
	if m.GainParameterNumber == nil || *m.GainParameterNumber != 1 {
		t.Errorf("GainParameterNumber = %v", m.GainParameterNumber)
	}
	if len(m.Labels) != 1 || m.Labels[0].BasePath != "1.2.1" {
		t.Errorf("Labels = %+v", m.Labels)
	}
	if len(m.Targets) != 4 {
		t.Errorf("Targets count = %d, want 4", len(m.Targets))
	}
	if len(m.Connections) != 1 || m.Connections[0].Target != 3 {
		t.Errorf("Connections = %+v", m.Connections)
	}
	if m.TargetLabels["Primary"]["3"] != "AES-T-3" {
		t.Errorf("TargetLabels[Primary][3] = %q", m.TargetLabels["Primary"]["3"])
	}
	if m.SourceLabels["Primary"]["3"] != "AES-S-3" {
		t.Errorf("SourceLabels[Primary][3] = %q", m.SourceLabels["Primary"]["3"])
	}
}

// TestBuildCanonicalFunction_Tuples verifies Arguments + Result tuple
// round-trip from cached Meta (spec p.91).
func TestBuildCanonicalFunction_Tuples(t *testing.T) {
	o := dmObject{
		ID: 1, Label: "add", OID: "1.4.1",
		Meta: map[string]any{
			"element": "function",
			"arguments": []any{
				map[string]any{"type": "integer", "name": "a"},
				map[string]any{"type": "integer", "name": "b"},
			},
			"result": []any{
				map[string]any{"type": "integer", "name": "sum"},
			},
		},
	}
	f := buildCanonicalFunction(o, "1.4.1", "router.functions.add")
	if f.Identifier != "add" {
		t.Errorf("Identifier = %q, want add", f.Identifier)
	}
	if len(f.Arguments) != 2 || f.Arguments[0].Name != "a" || f.Arguments[1].Type != "integer" {
		t.Errorf("Arguments = %+v", f.Arguments)
	}
	if len(f.Result) != 1 || f.Result[0].Name != "sum" {
		t.Errorf("Result = %+v", f.Result)
	}
}

// TestElementKind_DispatchSurface covers the discriminator that
// buildSlotNode uses to route DM objects to the right canonical.*
// builder. Walker writes "node"/"parameter"/"matrix"/"function"/
// "template"; missing key is benign (legacy ACP1/ACP2 DMs that
// pre-date the discriminator still work via the container/leaf branch).
func TestElementKind_DispatchSurface(t *testing.T) {
	for _, c := range []struct {
		name string
		meta map[string]any
		want string
	}{
		{"matrix", map[string]any{"element": "matrix"}, "matrix"},
		{"function", map[string]any{"element": "function"}, "function"},
		{"template", map[string]any{"element": "template"}, "template"},
		{"missing", nil, ""},
		{"empty", map[string]any{}, ""},
	} {
		if got := elementKind(c.meta); got != c.want {
			t.Errorf("%s: elementKind = %q, want %q", c.name, got, c.want)
		}
	}
}
