package is11_test

// Round-trip + validation tests for the IS-11 canonical codec.
// Expected shapes come from the AMWA v1.0.0 schemas in
// schemas/v1.0.0/ (input.json, output.json, constraint_set.json,
// constraints_active.json, sender-status.json, receiver-status.json),
// not from working code.

import (
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is11"
	v10 "dhs/internal/amwa/codec/is11/v10"
)

func core(id string) is11.ResourceCore {
	return is11.ResourceCore{
		ID: id, Version: "100:0", Label: "l", Description: "d",
		Tags: map[string][]string{},
	}
}

func TestInputRoundTrip(t *testing.T) {
	adjust := true
	in := is11.Input{
		ResourceCore:    core("f47ac10b-58cc-4372-a567-0e02b2c3d479"),
		AdjustToCaps:    &adjust,
		BaseEDIDSupport: true,
		Connected:       true,
		EDIDSupport:     true,
		Status:          is11.Status{State: is11.InputSignalPresent},
		DeviceID:        "f47ac10b-58cc-4372-a567-0e02b2c3d001",
	}
	raw, err := is11.EncodeInput(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	// The schema's optional-by-existence property must be present when
	// set and absent when nil.
	if !strings.Contains(string(raw), "adjust_to_caps") {
		t.Error("adjust_to_caps missing from encoded Input")
	}
	back, err := is11.DecodeInput(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if back.Status.State != is11.InputSignalPresent || !back.BaseEDIDSupport {
		t.Errorf("round trip lost fields: %+v", back)
	}

	in.AdjustToCaps = nil
	raw, err = is11.EncodeInput(in)
	if err != nil {
		t.Fatalf("encode without adjust: %v", err)
	}
	if strings.Contains(string(raw), "adjust_to_caps") {
		t.Error("nil adjust_to_caps must be ABSENT, not false — the property's existence is the feature flag")
	}
}

func TestOutputRoundTrip(t *testing.T) {
	out := is11.Output{
		ResourceCore: core("f47ac10b-58cc-4372-a567-0e02b2c3d480"),
		Connected:    true,
		EDIDSupport:  false,
		Status:       is11.Status{State: is11.OutputDefaultSignal, Debug: "pattern generator"},
		DeviceID:     "f47ac10b-58cc-4372-a567-0e02b2c3d001",
	}
	raw, err := is11.EncodeOutput(out)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	back, err := is11.DecodeOutput(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if back.Status.Debug != "pattern generator" {
		t.Errorf("debug lost: %+v", back.Status)
	}
}

func TestStatusEnumsAreKindScoped(t *testing.T) {
	// Every kind accepts its own members and rejects a neighbour's.
	cases := []struct {
		kind string
		ok   string
		bad  string
	}{
		{"input", is11.InputAwaitingSignal, is11.OutputDefaultSignal},
		{"output", is11.OutputDefaultSignal, is11.SenderConstrained},
		{"sender", is11.SenderConstraintsViolation, is11.ReceiverCompliantStream},
		{"receiver", is11.ReceiverNonCompliantStream, is11.SenderNoEssence},
	}
	for _, tc := range cases {
		if err := is11.ValidateStatus(tc.kind, is11.Status{State: tc.ok}); err != nil {
			t.Errorf("%s/%s rejected: %v", tc.kind, tc.ok, err)
		}
		if err := is11.ValidateStatus(tc.kind, is11.Status{State: tc.bad}); err == nil {
			t.Errorf("%s must reject %q", tc.kind, tc.bad)
		}
	}
	if err := is11.ValidateStatus("nope", is11.Status{State: "x"}); err == nil {
		t.Error("unknown kind must be rejected")
	}
}

func TestActiveConstraintsValidation(t *testing.T) {
	good := is11.ActiveConstraints{ConstraintSets: []is11.ConstraintSet{{
		"urn:x-nmos:cap:meta:label":         "1080p50 only",
		"urn:x-nmos:cap:meta:preference":    100,
		"urn:x-nmos:cap:format:frame_width": map[string]any{"enum": []any{1920}},
	}}}
	if err := is11.ValidateActiveConstraints(good); err != nil {
		t.Fatalf("valid constraints rejected: %v", err)
	}

	// Empty constraint_sets = unconstrained, and legal.
	if err := is11.ValidateActiveConstraints(is11.ActiveConstraints{ConstraintSets: []is11.ConstraintSet{}}); err != nil {
		t.Errorf("empty constraint_sets must be legal (unconstrained): %v", err)
	}

	// nil constraint_sets is NOT — the schema requires the member.
	if err := is11.ValidateActiveConstraints(is11.ActiveConstraints{}); err == nil {
		t.Error("nil constraint_sets must be rejected")
	}

	// An empty set violates minProperties:1.
	if err := is11.ValidateActiveConstraints(is11.ActiveConstraints{ConstraintSets: []is11.ConstraintSet{{}}}); err == nil {
		t.Error("empty constraint set must be rejected (minProperties 1)")
	}

	// Non-capability keys are refused.
	bad := is11.ActiveConstraints{ConstraintSets: []is11.ConstraintSet{{"width": 1920}}}
	if err := is11.ValidateActiveConstraints(bad); err == nil {
		t.Error("non-URN constraint key must be rejected")
	}
}

func TestActiveConstraintsRoundTrip(t *testing.T) {
	a := is11.ActiveConstraints{ConstraintSets: []is11.ConstraintSet{{
		"urn:x-nmos:cap:format:grain_rate": map[string]any{
			"enum": []any{map[string]any{"numerator": float64(50), "denominator": float64(1)}},
		},
	}}}
	raw, err := is11.EncodeActiveConstraints(a)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	back, err := is11.DecodeActiveConstraints(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(back.ConstraintSets) != 1 {
		t.Fatalf("sets = %d", len(back.ConstraintSets))
	}
	if _, ok := back.ConstraintSets[0]["urn:x-nmos:cap:format:grain_rate"]; !ok {
		t.Error("capability key lost in round trip")
	}
}

func TestRegistryWiring(t *testing.T) {
	c, ok := is11.Get("v1.0")
	if !ok {
		t.Fatal("v1.0 codec not registered")
	}
	if c.SpecID() != is11.SpecID || c.SpecPatch() != v10.SpecPatch {
		t.Errorf("identity: %s %s %s", c.SpecID(), c.APIVer(), c.SpecPatch())
	}
	if got := is11.SupportedVersions(); len(got) != 1 || got[0] != "v1.0" {
		t.Errorf("supported = %v", got)
	}
	if is11.Default().APIVer() != "v1.0" {
		t.Error("default must be v1.0")
	}
}
