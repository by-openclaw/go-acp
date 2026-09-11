package ms05

import (
	"encoding/json"
	"strings"
	"testing"

	"dhs/internal/amwa/codec/spec"
)

func u32Ptr(u uint32) *uint32 { return &u }
func sPtr(s string) *string   { return &s }

// A constraint is checked by value or by pointer, on either the
// property or the parameter variant — a caller holding a descriptor's
// `constraints` field as `any` must not have to unwrap it first.
func TestCheckConstraintValueAcceptsEveryVariant(t *testing.T) {
	num := NcParameterConstraintsNumber{Minimum: 0, Maximum: 10, Step: 2}
	propNum := NcPropertyConstraintsNumber{Minimum: 0, Maximum: 10, Step: 2}
	str := NcParameterConstraintsString{MaxCharacters: u32Ptr(3)}
	propStr := NcPropertyConstraintsString{MaxCharacters: u32Ptr(3)}

	for name, c := range map[string]any{
		"a number parameter constraint by value":   num,
		"a number parameter constraint by pointer": &num,
		"a number property constraint by value":    propNum,
		"a number property constraint by pointer":  &propNum,
	} {
		t.Run(name, func(t *testing.T) {
			if err := CheckConstraintValue(4.0, c); err != nil {
				t.Errorf("an in-range value = %v", err)
			}
			if err := CheckConstraintValue(11.0, c); err == nil {
				t.Error("a value above the maximum was accepted")
			}
		})
	}
	for name, c := range map[string]any{
		"a string parameter constraint by value":   str,
		"a string parameter constraint by pointer": &str,
		"a string property constraint by value":    propStr,
		"a string property constraint by pointer":  &propStr,
	} {
		t.Run(name, func(t *testing.T) {
			if err := CheckConstraintValue("abc", c); err != nil {
				t.Errorf("a short-enough value = %v", err)
			}
			if err := CheckConstraintValue("abcd", c); err == nil {
				t.Error("a value above the character maximum was accepted")
			}
		})
	}

	// A nil value, a nil constraint and a base-only constraint each
	// constrain nothing — nullability is a separate descriptor rule.
	if err := CheckConstraintValue(nil, num); err != nil {
		t.Errorf("a null value = %v", err)
	}
	if err := CheckConstraintValue(1.0, nil); err != nil {
		t.Errorf("no constraint = %v", err)
	}
	if err := CheckConstraintValue(1.0, NcPropertyConstraints{}); err != nil {
		t.Errorf("a bare default-value constraint = %v", err)
	}
}

// The numeric members are authored as int or float literals, so the
// checker coerces whatever width the decoder produced — and refuses a
// value that is not a number at all.
func TestNumericConstraintCoercionAndRules(t *testing.T) {
	for name, value := range map[string]any{
		"float64": float64(4),
		"float32": float32(4),
		"int":     int(4),
		"int32":   int32(4),
		"int64":   int64(4),
		"uint32":  uint32(4),
		"uint64":  uint64(4),
	} {
		t.Run(name, func(t *testing.T) {
			if err := CheckConstraintValue(value, NcParameterConstraintsNumber{Minimum: 0, Maximum: 10}); err != nil {
				t.Errorf("= %v", err)
			}
		})
	}
	if err := CheckConstraintValue("four", NcParameterConstraintsNumber{}); err == nil ||
		!strings.Contains(err.Error(), "not numeric") {
		t.Error("a non-numeric value must be refused by a numeric constraint")
	}

	if err := CheckConstraintValue(-1.0, NcParameterConstraintsNumber{Minimum: 0}); err == nil ||
		!strings.Contains(err.Error(), "below the constraint minimum") {
		t.Error("a value below the minimum must be refused")
	}
	// Steps count from the declared minimum, and from zero when there
	// is none (MS-05-02 Constraints.html).
	if err := CheckConstraintValue(5.5, NcParameterConstraintsNumber{Minimum: 0.5, Step: 0.5}); err != nil {
		t.Errorf("a value on the step grid = %v", err)
	}
	if err := CheckConstraintValue(5.7, NcParameterConstraintsNumber{Minimum: 0.5, Step: 0.5}); err == nil ||
		!strings.Contains(err.Error(), "does not align") {
		t.Error("a value off the step grid must be refused")
	}
	if err := CheckConstraintValue(4.0, NcParameterConstraintsNumber{Step: 2}); err != nil {
		t.Errorf("a step counted from zero = %v", err)
	}
	// A non-positive step constrains nothing rather than dividing by
	// zero.
	if err := CheckConstraintValue(3.0, NcParameterConstraintsNumber{Step: 0}); err != nil {
		t.Errorf("a zero step = %v", err)
	}
}

// The string checker holds two rules: a character count (runes, not
// bytes — a device label is text) and a pattern the device published.
func TestStringConstraintRules(t *testing.T) {
	if err := CheckConstraintValue(42, NcParameterConstraintsString{}); err == nil ||
		!strings.Contains(err.Error(), "not a string") {
		t.Error("a non-string value must be refused by a string constraint")
	}
	// Three runes, more than three bytes.
	if err := CheckConstraintValue("héé", NcParameterConstraintsString{MaxCharacters: u32Ptr(3)}); err != nil {
		t.Errorf("the count is in runes, not bytes: %v", err)
	}
	if err := CheckConstraintValue("abc", NcParameterConstraintsString{Pattern: sPtr("^[a-z]+$")}); err != nil {
		t.Errorf("a matching value = %v", err)
	}
	if err := CheckConstraintValue("ABC", NcParameterConstraintsString{Pattern: sPtr("^[a-z]+$")}); err == nil ||
		!strings.Contains(err.Error(), "does not match") {
		t.Error("a value outside the pattern must be refused")
	}
	if err := CheckConstraintValue("abc", NcParameterConstraintsString{Pattern: sPtr("")}); err != nil {
		t.Errorf("an empty pattern constrains nothing: %v", err)
	}
	// A pattern the device published but Go cannot compile is
	// reported against the constraint, not blamed on the value.
	if err := CheckConstraintValue("abc", NcParameterConstraintsString{Pattern: sPtr("[")}); err == nil ||
		!strings.Contains(err.Error(), "does not compile") {
		t.Error("an uncompilable pattern must be reported")
	}
}

// Matching a runtime constraint to the property being written needs
// its propertyId, which every property-level variant carries and no
// parameter-level one does. constraints_test.go covers the pointer
// forms; this covers the by-value ones, which is how a decoded
// descriptor's `constraints` field usually arrives.
func TestConstraintPropertyIDByValue(t *testing.T) {
	id := NcPropertyId{Level: 3, Index: 1}
	base := NcPropertyConstraints{PropertyId: id}
	for name, c := range map[string]any{
		"a number property constraint":            NcPropertyConstraintsNumber{NcPropertyConstraints: base},
		"a string property constraint":            NcPropertyConstraintsString{NcPropertyConstraints: base},
		"a base property constraint":              base,
		"a base property constraint by pointer":   &base,
		"a string property constraint by pointer": &NcPropertyConstraintsString{NcPropertyConstraints: base},
	} {
		t.Run(name, func(t *testing.T) {
			got, ok := ConstraintPropertyID(c)
			if !ok || got != id {
				t.Errorf("= %+v, %v; want %+v", got, ok, id)
			}
		})
	}
	if _, ok := ConstraintPropertyID(NcParameterConstraintsString{}); ok {
		t.Error("a parameter constraint names no property")
	}
	if _, ok := ConstraintPropertyID(nil); ok {
		t.Error("no constraint names no property")
	}
}

// The framework catalogue is the spec's own data: a device model
// answers from it, so what a controller reads back must carry every
// inherited field, not just the leaf's own.
func TestFlattenedDatatypesMergeInheritedFields(t *testing.T) {
	// NcDatatypeDescriptorEnum inherits through NcDatatypeDescriptor
	// to NcDescriptor — three levels, which is why one level of
	// merging is not enough.
	leaf, ok := StandardDatatype("NcDatatypeDescriptorEnum")
	if !ok {
		t.Fatal("the framework must carry NcDatatypeDescriptorEnum")
	}
	flat, ok := FlattenedDatatype("NcDatatypeDescriptorEnum")
	if !ok {
		t.Fatal("FlattenedDatatype must answer for a struct the framework carries")
	}
	if len(flat.Fields) <= len(leaf.Fields) {
		t.Errorf("flattened %d fields, own %d — inheritance was not merged",
			len(flat.Fields), len(leaf.Fields))
	}
	// Own fields come first, then each ancestor's, in chain order.
	if len(leaf.Fields) > 0 && flat.Fields[0].Name != leaf.Fields[0].Name {
		t.Errorf("first field = %q, want the leaf's own %q", flat.Fields[0].Name, leaf.Fields[0].Name)
	}

	// A primitive has no parent, so flattening returns it as it is.
	prim, ok := FlattenedDatatype("NcString")
	if !ok || prim.Type != NcDatatypeTypePrimitive || len(prim.Fields) != 0 {
		t.Errorf("a flattened primitive = %+v", prim)
	}
	if _, ok := FlattenedDatatype("NcNotADatatype"); ok {
		t.Error("FlattenedDatatype answered for a name the framework does not carry")
	}

	all := FlattenedDatatypes()
	if len(all) != len(StandardDatatypes()) {
		t.Errorf("the flattened catalogue lists %d datatypes, the standard one %d",
			len(all), len(StandardDatatypes()))
	}
	for i := 1; i < len(all); i++ {
		if all[i-1].Name > all[i].Name {
			t.Fatalf("the catalogue must be sorted by name: %q before %q", all[i-1].Name, all[i].Name)
		}
	}
}

// A non-standard class or datatype joins the catalogue the device
// model publishes, and re-registering one replaces it — provider
// construction runs more than once in a process.
func TestRegisterNonStandardEntries(t *testing.T) {
	id := NcClassId{1, 2, 3, 0, 42} // 0 authority key: non-standard
	desc := "a vendor class"
	if err := RegisterClass(NcClassDescriptor{
		NcDescriptor: NcDescriptor{Description: &desc}, ClassID: id, Name: "NcVendorThing",
	}); err != nil {
		t.Fatalf("RegisterClass: %v", err)
	}
	got, ok := StandardClass(id)
	if !ok || got.Name != "NcVendorThing" {
		t.Fatalf("StandardClass = %+v, %v", got, ok)
	}
	if err := RegisterClass(NcClassDescriptor{ClassID: id, Name: "NcVendorThing2"}); err != nil {
		t.Fatalf("re-registering: %v", err)
	}
	if got, _ := StandardClass(id); got.Name != "NcVendorThing2" {
		t.Errorf("re-registration must replace the entry: %+v", got)
	}

	if err := RegisterDatatype(NcDatatypeDescriptor{
		Name: "NcVendorDatatype", Type: NcDatatypeTypePrimitive,
	}); err != nil {
		t.Fatalf("RegisterDatatype: %v", err)
	}
	if got, ok := StandardDatatype("NcVendorDatatype"); !ok || got.Type != NcDatatypeTypePrimitive {
		t.Errorf("StandardDatatype = %+v, %v", got, ok)
	}

	// A class id the framework does not carry is a miss, not an
	// empty descriptor that reads as one.
	if _, ok := StandardClass(NcClassId{9, 9, 9}); ok {
		t.Error("StandardClass answered for a class nobody registered")
	}
	if _, ok := FlattenedClass(NcClassId{9, 9, 9}); ok {
		t.Error("FlattenedClass answered for a class nobody registered")
	}
}

// Each datatype variant writes exactly its own keys: `constraints`
// stays even when null (the schema requires it), and a `fields` key
// on a primitive would satisfy the wrong branch of the schema's
// oneOf.
func TestDatatypeDescriptorMarshalsItsVariant(t *testing.T) {
	keysOf := func(t *testing.T, d NcDatatypeDescriptor) map[string]bool {
		t.Helper()
		raw, err := json.Marshal(d)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("unmarshal: %v (%s)", err, raw)
		}
		out := map[string]bool{}
		for k := range m {
			out[k] = true
		}
		return out
	}

	base := []string{"description", "name", "type", "constraints"}

	prim := keysOf(t, NcDatatypeDescriptor{Name: "NcString", Type: NcDatatypeTypePrimitive})
	for _, k := range base {
		if !prim[k] {
			t.Errorf("a primitive must carry %q", k)
		}
	}
	for _, k := range []string{"fields", "items", "parentType", "isSequence"} {
		if prim[k] {
			t.Errorf("a primitive must not carry %q", k)
		}
	}

	typedef := keysOf(t, NcDatatypeDescriptor{
		Name: "NcRolePath", Type: NcDatatypeTypeTypedef, ParentType: sPtr("NcString"),
	})
	if !typedef["parentType"] || !typedef["isSequence"] {
		t.Errorf("a typedef carries parentType and isSequence: %v", typedef)
	}
	if typedef["fields"] || typedef["items"] {
		t.Errorf("a typedef carries no fields or items: %v", typedef)
	}

	// A struct with no fields still writes an empty array, never null.
	structKeys := keysOf(t, NcDatatypeDescriptor{Name: "NcThing", Type: NcDatatypeTypeStruct})
	if !structKeys["fields"] || !structKeys["parentType"] {
		t.Errorf("a struct carries fields and parentType: %v", structKeys)
	}
	raw, err := json.Marshal(NcDatatypeDescriptor{Name: "NcThing", Type: NcDatatypeTypeStruct})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"fields":[]`) {
		t.Errorf("an empty struct must write an empty fields array: %s", raw)
	}

	enum := keysOf(t, NcDatatypeDescriptor{Name: "NcThingEnum", Type: NcDatatypeTypeEnum})
	if !enum["items"] {
		t.Errorf("an enum carries items: %v", enum)
	}
	if enum["fields"] || enum["parentType"] {
		t.Errorf("an enum carries no fields or parentType: %v", enum)
	}
	raw, err = json.Marshal(NcDatatypeDescriptor{Name: "NcThingEnum", Type: NcDatatypeTypeEnum})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"items":[]`) {
		t.Errorf("an empty enum must write an empty items array: %s", raw)
	}
}

// stubCodec is an MS-05-02 codec used to exercise the registry.
type stubCodec struct{ ver string }

func (c stubCodec) SpecID() string    { return SpecID }
func (c stubCodec) APIVer() string    { return c.ver }
func (c stubCodec) SpecPatch() string { return c.ver + ".0" }

var _ Codec = stubCodec{}

type wrongSpec struct{ stubCodec }

func (wrongSpec) SpecID() string { return "is-04" }

// The per-minor registry answers by version, negotiates with a peer,
// and refuses a codec registered under another spec.
func TestCodecRegistry(t *testing.T) {
	Register(stubCodec{ver: "v1.1"})
	Register(stubCodec{ver: "v1.1"}) // idempotent

	if c, ok := Get("v1.1"); !ok || c.APIVer() != "v1.1" {
		t.Errorf("Get(v1.1) = %v, %v", c, ok)
	}
	if _, ok := Get("v9.9"); ok {
		t.Error("Get answered for a minor nobody registered")
	}
	if len(AllCodecs()) == 0 {
		t.Error("AllCodecs must list what is registered")
	}
	vers := SupportedVersions()
	if len(vers) == 0 {
		t.Fatal("SupportedVersions must list what is registered")
	}
	if Default().APIVer() != vers[len(vers)-1] {
		t.Errorf("Default = %q, want the newest minor", Default().APIVer())
	}
	if c, err := SelectHighest([]string{"v1.1", "v9.9"}); err != nil || c.APIVer() != "v1.1" {
		t.Errorf("SelectHighest = %v, %v", c, err)
	}
	if _, err := SelectHighest([]string{"v9.9"}); err == nil {
		t.Error("a peer with no mutual minor must be reported")
	}

	func() {
		defer func() {
			if recover() == nil {
				t.Error("registering a codec for another spec must panic")
			}
		}()
		Register(wrongSpec{})
	}()

	prev := versions
	versions = spec.NewRegistry[Codec]()
	defer func() {
		versions = prev
		if r := recover(); r == nil {
			t.Error("Default with no codec registered must panic")
		}
	}()
	_ = Default()
}

// A framework that could not be loaded — a corrupted embed — makes
// every catalogue accessor answer "nothing", never a half-built model
// a controller would read as the device's truth.
func TestFrameworkAccessorsDegradeWhenTheModelsAreUnreadable(t *testing.T) {
	prev := frameworkErr
	frameworkErr = errBrokenFramework
	t.Cleanup(func() { frameworkErr = prev })

	if _, ok := StandardClass(NcClassId{1}); ok {
		t.Error("StandardClass answered from an unloaded framework")
	}
	if _, ok := FlattenedClass(NcClassId{1}); ok {
		t.Error("FlattenedClass answered from an unloaded framework")
	}
	if _, ok := StandardDatatype("NcString"); ok {
		t.Error("StandardDatatype answered from an unloaded framework")
	}
	if _, ok := FlattenedDatatype("NcString"); ok {
		t.Error("FlattenedDatatype answered from an unloaded framework")
	}
	if got := StandardClasses(); got != nil {
		t.Errorf("StandardClasses = %v, want nothing", got)
	}
	if got := StandardDatatypes(); got != nil {
		t.Errorf("StandardDatatypes = %v, want nothing", got)
	}
	if got := FlattenedDatatypes(); got != nil {
		t.Errorf("FlattenedDatatypes = %v, want nothing", got)
	}
	if err := RegisterClass(NcClassDescriptor{ClassID: NcClassId{1, 0, 1}}); err == nil {
		t.Error("RegisterClass must report the load failure rather than registering into nothing")
	}
	if err := RegisterDatatype(NcDatatypeDescriptor{Name: "NcVendor"}); err == nil {
		t.Error("RegisterDatatype must report the load failure")
	}
}

var errBrokenFramework = errFramework("ms05: framework embed: scripted failure")

// errFramework is a minimal error type so the test does not import
// errors just to build one.
type errFramework string

func (e errFramework) Error() string { return string(e) }
