package emberplus

import (
	"context"
	"testing"

	"dhs/internal/consumer"
	"dhs/internal/consumer/compliance"
	"dhs/internal/emberplus/codec/glow"
)

// seedEntry stores a treeEntry under its numeric path with a built obj
// header so Canonicalize's bottom-up pass can wire it up.
func seedEntry(p *Plugin, numPath []int32, label string, set func(*treeEntry)) {
	e := &treeEntry{
		numericPath: numPath,
		freshness:   FreshnessLive,
		obj: consumer.Object{
			ID:    int(numPath[len(numPath)-1]),
			OID:   numericKey(numPath),
			Label: label,
			Path:  []string{label},
		},
	}
	set(e)
	p.numIndex[numericKey(numPath)] = e
}

// TestCanonicalize_RichTree builds a tree exercising every element kind
// and most optional fields, then runs Canonicalize in pointer, inline,
// and both modes. Drives buildElement / buildNode / buildParameter /
// buildMatrix / buildFunction / buildTemplateEntry and the resolver.
func TestCanonicalize_RichTree(t *testing.T) {
	build := func() *Plugin {
		p := freshPlugin()
		// root Node (1)
		seedEntry(p, []int32{1}, "root", func(e *treeEntry) {
			e.glowNode = &glow.Node{Number: 1, Identifier: "root", Description: "root node"}
		})
		// integer Parameter with full metadata + format+unit (1.1)
		seedEntry(p, []int32{1, 1}, "gain", func(e *treeEntry) {
			e.glowParam = &glow.Parameter{
				Number: 1, Identifier: "gain", Description: "audio gain",
				Type: glow.ParamTypeInteger, Value: int64(5),
				Minimum: int64(0), Maximum: int64(100), Step: int64(1),
				Format: "%d°dB", Factor: 10, Access: 3,
				HasStreamIdentifier: true, StreamIdentifier: 42,
				StreamDescriptor:  &glow.StreamDescription{Format: glow.StreamFmtUnsignedInt16BigEndian, Offset: 0},
				SchemaIdentifiers: "com.example/gain",
			}
		})
		// enum Parameter with masked enum map + a templateReference (1.2)
		seedEntry(p, []int32{1, 2}, "mode", func(e *treeEntry) {
			e.glowParam = &glow.Parameter{
				Number: 2, Identifier: "mode",
				Type: glow.ParamTypeEnum, Value: int64(1),
				EnumMap:           map[int64]string{0: "off", 1: "~hidden", 2: "on"},
				Enumeration:       "off\n~hidden\non",
				TemplateReference: []int32{10, 1},
			}
		})
		// untyped Parameter (Type null) with a string value → inferParamType (1.3)
		seedEntry(p, []int32{1, 3}, "name", func(e *treeEntry) {
			e.glowParam = &glow.Parameter{Number: 3, Identifier: "name", Type: glow.ParamTypeNull, Value: "device"}
		})
		// untyped Parameter with nil value → ParamString fallback (1.4)
		seedEntry(p, []int32{1, 4}, "blank", func(e *treeEntry) {
			e.glowParam = &glow.Parameter{Number: 4, Identifier: "blank", Type: glow.ParamTypeNull, Value: nil}
		})
		// nToN Matrix with labels + parametersLocation + caps + conns (1.5)
		desc := "Primary"
		seedEntry(p, []int32{1, 5}, "mat", func(e *treeEntry) {
			e.glowMatrix = &glow.Matrix{
				Number: 5, Identifier: "mat", Description: "router",
				MatrixType: glow.MatrixTypeNToN, AddressingMode: glow.MatrixAddrNonLinear,
				TargetCount: 2, SourceCount: 2,
				MaxTotalConnects: 4, MaxConnectsPerTarget: 2,
				ParametersLocation:  []int32{1, 6},
				GainParameterNumber: 1,
				Labels:              []glow.Label{{BasePath: []int32{1, 7}, Description: desc}},
				Targets:             []int32{1, 0}, Sources: []int32{1, 0},
				Connections: []glow.Connection{
					{Target: 0, Sources: []int32{1}, Operation: glow.ConnOpConnect, Disposition: glow.ConnDispLocked},
				},
			}
		})
		// labels base Node (1.7) → targets (1.7.1) + sources (1.7.2)
		seedEntry(p, []int32{1, 7}, "labels", func(e *treeEntry) {
			e.glowNode = &glow.Node{Number: 7, Identifier: "labels"}
		})
		seedEntry(p, []int32{1, 7, 1}, "targets", func(e *treeEntry) {
			e.glowNode = &glow.Node{Number: 1, Identifier: "targets"}
		})
		seedEntry(p, []int32{1, 7, 1, 0}, "t0", func(e *treeEntry) {
			e.glowParam = &glow.Parameter{Number: 0, Identifier: "t0", Type: glow.ParamTypeString, Value: "OUT 1"}
		})
		seedEntry(p, []int32{1, 7, 2}, "sources", func(e *treeEntry) {
			e.glowNode = &glow.Node{Number: 2, Identifier: "sources"}
		})
		seedEntry(p, []int32{1, 7, 2, 0}, "s0", func(e *treeEntry) {
			e.glowParam = &glow.Parameter{Number: 0, Identifier: "s0", Type: glow.ParamTypeString, Value: "IN 1"}
		})
		// parametersLocation base (1.6) with targets/sources/connections
		seedEntry(p, []int32{1, 6}, "ploc", func(e *treeEntry) {
			e.glowNode = &glow.Node{Number: 6, Identifier: "ploc"}
		})
		seedEntry(p, []int32{1, 6, 1}, "ptargets", func(e *treeEntry) {
			e.glowNode = &glow.Node{Number: 1, Identifier: "targets"}
		})
		seedEntry(p, []int32{1, 6, 1, 0}, "pt0", func(e *treeEntry) {
			e.glowNode = &glow.Node{Number: 0, Identifier: "0"}
		})
		seedEntry(p, []int32{1, 6, 1, 0, 1}, "pgain", func(e *treeEntry) {
			e.glowParam = &glow.Parameter{Number: 1, Identifier: "gain", Type: glow.ParamTypeInteger, Value: int64(3)}
		})
		// Function (1.8) with args + result
		seedEntry(p, []int32{1, 8}, "sum", func(e *treeEntry) {
			e.glowFunc = &glow.Function{
				Number: 8, Identifier: "sum", Description: "adder",
				Arguments: []glow.TupleItem{{Name: "a", Type: glow.ParamTypeInteger}},
				Result:    []glow.TupleItem{{Name: "r", Type: glow.ParamTypeInteger}},
			}
		})

		// Templates of each kind.
		p.templates["10.1"] = &glow.Template{
			Path: []int32{10, 1}, Description: "param template",
			Element: &glow.TemplateElement{Parameter: &glow.Parameter{Identifier: "tp", Type: glow.ParamTypeReal}},
		}
		p.templates["10.2"] = &glow.Template{
			Path: []int32{10, 2}, Description: "node template",
			Element: &glow.TemplateElement{Node: &glow.Node{Identifier: "tn"}},
		}
		p.templates["10.3"] = &glow.Template{
			Number: 3, Description: "matrix template",
			Element: &glow.TemplateElement{Matrix: &glow.Matrix{Identifier: "tm", MatrixType: glow.MatrixTypeOneToN}},
		}
		p.templates["10.4"] = &glow.Template{
			Path: []int32{10, 4}, Description: "func template",
			Element: &glow.TemplateElement{Function: &glow.Function{Identifier: "tf"}},
		}
		// A template with nil Element → buildTemplateEntry returns nil.
		p.templates["10.5"] = &glow.Template{Path: []int32{10, 5}}
		return p
	}

	for _, mode := range []string{"pointer", "inline", "both", "", "garbage"} {
		exp, err := build().Canonicalize(context.Background(), CanonicalOptions{
			Templates: mode, Labels: mode, Gain: mode,
		})
		if err != nil {
			t.Fatalf("Canonicalize(%q): %v", mode, err)
		}
		if exp.Root == nil {
			t.Fatalf("Canonicalize(%q): nil root", mode)
		}
	}
}

// TestCanonicalize_CancelledCtx covers the ctx-cancel early return.
func TestCanonicalize_CancelledCtx(t *testing.T) {
	p := freshPlugin()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Canonicalize(ctx, CanonicalOptions{}); err == nil {
		t.Error("cancelled ctx should error")
	}
}

// TestCanonicalize_BuildError covers the buildElement error propagation:
// an entry with no concrete glow pointer.
func TestCanonicalize_BuildError(t *testing.T) {
	p := freshPlugin()
	p.numIndex["1"] = &treeEntry{numericPath: []int32{1}, obj: consumer.Object{OID: "1"}}
	if _, err := p.Canonicalize(context.Background(), CanonicalOptions{}); err == nil {
		t.Error("entry with no glow type should error")
	}
}

// TestCanonicalize_MultiRoot covers the synthetic-root wrapping when the
// tree has more than one top-level element.
func TestCanonicalize_MultiRoot(t *testing.T) {
	p := freshPlugin()
	seedEntry(p, []int32{1}, "a", func(e *treeEntry) { e.glowNode = &glow.Node{Number: 1, Identifier: "a"} })
	seedEntry(p, []int32{2}, "b", func(e *treeEntry) { e.glowNode = &glow.Node{Number: 2, Identifier: "b"} })
	exp, err := p.Canonicalize(context.Background(), CanonicalOptions{})
	if err != nil {
		t.Fatalf("Canonicalize: %v", err)
	}
	if exp.Root.Common().Identifier != "" {
		t.Errorf("multi-root should wrap in a synthetic (empty-identifier) root, got %q", exp.Root.Common().Identifier)
	}
}

var _ = compliance.Profile{}
