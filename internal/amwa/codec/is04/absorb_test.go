package is04

import (
	"errors"
	"reflect"
	"testing"
)

func boolp(b bool) *bool { return &b }

type absorbNested struct {
	Deep string `json:"deep"`
}

// absorbProbe covers every way encoding/json names a field: an
// embedded interface (contributes nothing), a `-` tag (hidden), no
// tag (Go name), a named tag with options, an embedded struct and an
// embedded pointer (both promoted).
type absorbProbe struct {
	error
	Hidden   string `json:"-"`
	Untagged string
	Named    string `json:"named,omitempty"`
	ResourceCore
	*absorbNested
}

// TestJSONFieldNamesFollowsEncodingJSONRules: the unknown-field scan
// must accept exactly the keys encoding/json would, or a legitimate
// key gets reported as a deviation.
func TestJSONFieldNamesFollowsEncodingJSONRules(t *testing.T) {
	probe := absorbProbe{
		error:        errors.New("probe"),
		Hidden:       "h",
		Untagged:     "u",
		Named:        "n",
		absorbNested: &absorbNested{Deep: "d"},
	}
	names := jsonFieldNames(reflect.TypeOf(&probe))
	for _, want := range []string{"Untagged", "named", "id", "version", "label", "description", "tags", "deep"} {
		if !names[want] {
			t.Errorf("%q must be a known field", want)
		}
	}
	for _, absent := range []string{"Hidden", "-", "error", ""} {
		if names[absent] {
			t.Errorf("%q must not be a known field", absent)
		}
	}
}

func TestJSONFieldNamesOfNonStructIsEmpty(t *testing.T) {
	if n := jsonFieldNames(reflect.TypeOf(0)); len(n) != 0 {
		t.Errorf("an int has no JSON fields, got %v", n)
	}
	if n := jsonFieldNames(nil); len(n) != 0 {
		t.Errorf("a nil type has no JSON fields, got %v", n)
	}
}

// TestUnknownFieldsOnUnparseableFrameIsEmpty: the scan is a reporter,
// not a second parser. A frame it cannot read reports nothing and
// leaves the decode result alone.
func TestUnknownFieldsOnUnparseableFrameIsEmpty(t *testing.T) {
	if got := unknownFields([]byte(`{"id":`), &Node{}); got != nil {
		t.Fatalf("want no names from an unparseable frame, got %v", got)
	}
}

// TestBoolCapValuesIgnoresMalformedCapabilities: a capability that is
// not the BCP-004 shape (an object whose enum is an array) yields no
// values, so consistency checks treat it as absent rather than guess.
func TestBoolCapValuesIgnoresMalformedCapabilities(t *testing.T) {
	cases := map[string]map[string]any{
		"capability is not an object": {HKEPCapabilityURN: "yes", PrivacyCapabilityURN: "no"},
		"enum is not an array": {
			HKEPCapabilityURN:    map[string]any{"enum": true},
			PrivacyCapabilityURN: map[string]any{"enum": "true"},
		},
		"enum holds no booleans": {
			HKEPCapabilityURN:    map[string]any{"enum": []any{"true", 1}},
			PrivacyCapabilityURN: map[string]any{"enum": []any{"false"}},
		},
	}
	for name, caps := range cases {
		t.Run(name, func(t *testing.T) {
			if got := HKEPCapValues(caps); len(got) != 0 {
				t.Errorf("hkep values = %v, want none", got)
			}
			if got := PrivacyCapValues(caps); len(got) != 0 {
				t.Errorf("privacy values = %v, want none", got)
			}
		})
	}
}

// TestHKEPConsistencyNamesAnExplicitFalse: the error distinguishes a
// Sender that says hkep=false from one that says nothing at all.
func TestHKEPConsistencyNamesAnExplicitFalse(t *testing.T) {
	err := ValidateHKEPConsistency(boolp(false), []bool{true})
	if err == nil || err.Error() != "is04: hkep capability allows only true but the sender's hkep attribute is false" {
		t.Fatalf("got %v", err)
	}
	err = ValidateHKEPConsistency(nil, []bool{true})
	if err == nil || err.Error() != "is04: hkep capability allows only true but the sender's hkep attribute is absent" {
		t.Fatalf("got %v", err)
	}
}

func TestBoolPtrStr(t *testing.T) {
	cases := map[string]*bool{"absent": nil, "true": boolp(true), "false": boolp(false)}
	for want, b := range cases {
		if got := boolPtrStr(b); got != want {
			t.Errorf("boolPtrStr = %q, want %q", got, want)
		}
	}
}
