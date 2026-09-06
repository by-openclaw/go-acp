package acp2

import (
	"testing"

	"dhs/internal/acp2/codec"
	"dhs/internal/export/canonical"
)

func TestTree_FlattensAndIndexesBySlot(t *testing.T) {
	fmt := func(s string) *string { return &s }
	// Canonical layout mirrors what the acp2 consumer's Canonicalize
	// emits: device -> slot-1 -> ROOT_NODE_V2(1) -> BOARD(2) -> Label(3)
	label := &canonical.Parameter{
		Header: canonical.Header{
			Number: 3, Identifier: "UserLabel", OID: "1.1.3",
			Access: canonical.AccessReadWrite, Children: canonical.EmptyChildren(),
		},
		Type:   canonical.ParamString,
		Value:  "Input-A",
		Format: fmt("maxLen=16"),
	}
	board := &canonical.Node{
		Header: canonical.Header{
			Number: 2, Identifier: "BOARD", OID: "1.1.2",
			Access:   canonical.AccessRead,
			Children: []canonical.Element{label},
		},
	}
	root := &canonical.Node{
		Header: canonical.Header{
			Number: 1, Identifier: "ROOT_NODE_V2", OID: "1.1.1",
			Access:   canonical.AccessRead,
			Children: []canonical.Element{board},
		},
	}
	slot1 := &canonical.Node{
		Header: canonical.Header{
			Number: 1, Identifier: "slot-1", OID: "1.1",
			Access:   canonical.AccessRead,
			Children: []canonical.Element{root},
		},
	}
	exp := &canonical.Export{Root: &canonical.Node{
		Header: canonical.Header{
			Number: 1, Identifier: "device", OID: "1",
			Access:   canonical.AccessRead,
			Children: []canonical.Element{slot1},
		},
	}}

	tr, err := newTree(exp)
	if err != nil {
		t.Fatalf("newTree: %v", err)
	}
	// 3 unique entries (obj-ids 1, 2, 3) + obj-id 0 alias pointing at
	// obj-id 1 for spec-§Walk starts-at-0 compatibility.
	if tr.count() != 4 {
		t.Fatalf("count=%d want 4 (3 entries + obj-id=0 alias)", tr.count())
	}
	if e0, ok := tr.lookup(1, 0); !ok || e0.objID != 1 {
		t.Errorf("obj-id 0 alias missing or wrong: %+v", e0)
	}
	if tr.slotN != 1 {
		t.Errorf("slotN=%d want 1", tr.slotN)
	}

	// Lookup ROOT_NODE_V2
	e, ok := tr.lookup(1, 1)
	if !ok || e.objType != codec.ObjTypeNode {
		t.Fatalf("root lookup failed: %+v", e)
	}
	if len(e.children) != 1 || e.children[0] != 2 {
		t.Errorf("root children=%v want [2]", e.children)
	}

	// Lookup BOARD
	e, ok = tr.lookup(1, 2)
	if !ok || e.objType != codec.ObjTypeNode {
		t.Fatalf("board lookup failed: %+v", e)
	}
	if len(e.children) != 1 || e.children[0] != 3 {
		t.Errorf("board children=%v want [3]", e.children)
	}

	// Lookup UserLabel (string with maxLen=16)
	e, ok = tr.lookup(1, 3)
	if !ok {
		t.Fatal("label lookup failed")
	}
	if e.objType != codec.ObjTypeString || e.numType != codec.NumTypeString {
		t.Errorf("label obj=%d num=%d want String/NumString", e.objType, e.numType)
	}
	if e.access != 0x03 {
		t.Errorf("access=%02x want 03", e.access)
	}
	if got := maxLenHint(e.param); got != 16 {
		t.Errorf("maxLen=%d want 16", got)
	}

	// Unknown slot / obj
	if _, ok := tr.lookup(99, 1); ok {
		t.Error("lookup(99,1) should miss")
	}
	if _, ok := tr.lookup(1, 999); ok {
		t.Error("lookup(1,999) should miss")
	}
}

func TestDeriveACP2Type(t *testing.T) {
	fmt := func(s string) *string { return &s }
	param := func(typ string, hint string) *canonical.Parameter {
		p := &canonical.Parameter{Type: typ}
		if hint != "" {
			p.Format = fmt(hint)
		}
		return p
	}
	cases := []struct {
		name   string
		p      *canonical.Parameter
		obj    codec.ACP2ObjType
		num    codec.NumberType
		wantOK bool
	}{
		{"integer default = s32", param(canonical.ParamInteger, ""), codec.ObjTypeNumber, codec.NumTypeS32, true},
		{"integer s16", param(canonical.ParamInteger, "s16"), codec.ObjTypeNumber, codec.NumTypeS16, true},
		{"integer u8", param(canonical.ParamInteger, "u8"), codec.ObjTypeNumber, codec.NumTypeU8, true},
		{"integer u64", param(canonical.ParamInteger, "u64"), codec.ObjTypeNumber, codec.NumTypeU64, true},
		{"integer bad", param(canonical.ParamInteger, "u128"), 0, 0, false},
		{"real", param(canonical.ParamReal, ""), codec.ObjTypeNumber, codec.NumTypeFloat, true},
		{"enum", param(canonical.ParamEnum, ""), codec.ObjTypeEnum, codec.NumTypeU32, true},
		{"string default", param(canonical.ParamString, ""), codec.ObjTypeString, codec.NumTypeString, true},
		{"string + maxLen", param(canonical.ParamString, "maxLen=32"), codec.ObjTypeString, codec.NumTypeString, true},
		{"ipv4", param(canonical.ParamString, "ipv4"), codec.ObjTypeIPv4, codec.NumTypeIPv4, true},
		{"boolean REJECTED", param(canonical.ParamBoolean, ""), 0, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			obj, num, err := deriveACP2Type(tc.p)
			if tc.wantOK {
				if err != nil {
					t.Fatalf("err=%v", err)
				}
				if obj != tc.obj || num != tc.num {
					t.Fatalf("got obj=%d num=%d want obj=%d num=%d", obj, num, tc.obj, tc.num)
				}
				return
			}
			if err == nil {
				t.Fatalf("want error, got obj=%d num=%d", obj, num)
			}
		})
	}
}
