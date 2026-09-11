package bcp00801

import (
	"testing"

	"dhs/internal/amwa/codec/bcp"
	"dhs/internal/amwa/codec/ms05"
	"dhs/internal/amwa/codec/spec"
)

func TestIdentity(t *testing.T) {
	v := New()
	if v.SpecID() != "bcp-008-01" || v.APIVer() != "v1.0" || v.SpecPatch() != "v1.0.0" {
		t.Fatalf("identity = %s/%s/%s", v.SpecID(), v.APIVer(), v.SpecPatch())
	}
	if v.HostKind() != bcp.KindMS05Class {
		t.Fatalf("HostKind = %q, want ms05.class", v.HostKind())
	}
	if got, ok := bcp.Get(SpecID, APIVer); !ok || got != any(v) {
		t.Fatal("validator not registered under its own identity")
	}
}

// Only a descriptor that claims the NcReceiverMonitor name is checked;
// its classId must be the lineage fixed by the spec, element for
// element.
func TestValidate(t *testing.T) {
	cases := []struct {
		name     string
		payload  string
		wantCode string
		wantSev  spec.Severity
	}{
		{"canonical", `{"classId":[1,2,2,1],"name":"NcReceiverMonitor"}`, "", 0},
		{"other class is out of scope", `{"classId":[1,2,2,9],"name":"NcSomethingElse"}`, "", 0},
		{"wrong last element", `{"classId":[1,2,2,9],"name":"NcReceiverMonitor"}`, "bcp_008_01_class_id_mismatch", spec.SeverityError},
		{"truncated lineage", `{"classId":[1,2,2],"name":"NcReceiverMonitor"}`, "bcp_008_01_class_id_mismatch", spec.SeverityError},
		{"classId missing", `{"name":"NcReceiverMonitor"}`, "bcp_008_01_class_id_mismatch", spec.SeverityError},
		{"not json", `{`, "bcp_008_01_decode_error", spec.SeverityError},
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

func TestClassIDMatches(t *testing.T) {
	want := NcReceiverMonitorClassID
	cases := []struct {
		name string
		got  ms05.NcClassId
		want bool
	}{
		{"exact", want, true},
		{"shorter", ms05.NcClassId{1, 2, 2}, false},
		{"longer", append(append(ms05.NcClassId{}, want...), 1), false},
		{"same length different element", ms05.NcClassId{1, 2, 3, want[3]}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classIDMatches(tc.got, want); got != tc.want {
				t.Fatalf("classIDMatches(%v) = %v, want %v", tc.got, got, tc.want)
			}
		})
	}
}
