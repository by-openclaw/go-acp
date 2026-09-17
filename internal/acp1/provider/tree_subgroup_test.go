package acp1

import (
	"testing"

	"dhs/internal/acp1/codec"
	"dhs/internal/export/canonical"
)

func subParam(id int, ident string) *canonical.Parameter {
	return &canonical.Parameter{
		Header: canonical.Header{
			Number: id, Identifier: ident,
			OID:    "1.2.2." + ident,
			Access: canonical.AccessReadWrite,
		},
		Type: canonical.ParamInteger,
	}
}

func subGroup(ident string, children ...canonical.Element) *canonical.Node {
	return &canonical.Node{Header: canonical.Header{
		Number: 90, Identifier: ident,
		Access:   canonical.AccessRead,
		Children: children,
	}}
}

// A real Synapse rack nests most of its objects under section headers, and
// the consumer canonicalises those as parent Nodes. buildGroupNode documents
// the nesting as presentation only — every Parameter keeps its (group, id)
// wire address whatever its depth — so the provider has to descend.
//
// Before it did, extracting slot 1 of the rack at 10.6.250.105 produced 182
// parameters and the served copy answered for 76 of them: the 106 sitting
// under 8 sub-group nodes were silently absent, with no error anywhere.
func TestNewTreeRegistersParametersInsideSubGroups(t *testing.T) {
	exp := &canonical.Export{Root: &canonical.Node{Header: canonical.Header{
		Number: 1, Identifier: "device", Access: canonical.AccessRead,
		Children: []canonical.Element{
			&canonical.Node{Header: canonical.Header{
				Number: 1, Identifier: "slot-1", Access: canonical.AccessRead,
				Children: []canonical.Element{
					&canonical.Node{Header: canonical.Header{
						Number: 2, Identifier: "control", Access: canonical.AccessRead,
						Children: []canonical.Element{
							subParam(0, "Gain"), // above the first section
							subGroup("DOWN CONV",
								subParam(1, "Aspect"),
								subGroup("INSERTER", subParam(2, "Line")), // sections nest
							),
							subGroup("VIDEO PROC", subParam(3, "Black")),
						},
					}},
				},
			}},
		},
	}}}

	tr, err := newTree(exp)
	if err != nil {
		t.Fatalf("newTree: %v", err)
	}

	for id, ident := range map[uint8]string{0: "Gain", 1: "Aspect", 2: "Line", 3: "Black"} {
		e, ok := tr.lookup(objectKey{slot: 1, group: codec.GroupControl, id: id})
		if !ok {
			t.Errorf("control id %d (%s) is not addressable", id, ident)
			continue
		}
		if e.param.Identifier != ident {
			t.Errorf("control id %d = %q, want %q", id, e.param.Identifier, ident)
		}
	}

	// The group's object count has to span the nested ids too, or a walk
	// stops before reaching them.
	if got := tr.slots[1].numControl; got != 4 {
		t.Errorf("numControl = %d, want 4 — the count must cover nested ids", got)
	}
}

// A group with no sub-groups is unchanged by the recursion.
func TestNewTreeFlatGroupIsUnchanged(t *testing.T) {
	exp := &canonical.Export{Root: &canonical.Node{Header: canonical.Header{
		Number: 1, Identifier: "device", Access: canonical.AccessRead,
		Children: []canonical.Element{
			&canonical.Node{Header: canonical.Header{
				Number: 1, Identifier: "slot-1", Access: canonical.AccessRead,
				Children: []canonical.Element{
					&canonical.Node{Header: canonical.Header{
						Number: 2, Identifier: "control", Access: canonical.AccessRead,
						Children: []canonical.Element{subParam(0, "Gain"), subParam(1, "Level")},
					}},
				},
			}},
		},
	}}}

	tr, err := newTree(exp)
	if err != nil {
		t.Fatalf("newTree: %v", err)
	}
	if got := tr.slots[1].numControl; got != 2 {
		t.Errorf("numControl = %d, want 2", got)
	}
}

// collectParams ignores anything that is neither a Parameter nor a Node, so
// a future element kind cannot smuggle itself into the wire index.
func TestCollectParamsSkipsOtherElementKinds(t *testing.T) {
	got := collectParams([]canonical.Element{
		subParam(0, "Gain"),
		&canonical.Matrix{Header: canonical.Header{Number: 1, Identifier: "xpt"}},
		subGroup("SECTION", subParam(1, "Aspect")),
	})
	if len(got) != 2 {
		t.Fatalf("collected %d parameters, want 2", len(got))
	}
	if got[0].Identifier != "Gain" || got[1].Identifier != "Aspect" {
		t.Errorf("collected %q, %q", got[0].Identifier, got[1].Identifier)
	}
}
