package bcp00601

import (
	"strings"
	"testing"

	"dhs/internal/amwa/codec/bcp"
	"dhs/internal/amwa/codec/spec"
)

const (
	rightFormat = "urn:x-nmos:format:video"
	wrongFormat = "urn:x-nmos:format:audio"
)

func TestIdentity(t *testing.T) {
	v := New()
	if v.SpecID() != "bcp-006-01" || v.APIVer() != "v1.0" || v.SpecPatch() != "v1.0.0" {
		t.Fatalf("identity = %s/%s/%s", v.SpecID(), v.APIVer(), v.SpecPatch())
	}
	if v.HostKind() != bcp.KindFlow {
		t.Fatalf("HostKind = %q, want flow", v.HostKind())
	}
	if got, ok := bcp.Get(SpecID, APIVer); !ok || got != any(v) {
		t.Fatal("validator not registered under its own identity")
	}
}

// The media type pins the format URN; a Flow of any other media type
// is outside this BCP and never flagged.
func TestValidate(t *testing.T) {
	cases := []struct {
		name     string
		payload  string
		wantCode string
		wantSev  spec.Severity
	}{
		{"canonical pair", `{"format":"` + rightFormat + `","media_type":"` + JPEGXSMediaType + `"}`, "", 0},
		{"other media type is out of scope", `{"format":"` + wrongFormat + `","media_type":"video/raw"}`, "", 0},
		{"no fields at all", `{}`, "", 0},
		{"wrong format", `{"format":"` + wrongFormat + `","media_type":"` + JPEGXSMediaType + `"}`, "bcp_006_01_format_mismatch", spec.SeverityWarn},
		{"missing format", `{"media_type":"` + JPEGXSMediaType + `"}`, "bcp_006_01_format_mismatch", spec.SeverityWarn},
		{"not json", `{`, "bcp_006_01_decode_error", spec.SeverityError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			events := New().Validate([]byte(tc.payload))
			if tc.wantCode == "" {
				if len(events) != 0 {
					t.Fatalf("unexpected events %+v", events)
				}
				return
			}
			if len(events) != 1 {
				t.Fatalf("got %d events %+v, want 1", len(events), events)
			}
			e := events[0]
			if e.Code != tc.wantCode || e.Severity != tc.wantSev {
				t.Fatalf("event = %s/%v, want %s/%v", e.Code, e.Severity, tc.wantCode, tc.wantSev)
			}
			if e.SpecID != SpecID || e.APIVer != APIVer || e.SpecPatch != SpecPatch || e.At.IsZero() {
				t.Fatalf("event identity incomplete: %+v", e)
			}
		})
	}
}

func TestMismatchDetailNamesBothSides(t *testing.T) {
	events := New().Validate([]byte(`{"format":"` + wrongFormat + `","media_type":"` + JPEGXSMediaType + `"}`))
	if len(events) != 1 || !strings.Contains(events[0].Detail, JPEGXSMediaType) || !strings.Contains(events[0].Detail, wrongFormat) {
		t.Fatalf("events = %+v", events)
	}
}
