package provider

import (
	"strings"
	"testing"

	"dhs/internal/amwa/codec/ms05"
)

func namePtr(s string) *string { return &s }

// A property's declared datatype decides what a write may carry. The
// check classifies by JSON kind and never rejects what it cannot
// classify — the AMWA restore round offers a wrong-kind value and
// expects it reported (IS-14-01 test_26); applying it silently was
// the defect this closes.
func TestTypeMismatchClassifiesByDatatype(t *testing.T) {
	desc := func(typeName string, seq bool) *ms05.NcPropertyDescriptor {
		return &ms05.NcPropertyDescriptor{TypeName: namePtr(typeName), IsSequence: seq}
	}

	for name, tc := range map[string]struct {
		desc *ms05.NcPropertyDescriptor
		v    any
		want string // "" = accepted
	}{
		"a string property given a string":   {desc("NcString", false), "label", ""},
		"a string property given a number":   {desc("NcString", false), float64(1), "string expected"},
		"a boolean property given a boolean": {desc("NcBoolean", false), true, ""},
		"a boolean property given a string":  {desc("NcBoolean", false), "true", "boolean expected"},
		"a numeric property given a number":  {desc("NcUint32", false), float64(7), ""},
		"a numeric property given a string":  {desc("NcUint32", false), "7", "number expected"},
		"an enum property given a number":    {desc("NcConnectionStatus", false), float64(1), ""},
		"an enum property given a string":    {desc("NcConnectionStatus", false), "Healthy", "number expected"},
		"a struct property given an object": {
			desc("NcTouchpoint", false), map[string]any{"contextNamespace": "x"}, "",
		},
		"a struct property given a string": {desc("NcTouchpoint", false), "x", "object expected"},
		"a sequence property given an array": {
			desc("NcString", true), []any{"a", "b"}, "",
		},
		"a sequence property given a scalar": {desc("NcString", true), "a", "must be an array"},
		"a null is always accepted":          {desc("NcString", false), nil, ""},
		"a property with no declared type":   {&ms05.NcPropertyDescriptor{}, float64(1), ""},
		"a datatype the framework does not carry": {
			desc("NcVendorThing", false), "anything", "",
		},
	} {
		t.Run(name, func(t *testing.T) {
			got := typeMismatch(tc.desc, tc.v)
			if tc.want == "" {
				if got != "" {
					t.Errorf("= %q, want it accepted", got)
				}
				return
			}
			if !strings.Contains(got, tc.want) {
				t.Errorf("= %q, want a message mentioning %q", got, tc.want)
			}
		})
	}
}

// The JSON kind of a datatype is resolved through the framework
// catalogue, including a typedef chain — a controller writing
// NcRolePath must not be told it wrote the wrong kind.
func TestJSONKindFor(t *testing.T) {
	for name, want := range map[string]string{
		"NcString":           "string",
		"NcUri":              "string",
		"NcBoolean":          "bool",
		"NcUint64":           "number",
		"NcTimeInterval":     "number",
		"NcConnectionStatus": "number", // an enum
		"NcTouchpoint":       "object", // a struct
		"NcNotADatatype":     "",       // nothing to check against
	} {
		if got := jsonKindFor(name); got != want {
			t.Errorf("jsonKindFor(%q) = %q, want %q", name, got, want)
		}
	}

	// A typedef resolves to its parent's kind; a sequence typedef
	// carries no scalar kind at all.
	scalar := "NcVendorScalar"
	if err := ms05.RegisterDatatype(ms05.NcDatatypeDescriptor{
		Name: scalar, Type: ms05.NcDatatypeTypeTypedef, ParentType: namePtr("NcString"),
	}); err != nil {
		t.Fatal(err)
	}
	if got := jsonKindFor(scalar); got != "string" {
		t.Errorf("a typedef of NcString = %q, want string", got)
	}

	seq := "NcVendorSequence"
	if err := ms05.RegisterDatatype(ms05.NcDatatypeDescriptor{
		Name: seq, Type: ms05.NcDatatypeTypeTypedef, ParentType: namePtr("NcString"), IsSequence: true,
	}); err != nil {
		t.Fatal(err)
	}
	if got := jsonKindFor(seq); got != "" {
		t.Errorf("a sequence typedef = %q, want no scalar kind", got)
	}

	rootless := "NcVendorRootless"
	if err := ms05.RegisterDatatype(ms05.NcDatatypeDescriptor{
		Name: rootless, Type: ms05.NcDatatypeTypeTypedef,
	}); err != nil {
		t.Fatal(err)
	}
	if got := jsonKindFor(rootless); got != "" {
		t.Errorf("a typedef with no parent = %q, want no kind", got)
	}
}

// A method parameter with no declared type still resolves to a
// descriptor — a controller reading the method signature gets an
// answer rather than a hole.
func TestFlattenedDatatypeForAnUntypedParameter(t *testing.T) {
	d, ok := flattenedDatatype(nil)
	if !ok || d.Name != "NcAny" || d.Type != ms05.NcDatatypeTypePrimitive {
		t.Errorf("flattenedDatatype(nil) = %+v, %v", d, ok)
	}

	d, ok = flattenedDatatype(namePtr("NcTouchpoint"))
	if !ok || d.Name != "NcTouchpoint" {
		t.Errorf("flattenedDatatype(NcTouchpoint) = %+v, %v", d, ok)
	}
	if _, ok := flattenedDatatype(namePtr("NcNotADatatype")); ok {
		t.Error("flattenedDatatype answered for a name the framework does not carry")
	}
}
