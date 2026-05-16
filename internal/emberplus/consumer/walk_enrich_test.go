package emberplus

import (
	"testing"

	"dhs/internal/consumer"
)

// TestEnrichMatrixLabels_InlineTargetSource pins the post-walk join
// that resolves a matrix's labels[basePath] pointer into inline
// targetLabels / sourceLabels maps. Mirrors provider-side
// canonical.Matrix.TargetLabels / SourceLabels shape so the DM file is
// self-contained for crosspoint rendering.
func TestEnrichMatrixLabels_InlineTargetSource(t *testing.T) {
	objs := []consumer.Object{
		// The matrix carrying labels[basePath="1.2.1", description="Primary"]
		{
			OID:   "1.2.3",
			Label: "matrix",
			Path:  []string{"router", "nToN", "matrix"},
			Meta: map[string]any{
				"element": "matrix",
				"type":    "nToN",
				"labels": []map[string]any{
					{"basePath": "1.2.1", "description": "Primary"},
				},
			},
		},
		// Targets container (number=1 under basePath)
		{
			OID:   "1.2.1.1",
			Label: "targets",
			Path:  []string{"router", "nToN", "labels", "targets"},
			Meta:  map[string]any{"element": "node"},
		},
		// Target label parameters
		{
			OID:   "1.2.1.1.3",
			Label: "t-3",
			Path:  []string{"router", "nToN", "labels", "targets", "t-3"},
			Value: consumer.Value{Kind: consumer.KindString, Str: "AES-T-3"},
			Meta:  map[string]any{"element": "parameter", "type": "string"},
		},
		{
			OID:   "1.2.1.1.6",
			Label: "t-6",
			Path:  []string{"router", "nToN", "labels", "targets", "t-6"},
			Value: consumer.Value{Kind: consumer.KindString, Str: "AES-T-6"},
			Meta:  map[string]any{"element": "parameter", "type": "string"},
		},
		// Sources container (number=2)
		{
			OID:   "1.2.1.2",
			Label: "sources",
			Path:  []string{"router", "nToN", "labels", "sources"},
			Meta:  map[string]any{"element": "node"},
		},
		{
			OID:   "1.2.1.2.3",
			Label: "s-3",
			Path:  []string{"router", "nToN", "labels", "sources", "s-3"},
			Value: consumer.Value{Kind: consumer.KindString, Str: "AES-S-3"},
			Meta:  map[string]any{"element": "parameter", "type": "string"},
		},
	}

	enriched := enrichMatrixLabels(objs)
	var matrix *consumer.Object
	for i := range enriched {
		if enriched[i].OID == "1.2.3" {
			matrix = &enriched[i]
			break
		}
	}
	if matrix == nil {
		t.Fatal("matrix entry missing after enrich")
	}
	tl, ok := matrix.Meta["targetLabels"].(map[string]map[string]string)
	if !ok {
		t.Fatalf("targetLabels missing or wrong shape: %T", matrix.Meta["targetLabels"])
	}
	primary := tl["Primary"]
	if primary["3"] != "AES-T-3" || primary["6"] != "AES-T-6" {
		t.Errorf("targetLabels[Primary] = %v, want {3:AES-T-3, 6:AES-T-6}", primary)
	}
	sl, ok := matrix.Meta["sourceLabels"].(map[string]map[string]string)
	if !ok {
		t.Fatalf("sourceLabels missing")
	}
	if sl["Primary"]["3"] != "AES-S-3" {
		t.Errorf("sourceLabels[Primary][3] = %q, want AES-S-3", sl["Primary"]["3"])
	}
}

// TestEnrichMatrixLabels_DescriptionFallback verifies a label with empty
// description keys the inline map by basePath (matches the resolver's
// fallback convention).
func TestEnrichMatrixLabels_DescriptionFallback(t *testing.T) {
	objs := []consumer.Object{
		{
			OID: "1.2.3", Label: "matrix",
			Path: []string{"router", "matrix"},
			Meta: map[string]any{
				"element": "matrix",
				"labels": []map[string]any{
					{"basePath": "9.9", "description": ""},
				},
			},
		},
		{
			OID:   "9.9.1.0",
			Path:  []string{"router", "lbl", "targets", "first"},
			Value: consumer.Value{Kind: consumer.KindString, Str: "T0"},
			Meta:  map[string]any{"element": "parameter"},
		},
	}
	enriched := enrichMatrixLabels(objs)
	tl := enriched[0].Meta["targetLabels"].(map[string]map[string]string)
	if tl["9.9"]["0"] != "T0" {
		t.Errorf("fallback key by basePath failed: %+v", tl)
	}
}

// TestEnrichMatrixLabels_Idempotent re-runs enrichment on already-
// enriched objects and verifies the shape is stable (no double-write,
// no shape change).
func TestEnrichMatrixLabels_Idempotent(t *testing.T) {
	objs := []consumer.Object{
		{
			OID: "1.2.3", Label: "matrix",
			Path: []string{"router", "matrix"},
			Meta: map[string]any{
				"element": "matrix",
				"labels": []map[string]any{
					{"basePath": "8.8", "description": "Primary"},
				},
			},
		},
		{
			OID:   "8.8.1.5",
			Path:  []string{"router", "lbl", "targets", "t-5"},
			Value: consumer.Value{Kind: consumer.KindString, Str: "Five"},
			Meta:  map[string]any{"element": "parameter"},
		},
	}
	once := enrichMatrixLabels(objs)
	twice := enrichMatrixLabels(once)
	a := once[0].Meta["targetLabels"].(map[string]map[string]string)
	b := twice[0].Meta["targetLabels"].(map[string]map[string]string)
	if a["Primary"]["5"] != "Five" || b["Primary"]["5"] != "Five" {
		t.Errorf("idempotency broken: %+v %+v", a, b)
	}
}
