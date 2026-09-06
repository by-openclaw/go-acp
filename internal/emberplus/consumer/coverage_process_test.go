package emberplus

import (
	"context"
	"testing"

	"dhs/internal/emberplus/codec/glow"
)

// TestProcessElements_AllMetaFields feeds qualified Node / Parameter /
// Matrix / Function elements that populate every optional field so the
// parameterMeta / matrixMeta / functionMeta / nodeMeta builders take
// each "field present" branch, and buildNode / buildMatrix's int32
// parametersLocation + templateReference branches run via a following
// Canonicalize.
func TestProcessElements_AllMetaFields(t *testing.T) {
	p := freshPlugin()

	root := &glow.Node{
		Path: []int32{1}, Number: 1, Identifier: "root", Description: "root",
		IsOnline: true, SchemaIdentifiers: "root-sch", TemplateReference: []int32{9, 1},
	}
	param := &glow.Parameter{
		Path: []int32{1, 1}, Number: 1, Identifier: "p", Type: glow.ParamTypeInteger,
		Value: int64(5), Description: "param", Format: "%d°dB", Formula: "x*2",
		Factor: 10, Enumeration: "a\nb", EnumMap: map[int64]string{0: "a", 1: "b"},
		HasStreamIdentifier: true, StreamIdentifier: 3,
		StreamDescriptor:  &glow.StreamDescription{Format: glow.StreamFmtFloat32BigEndian, Offset: 1},
		SchemaIdentifiers: "p-sch", TemplateReference: []int32{9, 2},
		Access: glow.AccessReadWrite, IsOnline: true,
	}
	// Untyped parameter whose type is inferred from the value.
	inferred := &glow.Parameter{
		Path: []int32{1, 2}, Number: 2, Identifier: "inf", Type: glow.ParamTypeNull,
		Value: "text", IsOnline: true,
	}
	matrix := &glow.Matrix{
		Path: []int32{1, 3}, Number: 3, Identifier: "m", Description: "matrix",
		MatrixType: glow.MatrixTypeNToN, AddressingMode: glow.MatrixAddrNonLinear,
		TargetCount: 2, SourceCount: 2, MaxTotalConnects: 4, MaxConnectsPerTarget: 2,
		ParametersLocation:  int32(5), // inline integer → the int32 meta/build branch
		GainParameterNumber: 1,
		Labels:              []glow.Label{{BasePath: []int32{1, 9}, Description: "Primary"}},
		SchemaIdentifiers:   "m-sch", TemplateReference: []int32{9, 3},
		Targets: []int32{0, 1}, Sources: []int32{0, 1},
		Connections: []glow.Connection{
			{Target: 0, Sources: []int32{1}, Operation: glow.ConnOpConnect, Disposition: glow.ConnDispLocked},
		},
	}
	fn := &glow.Function{
		Path: []int32{1, 4}, Number: 4, Identifier: "f", Description: "fn",
		Arguments:         []glow.TupleItem{{Name: "a", Type: glow.ParamTypeInteger}},
		Result:            []glow.TupleItem{{Name: "r", Type: glow.ParamTypeInteger}},
		TemplateReference: []int32{9, 4},
	}

	p.handleElements([]glow.Element{
		{Node: root}, {Parameter: param}, {Parameter: inferred},
		{Matrix: matrix}, {Function: fn},
	})

	for _, oid := range []string{"1", "1.1", "1.2", "1.3", "1.4"} {
		if _, ok := p.numIndex[oid]; !ok {
			t.Errorf("entry %s not indexed", oid)
		}
	}
	// Confirm meta fields surfaced.
	pe := p.numIndex["1.1"]
	if pe.obj.Meta["formula"] != "x*2" || pe.obj.Meta["factor"] != int64(10) {
		t.Errorf("parameter meta missing formula/factor: %v", pe.obj.Meta)
	}
	me := p.numIndex["1.3"]
	if me.obj.Meta["parametersLocation"] != int32(5) {
		t.Errorf("matrix meta int32 parametersLocation: %v", me.obj.Meta["parametersLocation"])
	}

	// Canonicalize to drive buildMatrix int32 parametersLocation +
	// templateReference build branches.
	if _, err := p.Canonicalize(context.Background(), CanonicalOptions{}); err != nil {
		t.Fatalf("Canonicalize: %v", err)
	}
}
