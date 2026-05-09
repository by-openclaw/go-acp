package acp2

import (
	"testing"

	"dhs/internal/export/canonical"
)

// TestDisabledFilter_LeafSkipped pins spec acp2_protocol.docx
// §"Requirements" line 320: "Disabled parts of the menu are not
// visible to clients (not shown as children, options, etc)."
//
// A canonical Parameter with `disabled` in its Format hint MUST NOT
// appear in the per-slot obj-id index, MUST NOT be listed in its
// parent's pid=14 children, and MUST NOT be reachable via lookup.
func TestDisabledFilter_LeafSkipped(t *testing.T) {
	disabledFmt := "u32,disabled"
	enabledFmt := "u32"
	root := &canonical.Node{
		Header: canonical.Header{
			Number: 1, Identifier: "ROOT_NODE_V2",
			Children: []canonical.Element{
				&canonical.Parameter{
					Header: canonical.Header{Number: 100, Identifier: "Visible"},
					Type:   canonical.ParamInteger, Value: int64(0),
					Format: &enabledFmt,
				},
				&canonical.Parameter{
					Header: canonical.Header{Number: 200, Identifier: "Hidden"},
					Type:   canonical.ParamInteger, Value: int64(0),
					Format: &disabledFmt,
				},
			},
		},
	}
	slot := &canonical.Node{
		Header: canonical.Header{
			Number: 1, Identifier: "slot-1",
			Children: []canonical.Element{root},
		},
	}
	device := &canonical.Node{
		Header: canonical.Header{
			Number: 1, Identifier: "device",
			Children: []canonical.Element{slot},
		},
	}
	exp := &canonical.Export{Root: device}

	tr, err := newTree(exp)
	if err != nil {
		t.Fatalf("newTree: %v", err)
	}

	// The disabled obj-id MUST NOT be indexed.
	if _, ok := tr.lookup(1, 200); ok {
		t.Errorf("obj-id 200 (disabled) is indexed; spec §Requirements says it must be invisible")
	}
	// The visible peer is still indexed.
	if _, ok := tr.lookup(1, 100); !ok {
		t.Errorf("obj-id 100 (enabled) missing from index")
	}
	// Root's children list excludes the disabled obj-id.
	rootEntry, _ := tr.lookup(1, 1)
	if rootEntry == nil {
		t.Fatal("root not indexed")
	}
	for _, kid := range rootEntry.children {
		if kid == 200 {
			t.Errorf("root.children = %v; obj-id 200 (disabled) MUST be filtered out", rootEntry.children)
		}
	}
	if len(rootEntry.children) != 1 || rootEntry.children[0] != 100 {
		t.Errorf("root.children = %v; want [100] (only the enabled child)", rootEntry.children)
	}
}

// TestDisabledFilter_BareTokenShortCircuits verifies a Parameter
// whose Format is just "disabled" never reaches deriveACP2Type —
// the disabled flag check fires first, so a malformed token list
// doesn't get rejected by the unknown-type validator.
func TestDisabledFilter_BareTokenShortCircuits(t *testing.T) {
	fmt := "disabled"
	root := &canonical.Node{
		Header: canonical.Header{
			Number: 1, Identifier: "ROOT_NODE_V2",
			Children: []canonical.Element{
				&canonical.Parameter{
					Header: canonical.Header{Number: 50, Identifier: "Stub"},
					Type:   canonical.ParamInteger, Value: int64(0),
					Format: &fmt,
				},
			},
		},
	}
	slot := &canonical.Node{
		Header: canonical.Header{
			Number: 1, Identifier: "slot-1",
			Children: []canonical.Element{root},
		},
	}
	device := &canonical.Node{
		Header: canonical.Header{
			Number: 1, Identifier: "device",
			Children: []canonical.Element{slot},
		},
	}
	exp := &canonical.Export{Root: device}

	tr, err := newTree(exp)
	if err != nil {
		t.Fatalf("newTree: %v (disabled-only Format must short-circuit before type-derive)", err)
	}
	if _, ok := tr.lookup(1, 50); ok {
		t.Error("disabled-only Parameter is indexed; should be skipped")
	}
}
