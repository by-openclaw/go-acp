package bcp00402

import (
	"strings"
	"testing"

	"dhs/internal/amwa/codec/bcp"
	"dhs/internal/amwa/codec/spec"
)

func TestIdentity(t *testing.T) {
	v := New()
	if v.SpecID() != "bcp-004-02" || v.APIVer() != "v1.0" || v.SpecPatch() != "v1.0.0" {
		t.Fatalf("identity = %s/%s/%s", v.SpecID(), v.APIVer(), v.SpecPatch())
	}
	if v.HostKind() != bcp.KindSender {
		t.Fatalf("HostKind = %q, want sender", v.HostKind())
	}
	if got, ok := bcp.Get(SpecID, APIVer); !ok || got != any(v) {
		t.Fatal("validator not registered under its own identity")
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name     string
		payload  string
		wantCode string
		wantSev  spec.Severity
		wantN    int
	}{
		{"caps absent", `{}`, "", 0, 0},
		{"constraint_sets absent", `{"caps":{}}`, "", 0, 0},
		{"empty array", `{"caps":{"constraint_sets":[]}}`, "", 0, 0},
		{"populated set", `{"caps":{"constraint_sets":[{"urn:x-nmos:cap:format:media_type":{"enum":["video/raw"]}}]}}`, "", 0, 0},
		{"one empty among two", `{"caps":{"constraint_sets":[{"a":1},{}]}}`, "bcp_004_02_empty_constraint_set", spec.SeverityWarn, 1},
		{"not json", `{"caps":`, "bcp_004_02_decode_error", spec.SeverityError, 1},
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

func TestEmptySetDetailNamesIndex(t *testing.T) {
	events := New().Validate([]byte(`{"caps":{"constraint_sets":[{"a":1},{}]}}`))
	if len(events) != 1 || !strings.Contains(events[0].Detail, "constraint_sets[1]") {
		t.Fatalf("events = %+v", events)
	}
}

func TestItoa(t *testing.T) {
	for in, want := range map[int]string{0: "0", 3: "3", 12: "12", 305: "305"} {
		if got := itoa(in); got != want {
			t.Errorf("itoa(%d) = %q, want %q", in, got, want)
		}
	}
}
