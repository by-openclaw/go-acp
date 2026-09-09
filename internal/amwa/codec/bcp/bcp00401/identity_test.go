package bcp00401

import (
	"testing"

	"dhs/internal/amwa/codec/bcp"
	"dhs/internal/amwa/codec/spec"
)

func TestIdentity(t *testing.T) {
	v := New()
	if v.SpecID() != "bcp-004-01" || v.APIVer() != "v1.0" || v.SpecPatch() != "v1.0.0" {
		t.Fatalf("identity = %s/%s/%s", v.SpecID(), v.APIVer(), v.SpecPatch())
	}
	if v.HostKind() != bcp.KindReceiver {
		t.Fatalf("HostKind = %q, want receiver", v.HostKind())
	}
	if got, ok := bcp.Get(SpecID, APIVer); !ok || got != any(v) {
		t.Fatal("validator not registered under its own identity")
	}
}

func TestValidateShapes(t *testing.T) {
	cases := []struct {
		name     string
		payload  string
		wantCode string
		wantSev  spec.Severity
		wantN    int
	}{
		{"caps absent is not a deviation", `{"id":"x"}`, "", 0, 0},
		{"constraint_sets absent", `{"caps":{}}`, "", 0, 0},
		{"empty array is a valid no-constraint receiver", `{"caps":{"constraint_sets":[]}}`, "", 0, 0},
		{"meta keys only are neither cap URNs nor empty", `{"caps":{"constraint_sets":[{"urn:x-nmos:cap:meta:label":"a"}]}}`, "", 0, 0},
		{"empty key is skipped", `{"caps":{"constraint_sets":[{"":1}]}}`, "", 0, 0},
		{"two empty sets, two events", `{"caps":{"constraint_sets":[{},{}]}}`, "bcp_004_01_empty_constraint_set", spec.SeverityWarn, 2},
		{"not json", `nope`, "bcp_004_01_decode_error", spec.SeverityError, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			events := New().Validate([]byte(tc.payload))
			if len(events) != tc.wantN {
				t.Fatalf("got %d events %+v, want %d", len(events), events, tc.wantN)
			}
			for _, e := range events {
				if e.Code != tc.wantCode || e.Severity != tc.wantSev {
					t.Fatalf("event = %s/%v, want %s/%v", e.Code, e.Severity, tc.wantCode, tc.wantSev)
				}
				if e.SpecID != SpecID || e.APIVer != APIVer || e.SpecPatch != SpecPatch || e.At.IsZero() {
					t.Fatalf("event identity incomplete: %+v", e)
				}
			}
		})
	}
}
