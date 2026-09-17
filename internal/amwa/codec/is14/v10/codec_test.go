package v10

// v1.0 is identity plus delegation: every method must hand the same
// bytes to the canonical is14 functions and hand back the same
// verdict, so each is exercised once on an accepted and once on a
// rejected body.

import (
	"testing"

	"dhs/internal/amwa/codec/is14"
)

func TestIdentity(t *testing.T) {
	c := New()
	if c.SpecID() != is14.SpecID || c.APIVer() != "v1.0" || c.SpecPatch() != SpecPatch {
		t.Fatalf("identity = %s/%s/%s", c.SpecID(), c.APIVer(), c.SpecPatch())
	}
	got, ok := is14.Get("v1.0")
	if !ok || got != any(c) {
		t.Fatal("v1.0 codec not registered under its own identity")
	}
}

func TestBulkPropertiesHolderDelegates(t *testing.T) {
	c := New()
	fp := "abc"
	h := is14.BulkPropertiesHolder{ValidationFingerprint: &fp, Values: []is14.ObjectPropertiesHolder{}}
	raw, err := c.EncodeBulkPropertiesHolder(h)
	if err != nil {
		t.Fatalf("EncodeBulkPropertiesHolder: %v", err)
	}
	back, err := c.DecodeBulkPropertiesHolder(raw)
	if err != nil || back.ValidationFingerprint == nil || *back.ValidationFingerprint != "abc" || back.Values == nil {
		t.Fatalf("DecodeBulkPropertiesHolder = %+v, %v", back, err)
	}
	if _, err := c.DecodeBulkPropertiesHolder([]byte(`{"validationFingerprint":null}`)); err == nil {
		t.Error("DecodeBulkPropertiesHolder must refuse a holder without values")
	}
}

func TestRequestDecodersDelegate(t *testing.T) {
	c := New()

	set, err := c.DecodeBulkPropertiesSetRequest([]byte(`{"arguments":{"dataSet":{"validationFingerprint":null,"values":[]},"recurse":true,"restoreMode":1}}`))
	if err != nil || set.Arguments == nil || *set.Arguments.RestoreMode != is14.RestoreModeRebuild {
		t.Fatalf("DecodeBulkPropertiesSetRequest = %+v, %v", set, err)
	}
	if _, err := c.DecodeBulkPropertiesSetRequest([]byte(`{}`)); err == nil {
		t.Error("DecodeBulkPropertiesSetRequest must refuse a body without arguments")
	}

	put, err := c.DecodePropertyValuePutRequest([]byte(`{"value":null}`))
	if err != nil || string(put.Value) != "null" {
		t.Fatalf("DecodePropertyValuePutRequest = %+v, %v", put, err)
	}
	if _, err := c.DecodePropertyValuePutRequest([]byte(`{}`)); err == nil {
		t.Error("DecodePropertyValuePutRequest must refuse a body without value")
	}

	patch, err := c.DecodeMethodPatchRequest([]byte(`{"arguments":{}}`))
	if err != nil || string(patch.Arguments) != "{}" {
		t.Fatalf("DecodeMethodPatchRequest = %+v, %v", patch, err)
	}
	if _, err := c.DecodeMethodPatchRequest([]byte(`{"arguments":[]}`)); err == nil {
		t.Error("DecodeMethodPatchRequest must refuse non-object arguments")
	}
}
