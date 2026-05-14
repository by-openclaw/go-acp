package emberplus

import (
	"context"
	"testing"

	"dhs/internal/emberplus/codec/glow"
	"dhs/internal/protocol"
)

func newSeedTestPlugin() *Plugin {
	return &Plugin{}
}

// TestSeed_Node verifies a Node round-trips its Path + Identifier through
// the cache shape (Object.OID + Object.Path + Meta["element"]="node").
func TestSeed_Node(t *testing.T) {
	p := newSeedTestPlugin()
	p.SeedTreeFromCachedObjects(0, []protocol.Object{{
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
	p.SeedTreeFromCachedObjects(0, []protocol.Object{{
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
	p.SeedTreeFromCachedObjects(0, []protocol.Object{{
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

// TestSeed_Matrix rebuilds a Matrix with enough to satisfy the matrix
// verb's gate (`entry.glowMatrix != nil`) and SendMatrixConnect's path
// arg.
func TestSeed_Matrix(t *testing.T) {
	p := newSeedTestPlugin()
	p.SeedTreeFromCachedObjects(0, []protocol.Object{{
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
// identity string without any wire I/O.
func TestSeed_RoundTripIdentity(t *testing.T) {
	p := newSeedTestPlugin()
	p.SeedTreeFromCachedObjects(0, []protocol.Object{
		{
			OID:   "1.0.1",
			Label: "product",
			Path:  []string{"router", "identity", "product"},
			Value: protocol.Value{Kind: protocol.KindString, Str: "Tiny Ember+ Router"},
			Meta:  map[string]any{"element": "parameter", "type": "string"},
		},
		{
			OID:   "1.0.3",
			Label: "version",
			Path:  []string{"router", "identity", "version"},
			Value: protocol.Value{Kind: protocol.KindString, Str: "1.6.2"},
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
	p.SeedTreeFromCachedObjects(1, []protocol.Object{{
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
	p.SeedTreeFromCachedObjects(0, []protocol.Object{{
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

// TestSeed_FreshnessStale pins disk-loaded entries as Stale until live
// confirmation flips them — ADR-0022 freshness contract.
func TestSeed_FreshnessStale(t *testing.T) {
	p := newSeedTestPlugin()
	p.SeedTreeFromCachedObjects(0, []protocol.Object{{
		OID:   "1.0.1",
		Label: "product",
		Path:  []string{"router", "identity", "product"},
		Meta:  map[string]any{"element": "parameter"},
	}})
	if p.numIndex["1.0.1"].freshness != FreshnessStale {
		t.Errorf("freshness = %d, want FreshnessStale", p.numIndex["1.0.1"].freshness)
	}
}
