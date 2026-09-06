package emberplus

import (
	"context"
	"testing"

	"dhs/internal/consumer"
	"dhs/internal/emberplus/codec/glow"
)

func newSeedTestPlugin() *Plugin {
	return &Plugin{}
}

// TestSeed_Node verifies a Node round-trips its Path + Identifier through
// the cache shape (Object.OID + Object.Path + Meta["element"]="node").
func TestSeed_Node(t *testing.T) {
	p := newSeedTestPlugin()
	p.SeedTreeFromCachedObjects(0, []consumer.Object{{
		OID:   "1.2",
		Label: "identity",
		Path:  []string{"router", "identity"},
		Meta:  map[string]any{"element": "node"},
	}})
	entry := p.numIndex["1.2"]
	if entry == nil {
		t.Fatal("numIndex missing seeded node 1.2")
	}
	if entry.glowNode == nil {
		t.Fatal("glowNode nil — Meta element=node should rebuild glow.Node")
	}
	if got := numericKey(entry.glowNode.Path); got != "1.2" {
		t.Errorf("glowNode.Path numericKey = %q, want %q", got, "1.2")
	}
	if entry.glowNode.Identifier != "identity" {
		t.Errorf("glowNode.Identifier = %q, want %q", entry.glowNode.Identifier, "identity")
	}
	if p.pathIndex["router.identity"] == nil {
		t.Error("pathIndex missing router.identity after seed")
	}
}

// TestSeed_Parameter rebuilds a Parameter with type + streamIdentifier
// from the cached Meta — the round-trip a `dhs stream` or `dhs set`
// needs after hot-load.
func TestSeed_Parameter(t *testing.T) {
	p := newSeedTestPlugin()
	p.SeedTreeFromCachedObjects(0, []consumer.Object{{
		OID:   "1.4.1.3",
		Label: "value",
		Path:  []string{"router", "streams", "stream1", "value"},
		Meta: map[string]any{
			"element":          "parameter",
			"type":             "integer",
			"streamIdentifier": float64(0), // JSON decodes as float64
		},
	}})
	entry := p.numIndex["1.4.1.3"]
	if entry == nil || entry.glowParam == nil {
		t.Fatal("glowParam missing for seeded parameter")
	}
	if entry.glowParam.Type != glow.ParamTypeInteger {
		t.Errorf("glowParam.Type = %d, want ParamTypeInteger", entry.glowParam.Type)
	}
	if entry.glowParam.Identifier != "value" {
		t.Errorf("glowParam.Identifier = %q, want %q", entry.glowParam.Identifier, "value")
	}
	if numericKey(entry.glowParam.Path) != "1.4.1.3" {
		t.Errorf("glowParam.Path = %v, want [1 4 1 3]", entry.glowParam.Path)
	}
}

// TestSeed_StreamIndex pins that a parameter with a non-zero stream
// identifier is registered in streamIndex so `dhs stream` finds it
// without re-walking.
func TestSeed_StreamIndex(t *testing.T) {
	p := newSeedTestPlugin()
	p.SeedTreeFromCachedObjects(0, []consumer.Object{{
		OID:   "1.4.1.3",
		Label: "value",
		Path:  []string{"router", "streams", "stream1", "value"},
		Meta: map[string]any{
			"element":          "parameter",
			"type":             "integer",
			"streamIdentifier": float64(7),
		},
	}})
	paths := p.streamIndex[7]
	if len(paths) != 1 || paths[0] != "1.4.1.3" {
		t.Errorf("streamIndex[7] = %v, want [1.4.1.3]", paths)
	}
}

// TestSeed_StreamIndexZero pins #436: a parameter cached with
// streamIdentifier=0 must round-trip as a stream parameter (present
// with value 0), not be silently treated as absent. The canonicalize
// layer (plugin.go) only writes Meta["streamIdentifier"] when
// HasStreamIdentifier is true, so presence of the Meta key IS the
// presence bit on the seed side.
func TestSeed_StreamIndexZero(t *testing.T) {
	p := newSeedTestPlugin()
	p.SeedTreeFromCachedObjects(0, []consumer.Object{{
		OID:   "1.4.1.3",
		Label: "value",
		Path:  []string{"router", "streams", "stream0", "value"},
		Meta: map[string]any{
			"element":          "parameter",
			"type":             "integer",
			"streamIdentifier": float64(0),
		},
	}})
	entry := p.numIndex["1.4.1.3"]
	if entry == nil || entry.glowParam == nil {
		t.Fatal("glowParam missing for seeded parameter")
	}
	if !entry.glowParam.HasStreamIdentifier {
		t.Error("HasStreamIdentifier = false, want true (id=0 present in cache)")
	}
	paths := p.streamIndex[0]
	if len(paths) != 1 || paths[0] != "1.4.1.3" {
		t.Errorf("streamIndex[0] = %v, want [1.4.1.3] (id=0 must index)", paths)
	}
}

// TestSeed_Matrix rebuilds a Matrix with enough to satisfy the matrix
// verb's gate (`entry.glowMatrix != nil`) and SendMatrixConnect's path
// arg.
func TestSeed_Matrix(t *testing.T) {
	p := newSeedTestPlugin()
	p.SeedTreeFromCachedObjects(0, []consumer.Object{{
		OID:   "1.2.3",
		Label: "matrix",
		Path:  []string{"router", "nToN", "matrix"},
		Meta:  map[string]any{"element": "matrix"},
	}})
	entry := p.numIndex["1.2.3"]
	if entry == nil || entry.glowMatrix == nil {
		t.Fatal("glowMatrix missing for seeded matrix")
	}
	if numericKey(entry.glowMatrix.Path) != "1.2.3" {
		t.Errorf("glowMatrix.Path = %v, want [1 2 3]", entry.glowMatrix.Path)
	}
	if entry.matrixState == nil {
		t.Error("matrixState nil — matrix.NewStateFromGlow should have produced one")
	}
}

// TestSeed_RoundTripIdentity proves chunk 2 + chunk 3 compose: seed the
// identity subtree from cache, then IdentityProbe returns the expected
// identity string without any wire I/O. The parent "identity" Node must
// be seeded too — IdentityProbe locates the identity location via the
// Node (DTD 2.30+ schemaIdentifiers OR identifier == "identity"), then
// reads children.
func TestSeed_RoundTripIdentity(t *testing.T) {
	p := newSeedTestPlugin()
	p.SeedTreeFromCachedObjects(0, []consumer.Object{
		{
			OID:   "1.0",
			Label: "identity",
			Path:  []string{"router", "identity"},
			Meta:  map[string]any{"element": "node"},
		},
		{
			OID:   "1.0.1",
			Label: "product",
			Path:  []string{"router", "identity", "product"},
			Value: consumer.Value{Kind: consumer.KindString, Str: "Tiny Ember+ Router"},
			Meta:  map[string]any{"element": "parameter", "type": "string"},
		},
		{
			OID:   "1.0.3",
			Label: "version",
			Path:  []string{"router", "identity", "version"},
			Value: consumer.Value{Kind: consumer.KindString, Str: "1.6.2"},
			Meta:  map[string]any{"element": "parameter", "type": "string"},
		},
	})
	id, err := p.IdentityProbe(context.Background(), 0)
	if err != nil {
		t.Fatalf("IdentityProbe after seed: %v", err)
	}
	if got, want := id, "Tiny Ember+ Router@1.6.2"; got != want {
		t.Errorf("identity = %q, want %q", got, want)
	}
}

// TestSeed_RejectsNonZeroSlot mirrors the slot=0 invariant of
// IdentityProbe. Non-zero slot is a usage error per ADR-0022 (Ember+
// flattens to one logical slot).
func TestSeed_RejectsNonZeroSlot(t *testing.T) {
	p := newSeedTestPlugin()
	p.SeedTreeFromCachedObjects(1, []consumer.Object{{
		OID:   "1.0.1",
		Label: "product",
		Path:  []string{"router", "identity", "product"},
		Meta:  map[string]any{"element": "parameter"},
	}})
	if p.numIndex["1.0.1"] != nil {
		t.Error("slot=1 should be a no-op; numIndex was populated")
	}
}

// TestSeed_UnknownElementKeepsLabelIndex verifies the legacy / partial
// cache case: an Object without Meta["element"] still lands in the
// label index so basic label resolution works post-load (no glow struct
// — get/set/matrix on such an entry then re-walks gracefully).
func TestSeed_UnknownElementKeepsLabelIndex(t *testing.T) {
	p := newSeedTestPlugin()
	p.SeedTreeFromCachedObjects(0, []consumer.Object{{
		OID:   "9.9.9",
		Label: "mystery",
		Path:  []string{"mystery"},
		// No Meta["element"]
	}})
	entry := p.numIndex["9.9.9"]
	if entry == nil {
		t.Fatal("legacy-shape entry should still land in numIndex for label resolution")
	}
	if entry.glowNode != nil || entry.glowParam != nil || entry.glowMatrix != nil {
		t.Error("unknown-element entry must not fabricate a glow.* struct")
	}
}

// TestSeed_MatrixFullSchema rehydrates every MatrixContents field the
// walker writes — type / mode / counts / parametersLocation /
// gainParameterNumber / labels (basePath+description) / targets /
// sources / last-known connections. Pins the contract with provider-side
// canonical.Matrix schema so the cache file ships enough to render
// crosspoint UI without re-walking.
func TestSeed_MatrixFullSchema(t *testing.T) {
	p := newSeedTestPlugin()
	p.SeedTreeFromCachedObjects(0, []consumer.Object{{
		OID:   "1.2.3",
		Label: "matrix",
		Path:  []string{"router", "nToN", "matrix"},
		Meta: map[string]any{
			"element":                  "matrix",
			"type":                     "nToN",
			"mode":                     "linear",
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
					"sources":     []any{float64(3), float64(6)},
					"operation":   "absolute",
					"disposition": "tally",
				},
			},
		},
	}})
	m := p.numIndex["1.2.3"].glowMatrix
	if m == nil {
		t.Fatal("glowMatrix nil after seed")
	}
	if m.MatrixType != glow.MatrixTypeNToN {
		t.Errorf("MatrixType = %d, want nToN", m.MatrixType)
	}
	if m.TargetCount != 4 || m.SourceCount != 4 {
		t.Errorf("counts = %d/%d, want 4/4", m.TargetCount, m.SourceCount)
	}
	if m.MaxTotalConnects != 16 || m.MaxConnectsPerTarget != 4 {
		t.Errorf("caps = %d/%d, want 16/4", m.MaxTotalConnects, m.MaxConnectsPerTarget)
	}
	if got, ok := m.ParametersLocation.([]int32); !ok || numericKey(got) != "1.2.2" {
		t.Errorf("ParametersLocation = %v, want []int32{1,2,2}", m.ParametersLocation)
	}
	if m.GainParameterNumber != 1 {
		t.Errorf("GainParameterNumber = %d, want 1", m.GainParameterNumber)
	}
	if len(m.Labels) != 1 || m.Labels[0].Description != "Primary" || numericKey(m.Labels[0].BasePath) != "1.2.1" {
		t.Errorf("Labels = %+v", m.Labels)
	}
	if got := numericKey(m.Targets); got != "3.6.9.12" {
		t.Errorf("Targets numericKey = %q, want 3.6.9.12", got)
	}
	if got := numericKey(m.Sources); got != "3.6.9.12" {
		t.Errorf("Sources numericKey = %q, want 3.6.9.12", got)
	}
	if len(m.Connections) != 1 {
		t.Errorf("Connections len = %d, want 1", len(m.Connections))
	} else {
		c := m.Connections[0]
		if c.Target != 3 || numericKey(c.Sources) != "3.6" {
			t.Errorf("Connection = %+v, want target=3 sources=[3 6]", c)
		}
	}
}

// TestSeed_FunctionTuples rehydrates Function.Arguments + Result from
// cached Meta. Pinned per spec p.91 (Function/TupleDescription).
func TestSeed_FunctionTuples(t *testing.T) {
	p := newSeedTestPlugin()
	p.SeedTreeFromCachedObjects(0, []consumer.Object{{
		OID:   "1.4.1",
		Label: "add",
		Path:  []string{"router", "functions", "add"},
		Meta: map[string]any{
			"element": "function",
			"arguments": []map[string]any{
				{"type": "integer", "name": "a"},
				{"type": "integer", "name": "b"},
			},
			"result": []map[string]any{
				{"type": "integer", "name": "sum"},
			},
			"templateReference": "0.5",
		},
	}})
	f := p.numIndex["1.4.1"].glowFunc
	if f == nil {
		t.Fatal("glowFunc nil")
	}
	if len(f.Arguments) != 2 || f.Arguments[0].Name != "a" || f.Arguments[1].Type != glow.ParamTypeInteger {
		t.Errorf("Arguments = %+v", f.Arguments)
	}
	if len(f.Result) != 1 || f.Result[0].Name != "sum" {
		t.Errorf("Result = %+v", f.Result)
	}
	if numericKey(f.TemplateReference) != "0.5" {
		t.Errorf("TemplateReference = %v", f.TemplateReference)
	}
}

// TestSeed_TemplateRoundTrip pins that cached Template objects (the
// synthetic Meta["element"]="template" shape appendTemplateObjects
// writes) flow back into p.templates and become ResolveTemplate-able.
func TestSeed_TemplateRoundTrip(t *testing.T) {
	p := newSeedTestPlugin()
	p.SeedTreeFromCachedObjects(0, []consumer.Object{{
		OID:   "0.5.2",
		Label: "Gain template",
		Meta: map[string]any{
			"element":     "template",
			"qualified":   true,
			"number":      float64(2),
			"description": "Gain template",
		},
	}})
	if p.numIndex["0.5.2"] != nil {
		t.Error("template should not land in numIndex")
	}
	got := p.ResolveTemplate([]int32{0, 5, 2})
	if got == nil {
		t.Fatal("ResolveTemplate returned nil")
	}
	if !got.Qualified || got.Description != "Gain template" {
		t.Errorf("template = %+v", got)
	}
}

// TestSeed_FreshnessStale pins disk-loaded entries as Stale until live
// confirmation flips them — ADR-0022 freshness contract.
func TestSeed_FreshnessStale(t *testing.T) {
	p := newSeedTestPlugin()
	p.SeedTreeFromCachedObjects(0, []consumer.Object{{
		OID:   "1.0.1",
		Label: "product",
		Path:  []string{"router", "identity", "product"},
		Meta:  map[string]any{"element": "parameter"},
	}})
	if p.numIndex["1.0.1"].freshness != FreshnessStale {
		t.Errorf("freshness = %d, want FreshnessStale", p.numIndex["1.0.1"].freshness)
	}
}
