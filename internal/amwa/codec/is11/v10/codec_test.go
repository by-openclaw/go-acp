package v10

// v1.0 is identity plus delegation: every method must hand the same
// bytes to the canonical is11 functions and hand back the same
// verdict, so each is exercised once on an accepted and once on a
// rejected body.

import (
	"testing"

	"dhs/internal/amwa/codec/is11"
)

func TestIdentity(t *testing.T) {
	c := New()
	if c.SpecID() != is11.SpecID || c.APIVer() != "v1.0" || c.SpecPatch() != SpecPatch {
		t.Fatalf("identity = %s/%s/%s", c.SpecID(), c.APIVer(), c.SpecPatch())
	}
	got, ok := is11.Get("v1.0")
	if !ok || got != any(c) {
		t.Fatal("v1.0 codec not registered under its own identity")
	}
}

func TestInputDelegates(t *testing.T) {
	c := New()
	in := is11.Input{ResourceCore: is11.ResourceCore{ID: "i", Tags: map[string][]string{}}, Status: is11.Status{State: is11.InputNoSignal}}
	raw, err := c.EncodeInput(in)
	if err != nil {
		t.Fatalf("EncodeInput: %v", err)
	}
	back, err := c.DecodeInput(raw)
	if err != nil || back.ID != "i" || back.Status.State != is11.InputNoSignal {
		t.Fatalf("DecodeInput = %+v, %v", back, err)
	}
	if _, err := c.EncodeInput(is11.Input{Status: is11.Status{State: "bogus"}}); err == nil {
		t.Error("EncodeInput must refuse an unknown state")
	}
	if _, err := c.DecodeInput([]byte(`{"bogus":1}`)); err == nil {
		t.Error("DecodeInput must refuse an unknown member")
	}
}

func TestOutputDelegates(t *testing.T) {
	c := New()
	out := is11.Output{ResourceCore: is11.ResourceCore{ID: "o", Tags: map[string][]string{}}, Status: is11.Status{State: is11.OutputSignalPresent}}
	raw, err := c.EncodeOutput(out)
	if err != nil {
		t.Fatalf("EncodeOutput: %v", err)
	}
	back, err := c.DecodeOutput(raw)
	if err != nil || back.ID != "o" || back.Status.State != is11.OutputSignalPresent {
		t.Fatalf("DecodeOutput = %+v, %v", back, err)
	}
	if _, err := c.EncodeOutput(is11.Output{Status: is11.Status{State: "bogus"}}); err == nil {
		t.Error("EncodeOutput must refuse an unknown state")
	}
	if _, err := c.DecodeOutput([]byte(`{"bogus":1}`)); err == nil {
		t.Error("DecodeOutput must refuse an unknown member")
	}
}

func TestActiveConstraintsDelegates(t *testing.T) {
	c := New()
	a := is11.ActiveConstraints{ConstraintSets: []is11.ConstraintSet{
		{"urn:x-nmos:cap:format:media_type": map[string]any{"enum": []any{"video/raw"}}},
	}}
	if err := c.ValidateActiveConstraints(a); err != nil {
		t.Fatalf("ValidateActiveConstraints: %v", err)
	}
	raw, err := c.EncodeActiveConstraints(a)
	if err != nil {
		t.Fatalf("EncodeActiveConstraints: %v", err)
	}
	back, err := c.DecodeActiveConstraints(raw)
	if err != nil || len(back.ConstraintSets) != 1 {
		t.Fatalf("DecodeActiveConstraints = %+v, %v", back, err)
	}
	bad := is11.ActiveConstraints{ConstraintSets: []is11.ConstraintSet{{"urn:x-vendor:x": 1}}}
	if err := c.ValidateActiveConstraints(bad); err == nil {
		t.Error("ValidateActiveConstraints must refuse a non-cap key")
	}
	if _, err := c.EncodeActiveConstraints(bad); err == nil {
		t.Error("EncodeActiveConstraints must refuse a non-cap key")
	}
	if _, err := c.DecodeActiveConstraints([]byte(`{}`)); err == nil {
		t.Error("DecodeActiveConstraints must refuse a missing constraint_sets")
	}
}
