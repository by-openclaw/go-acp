package bcp00401

// #851: the validator flags a constraint-set cap URN that is not in the
// AMWA capabilities register — the "this vendor invented a capability"
// case a filtering controller silently drops.

import (
	"strings"
	"testing"
)

func codesOf(v Validator, payload string) map[string]int {
	m := map[string]int{}
	for _, e := range v.Validate([]byte(payload)) {
		m[e.Code]++
	}
	return m
}

func TestUnknownCapURNFlagged(t *testing.T) {
	v := New()

	// A real register URN passes; an invented x-nmos cap URN is flagged;
	// a vendor URN outside urn:x-nmos: is permitted (BCP-004-01 §3).
	payload := `{"caps":{"constraint_sets":[
		{"urn:x-nmos:cap:format:color_sampling":{"enum":["YCbCr-4:2:2"]},
		 "urn:x-nmos:cap:format:teleportation":{"enum":["yes"]},
		 "urn:x-vendor:cap:secret":{"enum":["ok"]}}
	]}}`
	by := codesOf(v, payload)
	if by["bcp_004_01_unknown_cap_urn"] != 1 {
		t.Fatalf("expected exactly one unknown-cap-urn event, got %v", by)
	}
}

func TestKnownCapURNsClean(t *testing.T) {
	v := New()
	payload := `{"caps":{"constraint_sets":[
		{"urn:x-nmos:cap:format:media_type":{"enum":["video/raw"]},
		 "urn:x-nmos:cap:meta:preference":{"minimum":0}}
	]}}`
	for _, e := range v.Validate([]byte(payload)) {
		if strings.Contains(e.Code, "unknown_cap_urn") {
			t.Errorf("register URNs wrongly flagged: %s", e.Detail)
		}
	}
}

func TestEmptyConstraintSetStillFlagged(t *testing.T) {
	v := New()
	if codesOf(v, `{"caps":{"constraint_sets":[{}]}}`)["bcp_004_01_empty_constraint_set"] != 1 {
		t.Error("an empty constraint set must still be flagged")
	}
}
