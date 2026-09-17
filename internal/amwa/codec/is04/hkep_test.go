package is04_test

// BCP-005-02 (IPMX/HKEP) tests: the hkep Sender attribute round-trips,
// and the capability↔attribute consistency rules match the spec's
// "Consistency" section. Oracle = the spec doc + its sender.json
// example (hkep:true alongside urn:x-nmos:cap:transport:hkep enum
// [true]).

import (
	"encoding/json"
	"testing"

	"dhs/internal/amwa/codec/is04"
)

func boolp(b bool) *bool { return &b }

func TestSenderHKEPRoundTrip(t *testing.T) {
	// The spec sender.json example: hkep true.
	raw := []byte(`{
		"id":"cccccccc-3333-4333-8333-333333333333","version":"1:0",
		"label":"s","description":"d","tags":{},
		"flow_id":"dddddddd-4444-4444-8444-444444444444",
		"transport":"urn:x-nmos:transport:rtp.mcast",
		"device_id":"eeeeeeee-5555-4555-8555-555555555555",
		"manifest_href":"http://h/tf","interface_bindings":["eth0"],
		"subscription":{"receiver_id":null,"active":true},
		"hkep":true,
		"caps":{"constraint_sets":[{"urn:x-nmos:cap:transport:hkep":{"enum":[true]}}]}
	}`)
	s, err := is04.DecodeSender(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if s.Hkep == nil || !*s.Hkep {
		t.Fatalf("hkep attribute lost: %v", s.Hkep)
	}
	// Re-encode keeps hkep.
	out, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !contains(out, `"hkep":true`) {
		t.Errorf("re-encoded sender dropped hkep: %s", out)
	}

	// A sender without hkep keeps the attribute ABSENT (not false).
	noHkep := []byte(`{"id":"cccccccc-3333-4333-8333-333333333333","version":"1:0","label":"s","description":"d","tags":{},"flow_id":null,"transport":"urn:x-nmos:transport:rtp","device_id":"eeeeeeee-5555-4555-8555-555555555555","manifest_href":null,"interface_bindings":[],"subscription":{"receiver_id":null,"active":false}}`)
	s2, err := is04.DecodeSender(noHkep)
	if err != nil {
		t.Fatalf("decode no-hkep: %v", err)
	}
	if s2.Hkep != nil {
		t.Errorf("absent hkep must stay nil, got %v", *s2.Hkep)
	}
	out2, _ := json.Marshal(s2)
	if contains(out2, `"hkep"`) {
		t.Errorf("absent hkep must not serialise: %s", out2)
	}
}

func TestHKEPCapValues(t *testing.T) {
	caps := map[string]any{
		"urn:x-nmos:cap:transport:hkep": map[string]any{"enum": []any{true}},
	}
	if v := is04.HKEPCapValues(caps); len(v) != 1 || !v[0] {
		t.Errorf("cap values = %v, want [true]", v)
	}
	if v := is04.HKEPCapValues(map[string]any{}); v != nil {
		t.Errorf("absent cap = %v, want nil", v)
	}
}

func TestHKEPConsistency(t *testing.T) {
	cases := []struct {
		name    string
		hkep    *bool
		cap     []bool
		wantErr bool
	}{
		{"no cap, no attr", nil, nil, false},
		{"no cap, hkep true", boolp(true), nil, false},
		{"cap true-only + hkep true", boolp(true), []bool{true}, false},
		{"cap true-only + hkep false", boolp(false), []bool{true}, true},
		{"cap true-only + hkep absent", nil, []bool{true}, true},
		{"cap false-only + hkep false", boolp(false), []bool{false}, false},
		{"cap false-only + hkep true", boolp(true), []bool{false}, true},
		{"cap false-only + hkep absent", nil, []bool{false}, false},
		{"cap both + hkep true", boolp(true), []bool{true, false}, false},
		{"cap both + hkep false", boolp(false), []bool{true, false}, false},
		{"cap both + hkep absent", nil, []bool{true, false}, false},
	}
	for _, tc := range cases {
		err := is04.ValidateHKEPConsistency(tc.hkep, tc.cap)
		if (err != nil) != tc.wantErr {
			t.Errorf("%s: err=%v, wantErr=%v", tc.name, err, tc.wantErr)
		}
	}
}

func contains(b []byte, s string) bool {
	return len(b) >= len(s) && indexOf(string(b), s) >= 0
}

func indexOf(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}
