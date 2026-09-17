package emberplus

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dhs/internal/consumer"
	"dhs/internal/consumer/compliance"
	"dhs/internal/emberplus/codec/glow"
)

// TestReverseMappers_AllBranches drives every case (including the
// default fallback) of the cache→glow reverse-mapping helpers in
// seed_tree.go. These are pure switch tables; the previously-uncovered
// branches are the non-default named cases that the loopback/seed tests
// never hit because the smh/TinyEmber providers only emit a subset.
func TestReverseMappers_AllBranches(t *testing.T) {
	t.Run("paramTypeFromName", func(t *testing.T) {
		cases := map[string]int64{
			"integer": glow.ParamTypeInteger,
			"real":    glow.ParamTypeReal,
			"string":  glow.ParamTypeString,
			"boolean": glow.ParamTypeBoolean,
			"trigger": glow.ParamTypeTrigger,
			"enum":    glow.ParamTypeEnum,
			"octets":  glow.ParamTypeOctets,
			"":        glow.ParamTypeNull,
			"bogus":   glow.ParamTypeNull,
		}
		for in, want := range cases {
			if got := paramTypeFromName(in); got != want {
				t.Errorf("paramTypeFromName(%q) = %d, want %d", in, got, want)
			}
		}
	})

	t.Run("matrixTypeFromName", func(t *testing.T) {
		cases := map[string]int64{
			"oneToOne": glow.MatrixTypeOneToOne,
			"nToN":     glow.MatrixTypeNToN,
			"oneToN":   glow.MatrixTypeOneToN,
			"":         glow.MatrixTypeOneToN,
		}
		for in, want := range cases {
			if got := matrixTypeFromName(in); got != want {
				t.Errorf("matrixTypeFromName(%q) = %d, want %d", in, got, want)
			}
		}
	})

	t.Run("matrixAddrFromName", func(t *testing.T) {
		if got := matrixAddrFromName("nonLinear"); got != glow.MatrixAddrNonLinear {
			t.Errorf("nonLinear = %d, want %d", got, glow.MatrixAddrNonLinear)
		}
		if got := matrixAddrFromName("linear"); got != glow.MatrixAddrLinear {
			t.Errorf("linear default = %d, want %d", got, glow.MatrixAddrLinear)
		}
	})

	t.Run("connOpFromName", func(t *testing.T) {
		cases := map[string]int64{
			"connect":    glow.ConnOpConnect,
			"disconnect": glow.ConnOpDisconnect,
			"absolute":   glow.ConnOpAbsolute,
			"":           glow.ConnOpAbsolute,
		}
		for in, want := range cases {
			if got := connOpFromName(in); got != want {
				t.Errorf("connOpFromName(%q) = %d, want %d", in, got, want)
			}
		}
	})

	t.Run("connDispFromName", func(t *testing.T) {
		cases := map[string]int64{
			"modified": glow.ConnDispModified,
			"pending":  glow.ConnDispPending,
			"locked":   glow.ConnDispLocked,
			"tally":    glow.ConnDispTally,
			"":         glow.ConnDispTally,
		}
		for in, want := range cases {
			if got := connDispFromName(in); got != want {
				t.Errorf("connDispFromName(%q) = %d, want %d", in, got, want)
			}
		}
	})
}

// TestMetaInt64_AllNumericKinds covers every accepted numeric type plus
// the rejection branch.
func TestMetaInt64_AllNumericKinds(t *testing.T) {
	ok := []any{int64(7), int(7), int32(7), float64(7)}
	for _, v := range ok {
		if got, valid := metaInt64(v); !valid || got != 7 {
			t.Errorf("metaInt64(%T) = (%d,%v), want (7,true)", v, got, valid)
		}
	}
	if _, valid := metaInt64("nope"); valid {
		t.Error("metaInt64(string) should report not-ok")
	}
	if _, valid := metaInt64(nil); valid {
		t.Error("metaInt64(nil) should report not-ok")
	}
}

// TestParseNumericOID_EdgeCases covers empty input, trailing/empty
// segments, and a non-numeric segment that aborts the parse.
func TestParseNumericOID_EdgeCases(t *testing.T) {
	if got := parseNumericOID(""); got != nil {
		t.Errorf("empty = %v, want nil", got)
	}
	if got := parseNumericOID("1..2."); len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Errorf("with empty segments = %v, want [1 2]", got)
	}
	if got := parseNumericOID("1.x.3"); got != nil {
		t.Errorf("non-numeric segment must abort: got %v, want nil", got)
	}
}

// TestLastPathSegment covers both the empty and populated branches.
func TestLastPathSegment(t *testing.T) {
	if got := lastPathSegment(nil); got != "" {
		t.Errorf("empty = %q, want \"\"", got)
	}
	if got := lastPathSegment([]string{"a", "b", "c"}); got != "c" {
		t.Errorf("populated = %q, want c", got)
	}
}

// TestDecodeTupleItems_BothShapes drives the live ([]map[string]any) and
// JSON round-trip ([]any) shapes plus the nil fallthrough.
func TestDecodeTupleItems_BothShapes(t *testing.T) {
	live := []map[string]any{{"name": "a", "type": "integer"}, {"name": "b", "type": "real"}}
	got := decodeTupleItems(live)
	if len(got) != 2 || got[0].Name != "a" || got[0].Type != glow.ParamTypeInteger {
		t.Errorf("live shape = %+v", got)
	}

	jsonShape := []any{
		map[string]any{"name": "x", "type": "string"},
		"skip-me-not-a-map",
	}
	got = decodeTupleItems(jsonShape)
	if len(got) != 1 || got[0].Name != "x" || got[0].Type != glow.ParamTypeString {
		t.Errorf("json shape = %+v", got)
	}

	if got := decodeTupleItems("garbage"); got != nil {
		t.Errorf("unknown shape = %v, want nil", got)
	}
}

// TestDecodeLabels_BothShapes drives the JSON ([]any) shape, the live
// ([]map[string]any) shape, the non-map skip, and the nil fallthrough.
func TestDecodeLabels_BothShapes(t *testing.T) {
	jsonShape := []any{
		map[string]any{"basePath": "1.2", "description": "Primary"},
		"not-a-map",
	}
	got := decodeLabels(jsonShape)
	if len(got) != 1 || got[0].Description != "Primary" || len(got[0].BasePath) != 2 {
		t.Errorf("json shape = %+v", got)
	}

	live := []map[string]any{{"basePath": "3.4", "description": "Long"}}
	got = decodeLabels(live)
	if len(got) != 1 || got[0].Description != "Long" {
		t.Errorf("live shape = %+v", got)
	}

	if got := decodeLabels(42); got != nil {
		t.Errorf("unknown shape = %v, want nil", got)
	}
}

// TestDecodeInt32Slice_BothShapes covers []int32, []any, and the nil
// fallthrough.
func TestDecodeInt32Slice_BothShapes(t *testing.T) {
	if got := decodeInt32Slice([]int32{1, 2}); len(got) != 2 {
		t.Errorf("native = %v", got)
	}
	got := decodeInt32Slice([]any{float64(1), "skip", float64(3)})
	if len(got) != 2 || got[0] != 1 || got[1] != 3 {
		t.Errorf("json = %v", got)
	}
	if got := decodeInt32Slice("garbage"); got != nil {
		t.Errorf("unknown = %v, want nil", got)
	}
}

// TestDecodeConnections covers the populated map, the non-map entry
// skip, and the nil fallthrough.
func TestDecodeConnections(t *testing.T) {
	v := map[string]any{
		"0": map[string]any{
			"target":      float64(0),
			"sources":     []any{float64(1)},
			"operation":   "connect",
			"disposition": "modified",
		},
		"1": "not-a-map",
	}
	got := decodeConnections(v)
	if len(got) != 1 {
		t.Fatalf("connections = %d, want 1", len(got))
	}
	if got[0].Operation != glow.ConnOpConnect || got[0].Disposition != glow.ConnDispModified {
		t.Errorf("conn = %+v", got[0])
	}
	if got := decodeConnections("garbage"); got != nil {
		t.Errorf("unknown = %v, want nil", got)
	}
}

// TestDirOf covers the no-separator branch (returns ".") and the
// separator branch.
func TestDirOf(t *testing.T) {
	if got := dirOf("plainfile"); got != "." {
		t.Errorf("no separator = %q, want .", got)
	}
	if got := dirOf("a/b/c.md"); got != "a/b" {
		t.Errorf("slash = %q, want a/b", got)
	}
	if got := dirOf(`a\b\c.md`); got != `a\b` {
		t.Errorf("backslash = %q, want a\\b", got)
	}
}

// TestWriteUnknownCTXAuditTo drives the plugin-level writer: nil audit,
// empty audit (no file), and a populated audit (file written).
func TestWriteUnknownCTXAuditTo(t *testing.T) {
	// nil audit → nil, no panic.
	p := &Plugin{}
	if err := p.WriteUnknownCTXAuditTo(filepath.Join(t.TempDir(), "x.md")); err != nil {
		t.Errorf("nil audit: %v", err)
	}

	// empty audit → nil, no file.
	p = &Plugin{unknownCTX: newUnknownCTXAudit()}
	path := filepath.Join(t.TempDir(), "empty.md")
	if err := p.WriteUnknownCTXAuditTo(path); err != nil {
		t.Fatalf("empty audit: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("empty audit must not write a file")
	}

	// populated audit → file written with the device endpoint header.
	p = &Plugin{
		unknownCTX: newUnknownCTXAudit(),
		connIP:     "10.0.0.9",
		connPort:   9000,
	}
	p.unknownCTX.record("parameter", []int32{1, 2}, []int32{42})
	path = filepath.Join(t.TempDir(), "audit", "out.md")
	if err := p.WriteUnknownCTXAuditTo(path); err != nil {
		t.Fatalf("populated audit: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if want := "10.0.0.9:9000"; !strings.Contains(string(data), want) {
		t.Errorf("missing endpoint %q in:\n%s", want, data)
	}
}

// TestSeedTemplate covers both the qualified (path set) and
// non-qualified branches plus the empty-OID early return.
func TestSeedTemplate(t *testing.T) {
	p := &Plugin{}
	p.seedTemplate(consumer.Object{OID: ""}) // early return, no panic
	if len(p.templates) != 0 {
		t.Error("empty OID must not register a template")
	}

	p.seedTemplate(consumer.Object{
		OID:  "1.5",
		Meta: map[string]any{"qualified": true, "number": float64(5), "description": "tmpl"},
	})
	tmpl := p.ResolveTemplate([]int32{1, 5})
	if tmpl == nil || !tmpl.Qualified || len(tmpl.Path) != 2 {
		t.Fatalf("qualified template = %+v", tmpl)
	}

	p.seedTemplate(consumer.Object{
		OID:  "7",
		Meta: map[string]any{"qualified": false, "number": float64(7)},
	})
	if got := p.templates["7"]; got == nil || got.Qualified || len(got.Path) != 0 {
		t.Errorf("non-qualified template = %+v", got)
	}
}

// TestSeedTreeFromCachedObjects_Mixed drives SeedTreeFromCachedObjects
// and seedOneEntry across every element discriminator plus the skip
// branches: an empty-OID object (seedOneEntry → nil), a template object
// (routed to p.templates), a parameter carrying a streamIdentifier, a
// matrix, a function, and an unknown-element object.
func TestSeedTreeFromCachedObjects_Mixed(t *testing.T) {
	p := &Plugin{logger: discardLogger()}

	objs := []consumer.Object{
		{OID: "1", Label: "root", Path: []string{"root"}, Meta: map[string]any{"element": "node"}},
		{OID: "1.1", Label: "gain", Path: []string{"root", "gain"}, Meta: map[string]any{
			"element": "parameter", "type": "integer", "streamIdentifier": float64(7),
		}},
		{OID: "1.2", Label: "mat", Path: []string{"root", "mat"}, Meta: map[string]any{
			"element": "matrix", "type": "nToN", "mode": "nonLinear",
			"targetCount": float64(2), "sourceCount": float64(2),
			"parametersLocation": "1.6", "gainParameterNumber": float64(1),
			"schemaIdentifiers": "sch",
			"labels":            []map[string]any{{"basePath": "1.7", "description": "P"}},
			"targets":           []any{float64(0), float64(1)},
			"sources":           []any{float64(0)},
			"connections": map[string]any{
				"0": map[string]any{"target": float64(0), "sources": []any{float64(1)}, "operation": "connect", "disposition": "tally"},
			},
		}},
		{OID: "1.3", Label: "sum", Path: []string{"root", "sum"}, Meta: map[string]any{
			"element": "function", "arguments": []map[string]any{{"name": "a", "type": "integer"}},
		}},
		{OID: "5.1", Label: "tmpl", Path: []string{"tmpl"}, Meta: map[string]any{
			"element": "template", "qualified": true, "number": float64(1),
		}},
		{OID: "", Label: "noid"}, // empty OID → seedOneEntry nil
		{OID: "1.9", Label: "unk", Meta: map[string]any{"element": "weird"}}, // unknown element
	}
	p.SeedTreeFromCachedObjects(0, objs)

	if _, ok := p.numIndex["1.1"]; !ok {
		t.Error("parameter not seeded")
	}
	if p.streamIndex[7] == nil {
		t.Error("stream parameter not registered in streamIndex")
	}
	if _, ok := p.numIndex["1.2"]; !ok {
		t.Error("matrix not seeded")
	}
	if p.templates["5.1"] == nil {
		t.Error("template not routed to p.templates")
	}
	// Non-zero slot → no-op.
	p.SeedTreeFromCachedObjects(3, objs)
}

// TestBuildMatrixFromMeta_ParametersLocationKinds covers the float64 /
// int32 / int64 arms of the parametersLocation CHOICE in
// buildMatrixFromMeta.
func TestBuildMatrixFromMeta_ParametersLocationKinds(t *testing.T) {
	for _, v := range []any{float64(5), int32(5), int64(5)} {
		m := buildMatrixFromMeta([]int32{1, 2}, "m", map[string]any{
			"type": "nToN", "parametersLocation": v,
		})
		if m.ParametersLocation != int32(5) {
			t.Errorf("parametersLocation %T = %v, want 5", v, m.ParametersLocation)
		}
	}
}

// TestSessionSendHelpers_NotConnected drives the SendSubscribe /
// SendUnsubscribe / SendMatrixGetDirectory verbs over a session that
// has no writer, covering the legacy/non-legacy encode fork plus the
// not-connected error path of sendEmBER.
func TestSessionSendHelpers_NotConnected(t *testing.T) {
	s := NewSession(slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.SetProfile(&compliance.Profile{})

	// Default (non-legacy) glow.
	if err := s.SendSubscribe([]int32{1, 2}); err == nil {
		t.Error("SendSubscribe with no writer should error")
	}
	if err := s.SendUnsubscribe([]int32{1, 2}); err == nil {
		t.Error("SendUnsubscribe with no writer should error")
	}
	if err := s.SendMatrixGetDirectory([]int32{1, 2}); err == nil {
		t.Error("SendMatrixGetDirectory with no writer should error")
	}

	// Legacy glow fork.
	s.SetUseLegacyGlow(true)
	if !s.isLegacyGlow() {
		t.Fatal("isLegacyGlow should be true after SetUseLegacyGlow(true)")
	}
	if err := s.SendSubscribe([]int32{1, 2}); err == nil {
		t.Error("legacy SendSubscribe with no writer should error")
	}
	if err := s.SendUnsubscribe([]int32{1, 2}); err == nil {
		t.Error("legacy SendUnsubscribe with no writer should error")
	}
}
