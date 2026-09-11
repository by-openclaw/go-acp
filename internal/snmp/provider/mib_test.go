package provider

// The tree is what a walk walks. Its whole contract is ordering — a
// manager that has no MIB file discovers the device by asking for the
// next object, over and over — so these tests are mostly about order and
// about the two ways an object can be absent.

import (
	"strings"
	"testing"

	"dhs/internal/snmp/codec"
)

func oid(s string) codec.OID { return codec.MustParseOID(s) }

// sysGroup is the RFC 1213 system group, the branch every manager reads
// first and the one that makes this agent identifiable at all.
func sysGroup() []Object {
	return []Object{
		Scalar(oid("1.3.6.1.2.1.1.1.0"), codec.String("dhs SNMP agent")),
		Scalar(oid("1.3.6.1.2.1.1.2.0"), codec.ObjectID(oid("1.3.6.1.4.1.1773"))),
		Live(oid("1.3.6.1.2.1.1.3.0"), codec.TypeTimeTicks,
			func() codec.Value { return codec.TimeTicks(42) }),
	}
}

func newTree(t *testing.T, objs ...Object) *MIB {
	t.Helper()
	m := NewMIB()
	if err := m.Register(objs...); err != nil {
		t.Fatalf("Register: %v", err)
	}
	return m
}

// Objects come back in lexicographic OID order however they were
// registered: that order IS the walk, so a tree that kept insertion
// order would hand a manager a table with its rows shuffled.
func TestTheTreeIsSortedHoweverItWasBuilt(t *testing.T) {
	m := newTree(t,
		Scalar(oid("1.3.6.1.2.1.1.9.0"), codec.Int(9)),
		Scalar(oid("1.3.6.1.2.1.1.10.0"), codec.Int(10)),
		Scalar(oid("1.3.6.1.2.1.1.1.0"), codec.Int(1)),
	)

	var walked []string
	cur := codec.OID(nil)
	for {
		obj, ok := m.Next(cur)
		if !ok {
			break
		}
		walked = append(walked, obj.OID.String())
		cur = obj.OID
	}

	want := []string{"1.3.6.1.2.1.1.1.0", "1.3.6.1.2.1.1.9.0", "1.3.6.1.2.1.1.10.0"}
	if strings.Join(walked, " ") != strings.Join(want, " ") {
		t.Errorf("walk = %v, want %v", walked, want)
	}
	if m.Len() != 3 {
		t.Errorf("Len = %d, want 3", m.Len())
	}
}

// A walk of the whole agent starts at First, and it is the same object
// Next(nil) gives — a manager sends GETNEXT with an empty or a root OID
// and expects the first thing the device has.
func TestFirstIsWhereAWalkStarts(t *testing.T) {
	m := newTree(t, sysGroup()...)
	first, ok := m.First()
	if !ok || first.OID.String() != "1.3.6.1.2.1.1.1.0" {
		t.Fatalf("First = %v, %v", first.OID, ok)
	}

	empty := NewMIB()
	if _, ok := empty.First(); ok {
		t.Error("an empty tree has no first object")
	}
}

// A structural node is stepped over, not returned: a walk that stopped
// on a table's own OID would hand the manager a name with no value and
// then be asked for the next one from there anyway.
func TestAWalkStepsOverStructuralNodes(t *testing.T) {
	m := newTree(t,
		Object{OID: oid("1.3.6.1.2.1.2.2"), Access: NotAccessible},   // ifTable
		Object{OID: oid("1.3.6.1.2.1.2.2.1"), Access: NotAccessible}, // ifEntry
		Scalar(oid("1.3.6.1.2.1.2.2.1.1.1"), codec.Int(1)),
	)

	obj, ok := m.Next(oid("1.3.6.1.2.1.2"))
	if !ok {
		t.Fatal("the walk stopped at the table")
	}
	if obj.OID.String() != "1.3.6.1.2.1.2.2.1.1.1" {
		t.Errorf("next = %s, want the first accessible instance", obj.OID)
	}
}

// The end of the tree is a fact the caller has to be able to see: it is
// what becomes endOfMibView, and what ends a manager's walk.
func TestTheEndOfTheTree(t *testing.T) {
	m := newTree(t, sysGroup()...)
	if _, ok := m.Next(oid("1.3.6.1.2.1.1.3.0")); ok {
		t.Error("there is nothing after the last object")
	}
	if _, ok := m.Next(oid("9.9.9")); ok {
		t.Error("there is nothing after an OID past the tree")
	}
}

// Get is exact. sysDescr and sysDescr.0 are different names and only one
// of them has a value.
func TestGetIsExact(t *testing.T) {
	m := newTree(t, sysGroup()...)
	if _, ok := m.Get(oid("1.3.6.1.2.1.1.1.0")); !ok {
		t.Error("sysDescr.0 must resolve")
	}
	if _, ok := m.Get(oid("1.3.6.1.2.1.1.1")); ok {
		t.Error("sysDescr without an instance must not resolve")
	}
	if _, ok := m.Get(oid("1.3.6.1.2.1.99.0")); ok {
		t.Error("an OID past the tree must not resolve")
	}
	if _, ok := NewMIB().Get(oid("1.3.6.1")); ok {
		t.Error("an empty tree resolves nothing")
	}
}

// Registration refuses a wiring mistake rather than starting an agent
// that answers a manager with a nil dereference or a silently read-only
// object.
func TestRegisterRefusals(t *testing.T) {
	for _, tc := range []struct {
		name string
		obj  Object
		want string
	}{
		{"no OID", Object{Access: ReadOnly, Get: func() codec.Value { return codec.Null() }},
			"no OID"},
		{"readable with no getter", Object{OID: oid("1.3.6.1"), Access: ReadOnly},
			"has no Get"},
		{"writable with no setter", Object{OID: oid("1.3.6.1"), Access: ReadWrite,
			Get: func() codec.Value { return codec.Null() }}, "has no Set"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := NewMIB().Register(tc.obj); err == nil ||
				!strings.Contains(err.Error(), tc.want) {
				t.Fatalf("= %v, want %q", err, tc.want)
			}
		})
	}

	// A structural node needs no getter, because nothing ever reads it.
	if err := NewMIB().Register(Object{OID: oid("1.3.6.1.2.1.2.2")}); err != nil {
		t.Errorf("a not-accessible node needs no getter: %v", err)
	}

	// Two objects at one OID is a tree where a walk's answer depends on
	// which copy the search lands on.
	m := newTree(t, Scalar(oid("1.3.6.1.2.1.1.1.0"), codec.Int(1)))
	if err := m.Register(Scalar(oid("1.3.6.1.2.1.1.1.0"), codec.Int(2))); err == nil ||
		!strings.Contains(err.Error(), "already registered") {
		t.Errorf("= %v, want the duplicate refused", err)
	}
}

func TestMustRegisterPanicsOnOurOwnMistake(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("a wiring mistake in our own tree must panic")
		}
	}()
	NewMIB().MustRegister(Object{})
}

// The three constructors are the shapes a tree is actually built from,
// and each has to produce an object the agent will serve.
func TestObjectConstructors(t *testing.T) {
	var stored int64 = 1

	m := newTree(t,
		Scalar(oid("1.3.6.1.4.1.1773.1.0"), codec.String("fixed")),
		Live(oid("1.3.6.1.4.1.1773.2.0"), codec.TypeInteger,
			func() codec.Value { return codec.Int(stored) }),
		Writable(oid("1.3.6.1.4.1.1773.3.0"), codec.TypeInteger,
			func() codec.Value { return codec.Int(stored) },
			func(v codec.Value) error { stored = v.Int; return nil }),
	)

	fixed, _ := m.Get(oid("1.3.6.1.4.1.1773.1.0"))
	if fixed.Get().String() != "fixed" || fixed.Access != ReadOnly {
		t.Errorf("Scalar = %s/%s", fixed.Get(), fixed.Access)
	}

	live, _ := m.Get(oid("1.3.6.1.4.1.1773.2.0"))
	stored = 7
	if live.Get().Int != 7 {
		t.Errorf("Live read %d, want the value at read time", live.Get().Int)
	}

	rw, _ := m.Get(oid("1.3.6.1.4.1.1773.3.0"))
	if rw.Access != ReadWrite {
		t.Fatalf("Writable = %s", rw.Access)
	}
	if err := rw.Set(codec.Int(9)); err != nil || stored != 9 {
		t.Errorf("Set: %v, stored=%d", err, stored)
	}
}

func TestAccessNames(t *testing.T) {
	for _, tc := range []struct {
		a    Access
		want string
	}{
		{NotAccessible, "not-accessible"},
		{ReadOnly, "read-only"},
		{ReadWrite, "read-write"},
		{Access(9), "access(9)"},
	} {
		if got := tc.a.String(); got != tc.want {
			t.Errorf("= %q, want %q", got, tc.want)
		}
	}
}
