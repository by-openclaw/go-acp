package ms05

import "testing"

func u32(v uint32) *uint32 { return &v }
func str(s string) *string { return &s }

func TestCheckConstraintValueNumber(t *testing.T) {
	c := &NcPropertyConstraintsNumber{
		NcPropertyConstraints: NcPropertyConstraints{PropertyId: NcPropertyId{Level: 4, Index: 2}},
		Minimum:               -60.0, Maximum: 6.0, Step: 0.5,
	}
	cases := []struct {
		name string
		v    any
		ok   bool
	}{
		{"in range on step", 3.5, true},
		{"minimum itself", -60.0, true},
		{"maximum itself", 6.0, true},
		{"above maximum", 6.5, false},
		{"below minimum", -60.5, false},
		{"off the step grid", 0.75, false},
		{"not numeric", "x", false},
		{"nil passes (nullability is elsewhere)", nil, true},
	}
	for _, tc := range cases {
		err := CheckConstraintValue(tc.v, c)
		if (err == nil) != tc.ok {
			t.Errorf("%s: err=%v, want ok=%v", tc.name, err, tc.ok)
		}
	}

	// Bounds authored as ints must coerce.
	ci := &NcParameterConstraintsNumber{Minimum: 0, Maximum: 10, Step: 1}
	if err := CheckConstraintValue(5.0, ci); err != nil {
		t.Errorf("int-authored bounds: %v", err)
	}
	if err := CheckConstraintValue(11.0, ci); err == nil {
		t.Error("11 above int-authored maximum accepted")
	}
}

func TestCheckConstraintValueString(t *testing.T) {
	c := &NcPropertyConstraintsString{
		NcPropertyConstraints: NcPropertyConstraints{PropertyId: NcPropertyId{Level: 4, Index: 1}},
		MaxCharacters:         u32(16),
		Pattern:               str("^[A-Za-z0-9 _-]*$"),
	}
	cases := []struct {
		name string
		v    any
		ok   bool
	}{
		{"fits", "Gain-2", true},
		{"empty", "", true},
		{"too long", "labels-longer-than-16", false},
		{"pattern break", "bad*chars", false},
		{"not a string", 3.0, false},
	}
	for _, tc := range cases {
		err := CheckConstraintValue(tc.v, c)
		if (err == nil) != tc.ok {
			t.Errorf("%s: err=%v, want ok=%v", tc.name, err, tc.ok)
		}
	}
}

func TestCheckConstraintValueBaseAndNil(t *testing.T) {
	if err := CheckConstraintValue(3.0, nil); err != nil {
		t.Errorf("nil constraint: %v", err)
	}
	// Base-only constraint (default value alone) constrains nothing.
	if err := CheckConstraintValue("anything at all, any length ->", &NcPropertyConstraints{}); err != nil {
		t.Errorf("base constraint: %v", err)
	}
}

func TestConstraintPropertyID(t *testing.T) {
	id := NcPropertyId{Level: 4, Index: 2}
	got, ok := ConstraintPropertyID(&NcPropertyConstraintsNumber{
		NcPropertyConstraints: NcPropertyConstraints{PropertyId: id},
	})
	if !ok || got != id {
		t.Errorf("ConstraintPropertyID = %v %v", got, ok)
	}
	if _, ok := ConstraintPropertyID(&NcParameterConstraintsNumber{}); ok {
		t.Error("parameter constraints carry no propertyId")
	}
}

func TestRegisterClassAndDatatype(t *testing.T) {
	cls := NcClassDescriptor{
		ClassID: NcClassId{1, 2, 0, 99},
		Name:    "TestVendorClass",
	}
	if err := RegisterClass(cls); err != nil {
		t.Fatalf("RegisterClass: %v", err)
	}
	got, ok := StandardClass(NcClassId{1, 2, 0, 99})
	if !ok || got.Name != "TestVendorClass" {
		t.Errorf("registered class lookup = %+v %v", got, ok)
	}
	// Flattening inherits through the standard prefix chain.
	flat, ok := FlattenedClass(NcClassId{1, 2, 0, 99})
	if !ok {
		t.Fatal("FlattenedClass on registered class")
	}
	seen := false
	for _, p := range flat.Properties {
		if p.Name == "oid" {
			seen = true
		}
	}
	if !seen {
		t.Error("registered class does not inherit NcObject properties")
	}

	dt := NcDatatypeDescriptor{Name: "TestVendorTypedef", Type: NcDatatypeTypeTypedef, ParentType: str("NcInt32")}
	if err := RegisterDatatype(dt); err != nil {
		t.Fatalf("RegisterDatatype: %v", err)
	}
	if _, ok := StandardDatatype("TestVendorTypedef"); !ok {
		t.Error("registered datatype lookup failed")
	}
}
