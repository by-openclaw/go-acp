package ms05_test

// Tests for the embedded MS-05-02 framework models. Expected content
// comes from the spec's own model files (classes/1.1.json etc.) and
// the IS-14 class-descriptor example (own elements first, then
// inherited).

import (
	"testing"

	"dhs/internal/amwa/codec/ms05"
)

func TestStandardClassesLoad(t *testing.T) {
	cs := ms05.StandardClasses()
	if len(cs) < 6 {
		t.Fatalf("framework classes = %d, want the 6 published models", len(cs))
	}
	if _, ok := ms05.StandardClass(ms05.NcClassId{1}); !ok {
		t.Error("NcObject [1] missing")
	}
	dm, ok := ms05.StandardClass(ms05.NcClassId{1, 3, 1})
	if !ok {
		t.Fatal("NcDeviceManager [1,3,1] missing")
	}
	if dm.FixedRole == nil || *dm.FixedRole != "DeviceManager" {
		t.Errorf("DeviceManager fixedRole = %v", dm.FixedRole)
	}
	if _, ok := ms05.StandardClass(ms05.NcClassId{9, 9}); ok {
		t.Error("unknown class id must miss")
	}
}

func TestFlattenedClassInheritance(t *testing.T) {
	blk, ok := ms05.FlattenedClass(ms05.NcClassId{1, 1})
	if !ok {
		t.Fatal("NcBlock [1,1] missing")
	}
	// Own first: 2p1 enabled leads, per the IS-14 example.
	if blk.Properties[0].ID.Level != 2 || blk.Properties[0].ID.Index != 1 || blk.Properties[0].Name != "enabled" {
		t.Errorf("first property = %+v, want 2p1 enabled", blk.Properties[0])
	}
	// Inherited userLabel 1p6 must be present.
	foundUserLabel := false
	for _, p := range blk.Properties {
		if p.ID.Level == 1 && p.ID.Index == 6 {
			foundUserLabel = p.Name == "userLabel" && !p.IsReadOnly
		}
	}
	if !foundUserLabel {
		t.Error("inherited 1p6 userLabel missing or wrong")
	}
	// Methods: own 2m1..2m4 AND inherited 1m1..1m7.
	var own, inherited int
	for _, m := range blk.Methods {
		switch m.ID.Level {
		case 2:
			own++
		case 1:
			inherited++
		}
	}
	if own != 4 || inherited != 7 {
		t.Errorf("NcBlock methods own=%d inherited=%d, want 4 and 7", own, inherited)
	}
	// NcObject flattened == own (no ancestors).
	obj, _ := ms05.FlattenedClass(ms05.NcClassId{1})
	if len(obj.Properties) != 8 {
		t.Errorf("NcObject properties = %d, want 8", len(obj.Properties))
	}
	if len(obj.Events) != 1 {
		t.Errorf("NcObject events = %d, want 1 (PropertyChanged)", len(obj.Events))
	}
}

func TestStandardDatatypes(t *testing.T) {
	dts := ms05.StandardDatatypes()
	if len(dts) < 40 {
		t.Fatalf("framework datatypes = %d, want the published models + primitives", len(dts))
	}
	b, ok := ms05.StandardDatatype("NcBoolean")
	if !ok || b.Type != ms05.NcDatatypeTypePrimitive {
		t.Errorf("NcBoolean = %+v ok=%v, want primitive", b, ok)
	}
	bmd, ok := ms05.StandardDatatype("NcBlockMemberDescriptor")
	if !ok || bmd.Type != ms05.NcDatatypeTypeStruct || len(bmd.Fields) == 0 {
		t.Errorf("NcBlockMemberDescriptor = %+v ok=%v, want struct with fields", bmd.Type, ok)
	}
	if _, ok := ms05.StandardDatatype("NcNotAThing"); ok {
		t.Error("unknown datatype must miss")
	}
}
