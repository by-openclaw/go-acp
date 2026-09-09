package bcp00202

import (
	"strings"
	"testing"

	"dhs/internal/amwa/codec/bcp"
	"dhs/internal/amwa/codec/spec"
)

func TestIdentity(t *testing.T) {
	v := New()
	if v.SpecID() != "bcp-002-02" || v.APIVer() != "v1.0" || v.SpecPatch() != "v1.0.0" {
		t.Fatalf("identity = %s/%s/%s", v.SpecID(), v.APIVer(), v.SpecPatch())
	}
	if v.HostKind() != bcp.KindSender {
		t.Fatalf("HostKind = %q, want sender", v.HostKind())
	}
	if got, ok := bcp.Get(SpecID, APIVer); !ok || got != any(v) {
		t.Fatal("validator not registered under its own identity")
	}
}

// Asset tag values are "<key>=<value>" with the BCP-002-02 key
// vocabulary; anything else is a malformed entry, one event each.
func TestValidate(t *testing.T) {
	cases := []struct {
		name     string
		payload  string
		wantCode string
		wantSev  spec.Severity
		wantN    int
	}{
		{"tag absent", `{"tags":{}}`, "", 0, 0},
		{"all five keys", `{"tags":{"urn:x-nmos:tag:asset/v1.0":["manufacturer=BY","product=dhs","instance-id=1","function=router","name=core"]}}`, "", 0, 0},
		{"value containing equals", `{"tags":{"urn:x-nmos:tag:asset/v1.0":["name=a=b"]}}`, "", 0, 0},
		{"unknown key", `{"tags":{"urn:x-nmos:tag:asset/v1.0":["serial=123"]}}`, "bcp_002_02_asset_malformed", spec.SeverityWarn, 1},
		{"empty value", `{"tags":{"urn:x-nmos:tag:asset/v1.0":["product="]}}`, "bcp_002_02_asset_malformed", spec.SeverityWarn, 1},
		{"bare string", `{"tags":{"urn:x-nmos:tag:asset/v1.0":["dhs"]}}`, "bcp_002_02_asset_malformed", spec.SeverityWarn, 1},
		{"each bad entry counted", `{"tags":{"urn:x-nmos:tag:asset/v1.0":["x","product=ok","y"]}}`, "bcp_002_02_asset_malformed", spec.SeverityWarn, 2},
		{"not json", `[`, "bcp_002_02_decode_error", spec.SeverityError, 1},
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

func TestMalformedDetailNamesIndex(t *testing.T) {
	events := New().Validate([]byte(`{"tags":{"urn:x-nmos:tag:asset/v1.0":["product=ok","bad"]}}`))
	if len(events) != 1 || !strings.Contains(events[0].Detail, "["+TagURN+"][1]=bad") {
		t.Fatalf("events = %+v", events)
	}
}

func TestItoa(t *testing.T) {
	for in, want := range map[int]string{0: "0", 9: "9", 42: "42", 1000: "1000"} {
		if got := itoa(in); got != want {
			t.Errorf("itoa(%d) = %q, want %q", in, got, want)
		}
	}
}
