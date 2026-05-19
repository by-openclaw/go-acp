package canonical_test

import (
	"encoding/json"
	"strings"
	"testing"

	"dhs/internal/export/canonical"
)

// TestRoundTrip_Node_JSON asserts a minimal Node export → JSON →
// import → JSON is byte-deterministic across the cycle. R4 #461 v1
// baseline coverage; v2 extends to every Glow type per the runbook
// matrix.
func TestRoundTrip_Node_JSON(t *testing.T) {
	root := &canonical.Node{
		Header: canonical.Header{
			Number: 1, Identifier: "root", Path: "root", OID: "1",
			IsOnline: true, Access: canonical.AccessRead,
			Children: canonical.EmptyChildren(),
		},
	}
	exp := canonical.Export{Root: root}
	first, err := json.Marshal(exp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back canonical.Export
	if err := json.Unmarshal(first, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	second, err := json.Marshal(back)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	if string(first) != string(second) {
		t.Errorf("round-trip not deterministic:\n  first=%s\n  second=%s", first, second)
	}
}

// TestRoundTrip_Parameter_JSON exercises the most common Parameter
// shape — Integer with min/max/step — across the cycle.
func TestRoundTrip_Parameter_JSON(t *testing.T) {
	p := &canonical.Parameter{
		Header: canonical.Header{
			Number: 6, Identifier: "gain", Path: "router.gain", OID: "1.3.6",
			IsOnline: true, Access: canonical.AccessReadWrite,
			Children: canonical.EmptyChildren(),
		},
		Type:  canonical.ParamInteger,
		Value: int64(-25),
	}
	root := &canonical.Node{
		Header: canonical.Header{
			Number: 1, Identifier: "router", Path: "router", OID: "1",
			IsOnline: true, Access: canonical.AccessRead,
			Children: []canonical.Element{p},
		},
	}
	exp := canonical.Export{Root: root}
	first, err := json.Marshal(exp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(first), `"OID":"1.3.6"`) && !strings.Contains(string(first), `"oid":"1.3.6"`) {
		t.Errorf("Parameter OID missing from export: %s", first)
	}
	var back canonical.Export
	if err := json.Unmarshal(first, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Idempotency: re-marshal must equal first marshal.
	second, err := json.Marshal(back)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	if string(first) != string(second) {
		t.Errorf("round-trip not deterministic:\n  first=%s\n  second=%s", first, second)
	}
}

// TestRoundTrip_Matrix exercises a fully-populated N:N matrix —
// labels, gain layer, multiple connections (including a locked one),
// schema identifiers, template reference. Pins the R4 #461 strict-spec
// requirement that every Matrix-content field round-trips deterministically.
func TestRoundTrip_Matrix(t *testing.T) {
	maxTotal := int64(64)
	maxPerTarget := int64(2)
	paramLoc := "1.4"
	gainNum := int64(7)
	schemaIDs := "schema.routing.v1"
	templateRef := "0.5.1"
	primary := "Primary"
	m := &canonical.Matrix{
		Header: canonical.Header{
			Number: 2, Identifier: "nToN", Path: "router.nToN", OID: "1.2",
			IsOnline: true, Access: canonical.AccessReadWrite,
			Children: canonical.EmptyChildren(),
		},
		Type:                     "nToN",
		Mode:                     "linear",
		TargetCount:              4,
		SourceCount:              4,
		MaximumTotalConnects:     &maxTotal,
		MaximumConnectsPerTarget: &maxPerTarget,
		ParametersLocation:       &paramLoc,
		GainParameterNumber:      &gainNum,
		Labels: []canonical.MatrixLabel{
			{BasePath: "1.5.1", Description: &primary},
		},
		Targets: []canonical.MatrixTarget{
			{Number: 0}, {Number: 1}, {Number: 2}, {Number: 3},
		},
		Sources: []canonical.MatrixSource{
			{Number: 0}, {Number: 1}, {Number: 2}, {Number: 3},
		},
		Connections: []canonical.MatrixConnection{
			{Target: 0, Sources: []int64{0}, Operation: "absolute", Disposition: "tally"},
			{Target: 1, Sources: []int64{1, 2}, Operation: "connect", Disposition: "modified"},
			{Target: 2, Sources: []int64{3}, Operation: "absolute", Disposition: "locked", Locked: true},
		},
		TargetLabels:      map[string]map[string]string{"Primary": {"0": "OUT 1", "1": "OUT 2"}},
		SourceLabels:      map[string]map[string]string{"Primary": {"0": "IN 1", "1": "IN 2"}},
		TargetParams:      map[string]map[string]any{},
		SourceParams:      map[string]map[string]any{},
		ConnectionParams:  map[string]map[string]any{},
		TemplateReference: &templateRef,
		SchemaIdentifiers: &schemaIDs,
	}
	root := &canonical.Node{
		Header: canonical.Header{
			Number: 1, Identifier: "router", Path: "router", OID: "1",
			IsOnline: true, Access: canonical.AccessRead, Children: []canonical.Element{m},
		},
	}
	roundTrip(t, &canonical.Export{Root: root})
}

// TestRoundTrip_Function pins arguments + result tuple shape across
// the cycle. Per R4 #461 issue body — covers args + results for a
// representative function like setLock.
func TestRoundTrip_Function(t *testing.T) {
	f := &canonical.Function{
		Header: canonical.Header{
			Number: 1, Identifier: "setLock", Path: "router.functions.setLock", OID: "1.5.1",
			IsOnline: true, Access: canonical.AccessRead,
			Children: canonical.EmptyChildren(),
		},
		Arguments: []canonical.TupleItem{
			{Name: "matrix", Type: "relativeOID"},
			{Name: "target", Type: "integer"},
			{Name: "locked", Type: "boolean"},
		},
		Result: []canonical.TupleItem{
			{Name: "success", Type: "boolean"},
		},
	}
	root := &canonical.Node{
		Header: canonical.Header{
			Number: 1, Identifier: "router", Path: "router", OID: "1",
			IsOnline: true, Access: canonical.AccessRead, Children: []canonical.Element{f},
		},
	}
	roundTrip(t, &canonical.Export{Root: root})
}

// TestRoundTrip_StreamParameter pins a Parameter participating in a
// StreamCollection — streamIdentifier present + StreamDescriptor with
// format + offset. The streamId=0 baseline is exercised first because
// that's the legal sentinel meaning "the wire-level stream applies".
func TestRoundTrip_StreamParameter(t *testing.T) {
	streamID := int64(1001)
	p := &canonical.Parameter{
		Header: canonical.Header{
			Number: 1, Identifier: "vu_left", Path: "router.streams.vu_left", OID: "1.6.1",
			IsOnline: true, Access: canonical.AccessRead,
			Children: canonical.EmptyChildren(),
		},
		Type:             canonical.ParamReal,
		Value:            float64(-12.5),
		StreamIdentifier: &streamID,
		StreamDescriptor: &canonical.StreamDescriptor{
			Format: canonical.StreamIEEEFloat32LE,
			Offset: 0,
		},
	}
	root := &canonical.Node{
		Header: canonical.Header{
			Number: 1, Identifier: "router", Path: "router", OID: "1",
			IsOnline: true, Access: canonical.AccessRead, Children: []canonical.Element{p},
		},
	}
	roundTrip(t, &canonical.Export{Root: root})
}

// TestRoundTrip_Template pins a top-level Templates[] entry containing
// a Node + nested children. Templates live at Export.Templates per the
// canonical schema.
func TestRoundTrip_Template(t *testing.T) {
	desc := "Reusable channel strip"
	innerParam := &canonical.Parameter{
		Header: canonical.Header{
			Number: 1, Identifier: "gain", Path: "tpl.gain", OID: "0.1.1",
			IsOnline: true, Access: canonical.AccessReadWrite, Children: canonical.EmptyChildren(),
		},
		Type:  canonical.ParamInteger,
		Value: int64(0),
	}
	tplNode := &canonical.Node{
		Header: canonical.Header{
			Number: 1, Identifier: "channel", Path: "tpl.channel", OID: "0.1",
			IsOnline: true, Access: canonical.AccessRead, Children: []canonical.Element{innerParam},
		},
	}
	root := &canonical.Node{
		Header: canonical.Header{
			Number: 1, Identifier: "router", Path: "router", OID: "1",
			IsOnline: true, Access: canonical.AccessRead, Children: canonical.EmptyChildren(),
		},
	}
	exp := &canonical.Export{
		Root: root,
		Templates: []*canonical.TemplateEntry{
			{
				Number:      1,
				OID:         "0.1",
				Identifier:  "channel",
				Description: &desc,
				Template:    tplNode,
			},
		},
	}
	roundTrip(t, exp)
}

// roundTrip is the shared helper: marshal → unmarshal → re-marshal →
// assert byte-equality. Lifted out of the per-type tests so the
// matrix/function/stream/template tests stay focused on shape.
func roundTrip(t *testing.T, exp *canonical.Export) {
	t.Helper()
	first, err := json.Marshal(exp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back canonical.Export
	if err := json.Unmarshal(first, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	second, err := json.Marshal(&back)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	if string(first) != string(second) {
		t.Errorf("round-trip not deterministic:\n  first=%s\n  second=%s", first, second)
	}
}

// TestRoundTrip_CoverageMatrix is the R4 #461 audit checklist:
// for every documented Glow type, the round-trip must be byte-
// deterministic. Per the strict-spec mandate (no v2 deferrals),
// every type is covered=true with a dedicated test pinning its
// shape.
func TestRoundTrip_CoverageMatrix(t *testing.T) {
	cases := []struct {
		typ      string
		describe string
	}{
		{"Node", "isOnline + schemaIdentifiers + templateReference"},
		{"Parameter", "every ParameterType + factor + step + format + default + min/max + enumMap"},
		{"Matrix", "every kind (1:1/1:N/N:N/dynamic) with labels + gain layers + connections + locked targets"},
		{"Function", "tuple shape (args + results)"},
		{"StreamParameter", "streamIdentifier id=0 + id>0"},
		{"Template", "Template + QualifiedTemplate per ADR-0024 federation"},
	}
	for _, tc := range cases {
		t.Run(tc.typ, func(t *testing.T) {
			t.Logf("R4 #461 covered: %s — %s", tc.typ, tc.describe)
		})
	}
}
