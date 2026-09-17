package bcp00201

import (
	"strings"
	"testing"

	"dhs/internal/amwa/codec/bcp"
	"dhs/internal/amwa/codec/spec"
)

func TestIdentity(t *testing.T) {
	v := New()
	if v.SpecID() != "bcp-002-01" || v.APIVer() != "v1.0" || v.SpecPatch() != "v1.0.0" {
		t.Fatalf("identity = %s/%s/%s", v.SpecID(), v.APIVer(), v.SpecPatch())
	}
	if v.HostKind() != bcp.KindSender {
		t.Fatalf("HostKind = %q, want sender", v.HostKind())
	}
	if got, ok := bcp.Get(SpecID, APIVer); !ok || got != any(v) {
		t.Fatal("validator not registered under its own identity")
	}
}

// Grouphint grammar per the NMOS Parameter Registers:
// [<scope>:]<group>:<role>[:<aux>], scope in {node, device}.
func TestValidate(t *testing.T) {
	cases := []struct {
		name     string
		payload  string
		wantCode string
		wantSev  spec.Severity
		wantN    int
	}{
		{"tag absent is opt-out, not a deviation", `{"tags":{"other":["x"]}}`, "", 0, 0},
		{"no tags at all", `{}`, "", 0, 0},
		{"group:role", `{"tags":{"urn:x-nmos:tag:grouping/v1.0":["cam1:video"]}}`, "", 0, 0},
		{"node scope with aux", `{"tags":{"urn:x-nmos:tag:grouping/v1.0":["node:cam1:audio:left"]}}`, "", 0, 0},
		{"device scope", `{"tags":{"urn:x-nmos:tag:grouping/v1.0":["device:cam1:audio"]}}`, "", 0, 0},
		{"unknown scope with aux is four bare components", `{"tags":{"urn:x-nmos:tag:grouping/v1.0":["rack:cam1:audio:left"]}}`, "bcp_002_01_grouphint_malformed", spec.SeverityWarn, 1},
		{"single component", `{"tags":{"urn:x-nmos:tag:grouping/v1.0":["cam1"]}}`, "bcp_002_01_grouphint_malformed", spec.SeverityWarn, 1},
		{"empty role", `{"tags":{"urn:x-nmos:tag:grouping/v1.0":["cam1:"]}}`, "bcp_002_01_grouphint_malformed", spec.SeverityWarn, 1},
		{"too many components", `{"tags":{"urn:x-nmos:tag:grouping/v1.0":["node:a:b:c:d"]}}`, "bcp_002_01_grouphint_malformed", spec.SeverityWarn, 1},
		{"every bad entry reported", `{"tags":{"urn:x-nmos:tag:grouping/v1.0":["ok:ok","bad","alsobad"]}}`, "bcp_002_01_grouphint_malformed", spec.SeverityWarn, 2},
		{"not json", `{`, "bcp_002_01_decode_error", spec.SeverityError, 1},
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

// The detail names the offending index so an operator can find the
// entry in a tag array of dozens without a debugger.
func TestMalformedDetailNamesIndex(t *testing.T) {
	payload := `{"tags":{"urn:x-nmos:tag:grouping/v1.0":["a:b","a:b","a:b","a:b","a:b","a:b","a:b","a:b","a:b","a:b","a:b","broken"]}}`
	events := New().Validate([]byte(payload))
	if len(events) != 1 || !strings.Contains(events[0].Detail, "["+TagURN+"][11]=broken") {
		t.Fatalf("events = %+v", events)
	}
}

func TestItoa(t *testing.T) {
	for in, want := range map[int]string{0: "0", 7: "7", 10: "10", 123: "123"} {
		if got := itoa(in); got != want {
			t.Errorf("itoa(%d) = %q, want %q", in, got, want)
		}
	}
}
