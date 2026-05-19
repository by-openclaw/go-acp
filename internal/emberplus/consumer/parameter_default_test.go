package emberplus

import (
	"context"
	"testing"

	"dhs/internal/emberplus/codec/glow"
	"dhs/internal/consumer"
)

// seedParameterWithDefault constructs a one-Parameter tree containing
// the given Default value, ready for ParameterDefault lookups. Helper
// used by every R14 absent-mode test.
func seedParameterWithDefault(kind consumer.ValueKind, glowDefault any) *Plugin {
	p := newSeedTestPlugin()
	p.numIndex = map[string]*treeEntry{}
	p.pathIndex = map[string]*treeEntry{}
	p.labelIndex = map[string][]*treeEntry{}
	entry := &treeEntry{
		numericPath: []int32{1, 6, 1},
		glowParam: &glow.Parameter{
			Identifier: "vInteger",
			Path:       []int32{1, 6, 1},
			Default:    glowDefault,
			Type:       glow.ParamTypeInteger,
		},
		obj: consumer.Object{
			OID:   "1.6.1",
			Label: "vInteger",
			Path:  []string{"types", "vInteger"},
			Value: consumer.Value{Kind: kind},
		},
	}
	p.numIndex["1.6.1"] = entry
	p.pathIndex["types.vInteger"] = entry
	p.labelIndex["vInteger"] = []*treeEntry{entry}
	return p
}

// TestParameterDefault_Int64 pins the happy path: a parameter declares
// Default=42 on the wire, ParameterDefault returns Kind=Int Int=42.
func TestParameterDefault_Int64(t *testing.T) {
	p := seedParameterWithDefault(consumer.KindInt, int64(42))
	v, has, err := p.ParameterDefault(context.Background(),
		consumer.ValueRequest{Path: "1.6.1", ID: -1})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if !has {
		t.Fatal("default should be present")
	}
	if v.Kind != consumer.KindInt || v.Int != 42 {
		t.Errorf("got %+v; want Kind=Int Int=42", v)
	}
}

// TestParameterDefault_Float pins float Default round-trips into KindFloat.
func TestParameterDefault_Float(t *testing.T) {
	p := seedParameterWithDefault(consumer.KindFloat, float64(3.14))
	v, has, err := p.ParameterDefault(context.Background(),
		consumer.ValueRequest{Path: "1.6.1", ID: -1})
	if err != nil || !has {
		t.Fatalf("unexpected: %v has=%v", err, has)
	}
	if v.Kind != consumer.KindFloat || v.Float != 3.14 {
		t.Errorf("got %+v; want Float=3.14", v)
	}
}

// TestParameterDefault_String pins string Default.
func TestParameterDefault_String(t *testing.T) {
	p := seedParameterWithDefault(consumer.KindString, "hello")
	v, has, err := p.ParameterDefault(context.Background(),
		consumer.ValueRequest{Path: "1.6.1", ID: -1})
	if err != nil || !has {
		t.Fatalf("unexpected: %v has=%v", err, has)
	}
	if v.Kind != consumer.KindString || v.Str != "hello" {
		t.Errorf("got %+v; want Str=hello", v)
	}
}

// TestParameterDefault_Bool pins bool Default.
func TestParameterDefault_Bool(t *testing.T) {
	p := seedParameterWithDefault(consumer.KindBool, true)
	v, has, err := p.ParameterDefault(context.Background(),
		consumer.ValueRequest{Path: "1.6.1", ID: -1})
	if err != nil || !has {
		t.Fatalf("unexpected: %v has=%v", err, has)
	}
	if v.Kind != consumer.KindBool || !v.Bool {
		t.Errorf("got %+v; want Bool=true", v)
	}
}

// TestParameterDefault_NoDefault — Parameter exists but has no Default
// on the wire. Callers map (false, nil) to validation:no-default-declared.
func TestParameterDefault_NoDefault(t *testing.T) {
	p := seedParameterWithDefault(consumer.KindInt, nil)
	v, has, err := p.ParameterDefault(context.Background(),
		consumer.ValueRequest{Path: "1.6.1", ID: -1})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if has {
		t.Errorf("default reported as present: %+v", v)
	}
}

// TestParameterDefault_NotFound — unknown path → error, not (false, nil).
func TestParameterDefault_NotFound(t *testing.T) {
	p := seedParameterWithDefault(consumer.KindInt, int64(42))
	_, _, err := p.ParameterDefault(context.Background(),
		consumer.ValueRequest{Path: "1.99.99", ID: -1})
	if err == nil {
		t.Fatal("expected error for unknown path")
	}
}
