package is14_test

// Round-trip + validation tests for the IS-14 canonical codec.
// Expected shapes come from the AMWA v1.0.0 schemas + published
// examples (bulkProperties-put-request.json pins restoreMode 0 =
// Modify, the rebuild variant pins 1 = Rebuild), not from working
// code.

import (
	"encoding/json"
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is14"
	v10 "dhs/internal/amwa/codec/is14/v10"
	"dhs/internal/amwa/codec/ms05"
)

func sampleHolder() is14.BulkPropertiesHolder {
	fp := "dhs|v1.0"
	label := "root"
	return is14.BulkPropertiesHolder{
		ValidationFingerprint: &fp,
		Values: []is14.ObjectPropertiesHolder{{
			Path:                  []string{"root"},
			DependencyPaths:       [][]string{},
			AllowedMembersClasses: []ms05.NcClassId{},
			Values: []is14.PropertyHolder{{
				ID:    ms05.NcPropertyId{Level: 1, Index: 6},
				Value: label,
			}},
			IsRebuildable: false,
		}},
	}
}

func TestBulkPropertiesHolderRoundTrip(t *testing.T) {
	raw, err := is14.EncodeBulkPropertiesHolder(sampleHolder())
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	// The feature set's required members must exist on the wire even
	// when empty — a restore consumer checks for them.
	for _, key := range []string{"validationFingerprint", "dependencyPaths", "allowedMembersClasses", "isRebuildable"} {
		if !strings.Contains(string(raw), key) {
			t.Errorf("%s missing from encoded holder", key)
		}
	}
	back, err := is14.DecodeBulkPropertiesHolder(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(back.Values) != 1 || back.Values[0].Path[0] != "root" {
		t.Errorf("round trip lost values: %+v", back)
	}
	if back.ValidationFingerprint == nil || *back.ValidationFingerprint != "dhs|v1.0" {
		t.Errorf("fingerprint lost: %+v", back.ValidationFingerprint)
	}

	if _, err := is14.DecodeBulkPropertiesHolder([]byte(`{"validationFingerprint":null}`)); err == nil {
		t.Error("holder without values must be rejected")
	}
}

func TestBulkPropertiesSetRequestValidation(t *testing.T) {
	good := `{"arguments":{"dataSet":{"validationFingerprint":null,"values":[]},"recurse":true,"restoreMode":0}}`
	r, err := is14.DecodeBulkPropertiesSetRequest([]byte(good))
	if err != nil {
		t.Fatalf("valid Modify request rejected: %v", err)
	}
	if *r.Arguments.RestoreMode != is14.RestoreModeModify {
		t.Errorf("restoreMode = %d", *r.Arguments.RestoreMode)
	}

	rebuild := strings.Replace(good, `"restoreMode":0`, `"restoreMode":1`, 1)
	if _, err := is14.DecodeBulkPropertiesSetRequest([]byte(rebuild)); err != nil {
		t.Errorf("valid Rebuild request rejected: %v", err)
	}

	cases := map[string]string{
		"missing arguments":   `{}`,
		"missing dataSet":     `{"arguments":{"recurse":true,"restoreMode":0}}`,
		"missing values":      `{"arguments":{"dataSet":{"validationFingerprint":null},"recurse":true,"restoreMode":0}}`,
		"missing recurse":     `{"arguments":{"dataSet":{"validationFingerprint":null,"values":[]},"restoreMode":0}}`,
		"missing restoreMode": `{"arguments":{"dataSet":{"validationFingerprint":null,"values":[]},"recurse":true}}`,
		"bad restoreMode":     `{"arguments":{"dataSet":{"validationFingerprint":null,"values":[]},"recurse":true,"restoreMode":5}}`,
	}
	for name, body := range cases {
		if _, err := is14.DecodeBulkPropertiesSetRequest([]byte(body)); err == nil {
			t.Errorf("%s: must be rejected", name)
		}
	}

	// Unknown members are legal — the schema never sets
	// additionalProperties=false, and the AMWA suite sends extras.
	extra := `{"arguments":{"dataSet":{"validationFingerprint":null,"values":[]},"recurse":true,"restoreMode":0,"x":1},"json":{}}`
	if _, err := is14.DecodeBulkPropertiesSetRequest([]byte(extra)); err != nil {
		t.Errorf("unknown members must be tolerated: %v", err)
	}
}

func TestPropertyValuePutRequest(t *testing.T) {
	r, err := is14.DecodePropertyValuePutRequest([]byte(`{"value": 42}`))
	if err != nil {
		t.Fatalf("valid put rejected: %v", err)
	}
	var n int
	if err := json.Unmarshal(r.Value, &n); err != nil || n != 42 {
		t.Errorf("value lost: %s", r.Value)
	}

	// `value: null` is legal — nullable properties are set to null this
	// way. Only the ABSENCE of the member is an error.
	if _, err := is14.DecodePropertyValuePutRequest([]byte(`{"value": null}`)); err != nil {
		t.Errorf("null value must be accepted: %v", err)
	}
	if _, err := is14.DecodePropertyValuePutRequest([]byte(`{}`)); err == nil {
		t.Error("missing value member must be rejected")
	}
	if _, err := is14.DecodePropertyValuePutRequest([]byte(`{"value":1,"other":2}`)); err != nil {
		t.Errorf("unknown members must be tolerated (schema allows them): %v", err)
	}
}

func TestMethodPatchRequest(t *testing.T) {
	r, err := is14.DecodeMethodPatchRequest([]byte(`{"arguments":{"id":{"level":1,"index":6}}}`))
	if err != nil {
		t.Fatalf("valid invoke rejected: %v", err)
	}
	if !strings.Contains(string(r.Arguments), "level") {
		t.Errorf("arguments lost: %s", r.Arguments)
	}
	// Parameterless methods send an EMPTY object, which is legal.
	if _, err := is14.DecodeMethodPatchRequest([]byte(`{"arguments":{}}`)); err != nil {
		t.Errorf("empty arguments object must be accepted: %v", err)
	}
	if _, err := is14.DecodeMethodPatchRequest([]byte(`{}`)); err == nil {
		t.Error("missing arguments must be rejected")
	}
	if _, err := is14.DecodeMethodPatchRequest([]byte(`{"arguments":[1]}`)); err == nil {
		t.Error("non-object arguments must be rejected")
	}
}

func TestValidationShapesMatchExamples(t *testing.T) {
	msg := "Some properties have notices"
	v := is14.ResultObjectPropertiesSetValidation{
		Status: ms05.NcMethodStatusOk,
		Value: []is14.ObjectPropertiesSetValidation{{
			Path:   []string{"root"},
			Status: ms05.NcMethodStatusOk,
			Notices: []is14.PropertyRestoreNotice{{
				ID:            ms05.NcPropertyId{Level: 2, Index: 1},
				Name:          "enabled",
				NoticeType:    is14.NoticeWarning,
				NoticeMessage: "Property is readonly",
			}},
			StatusMessage: &msg,
		}},
	}
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Key names pinned by bulkProperties-put-200.json.
	for _, key := range []string{`"noticeType":300`, `"noticeMessage"`, `"statusMessage"`, `"notices"`} {
		if !strings.Contains(string(raw), key) {
			t.Errorf("%s missing from validation wire shape: %s", key, raw)
		}
	}
}

func TestRegistryWiring(t *testing.T) {
	c, ok := is14.Get("v1.0")
	if !ok {
		t.Fatal("v1.0 codec not registered")
	}
	if c.SpecID() != is14.SpecID || c.SpecPatch() != v10.SpecPatch {
		t.Errorf("identity: %s %s %s", c.SpecID(), c.APIVer(), c.SpecPatch())
	}
	if got := is14.SupportedVersions(); len(got) != 1 || got[0] != "v1.0" {
		t.Errorf("supported = %v", got)
	}
	if is14.Default().APIVer() != "v1.0" {
		t.Error("default must be v1.0")
	}
}
