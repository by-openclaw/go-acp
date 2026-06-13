package acp1

import (
	"testing"

	"dhs/internal/acp1/codec"
	"dhs/internal/export/canonical"
)

func paramWith(typ string, format string) *canonical.Parameter {
	p := &canonical.Parameter{Type: typ}
	if format != "" {
		f := format
		p.Format = &f
	}
	return p
}

// TestDeriveACPType_UnknownHints covers the integer/string/octets unknown-
// hint errors, the unsupported-canonical-type error, and the boolean-without-
// alarm error.
func TestDeriveACPType_UnknownHints(t *testing.T) {
	cases := []struct {
		name string
		p    *canonical.Parameter
		ok   bool
	}{
		{"int unknown bare hint", paramWith(canonical.ParamInteger, "weird"), false},
		{"int valid-but-wrong hint", paramWith(canonical.ParamInteger, "ipv4"), false},
		{"string unknown bare hint", paramWith(canonical.ParamString, "weird"), false},
		{"string valid-but-wrong hint", paramWith(canonical.ParamString, "int32"), false},
		{"octets non-frame", paramWith(canonical.ParamOctets, "weird"), false},
		{"boolean no alarm", paramWith(canonical.ParamBoolean, ""), false},
		{"unsupported type", paramWith("totally-made-up", ""), false},
		{"int32 ok", paramWith(canonical.ParamInteger, "int32"), true},
		{"byte ok", paramWith(canonical.ParamInteger, "byte"), true},
		{"ipv4 ok", paramWith(canonical.ParamString, "ipv4"), true},
		{"file ok", paramWith(canonical.ParamString, "file"), true},
		{"alarm ok", paramWith(canonical.ParamBoolean, "alarm"), true},
		{"frame ok", paramWith(canonical.ParamOctets, "frame"), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := deriveACPType(tc.p)
			if tc.ok && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if !tc.ok && err == nil {
				t.Error("expected error")
			}
		})
	}
}

// TestPickTypeHint_SkipsAttribute: a key=value attribute before a real hint
// is skipped (covers the '=' continue arm).
func TestPickTypeHint_SkipsAttribute(t *testing.T) {
	hint, ok := pickTypeHint([]string{"maxlen=16", "ipv4"})
	if !ok || hint != "ipv4" {
		t.Fatalf("got %q,%v want ipv4,true", hint, ok)
	}
}

// TestBroadcastsEnabled_ValueKinds drives every value-type arm of
// broadcastsEnabled for both the enum-map and bare-numeric branches.
func TestBroadcastsEnabled_ValueKinds(t *testing.T) {
	mk := func(val any, withMap bool) *tree {
		p := &canonical.Parameter{Value: val}
		if withMap {
			p.EnumMap = []canonical.EnumEntry{{Key: "Off", Value: 0}, {Key: "On", Value: 1}}
		}
		return &tree{
			entries: map[objectKey]*entry{
				{slot: 0, group: codec.GroupControl, id: 4}: {param: p},
			},
			slots: map[uint8]*slotCounts{},
		}
	}
	// Enum-map branch with each numeric kind = 1 ("On").
	for _, v := range []any{int64(1), uint64(1), int(1), float64(1)} {
		if !mk(v, true).broadcastsEnabled() {
			t.Errorf("enum-map %T(1) should be enabled", v)
		}
	}
	// Bare-numeric branch with each kind != 0.
	for _, v := range []any{int64(2), uint64(2), int(2), float64(2)} {
		if !mk(v, false).broadcastsEnabled() {
			t.Errorf("bare %T(2) should be enabled", v)
		}
	}
	// Bare value of an unhandled type → default permissive true.
	if !mk("weird", false).broadcastsEnabled() {
		t.Error("unhandled value type should default to enabled")
	}
	// Enum-map with an index that matches no entry → false.
	if mk(int64(9), true).broadcastsEnabled() {
		t.Error("enum index with no match should be disabled")
	}
}

// TestNewTree_SkipsNonNodeChildren: non-Node slot/group children and
// non-Parameter group children are skipped without error; status/alarm/file
// groups exercise their count arms.
func TestNewTree_SkipsNonNodeChildren(t *testing.T) {
	leaf := func(num int, ident string) *canonical.Parameter {
		return &canonical.Parameter{
			Header: canonical.Header{Number: num, Identifier: ident, Access: canonical.AccessRead},
			Type:   canonical.ParamInteger,
		}
	}
	group := func(name string, children ...canonical.Element) *canonical.Node {
		return &canonical.Node{Header: canonical.Header{Identifier: name, Access: canonical.AccessRead, Children: children}}
	}
	slot := &canonical.Node{Header: canonical.Header{
		Number: 1, Identifier: "slot-1", Access: canonical.AccessRead,
		Children: []canonical.Element{
			// A Parameter directly under the slot (not a Node) → skipped.
			leaf(0, "stray"),
			// Group with an unknown name → skipped.
			group("not-a-group", leaf(0, "x")),
			// status / alarm / file groups → count arms.
			group("status", leaf(0, "St")),
			group("alarm", leaf(0, "Al")),
			group("file", leaf(0, "Fi")),
		},
	}}
	root := &canonical.Node{Header: canonical.Header{
		Identifier: "device", Access: canonical.AccessRead,
		Children: []canonical.Element{
			leaf(99, "stray-top"), // non-Node slot child → skipped
			slot,
		},
	}}
	exp := &canonical.Export{Root: root}
	tr, err := newTree(exp)
	if err != nil {
		t.Fatalf("newTree: %v", err)
	}
	c := tr.slots[1]
	if c == nil || c.numStatus == 0 || c.numAlarm == 0 || c.numFile == 0 {
		t.Fatalf("expected status/alarm/file counts, got %+v", c)
	}
}

// TestNewTree_DeriveACPTypeError: a parameter with an unsupported type makes
// newTree fail.
func TestNewTree_DeriveACPTypeError(t *testing.T) {
	bad := &canonical.Parameter{
		Header: canonical.Header{Number: 0, Identifier: "Bad", Access: canonical.AccessRead},
		Type:   canonical.ParamBoolean, // no "alarm" hint → deriveACPType error
	}
	group := &canonical.Node{Header: canonical.Header{Identifier: "control", Access: canonical.AccessRead,
		Children: []canonical.Element{bad}}}
	slot := &canonical.Node{Header: canonical.Header{Number: 1, Identifier: "slot-1", Access: canonical.AccessRead,
		Children: []canonical.Element{group}}}
	root := &canonical.Node{Header: canonical.Header{Identifier: "device", Access: canonical.AccessRead,
		Children: []canonical.Element{slot}}}
	if _, err := newTree(&canonical.Export{Root: root}); err == nil {
		t.Fatal("unsupported param type: want error")
	}
}
