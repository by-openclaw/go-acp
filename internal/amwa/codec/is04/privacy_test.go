package is04_test

// BCP-005-03 (IPMX/PEP) tests: the privacy Sender attribute round-trips,
// and the capability↔attribute consistency rules match the spec's
// "Consistency" section. The capability is a SINGULAR boolean, so a
// two-valued enum is itself an error. Helpers boolp/contains live in
// hkep_test.go (same package).

import (
	"encoding/json"
	"testing"

	"dhs/internal/amwa/codec/is04"
)

func TestSenderPrivacyRoundTrip(t *testing.T) {
	raw := []byte(`{
		"id":"cccccccc-3333-4333-8333-333333333333","version":"1:0",
		"label":"s","description":"d","tags":{},
		"flow_id":"dddddddd-4444-4444-8444-444444444444",
		"transport":"urn:x-nmos:transport:rtp.mcast",
		"device_id":"eeeeeeee-5555-4555-8555-555555555555",
		"manifest_href":"http://h/tf","interface_bindings":["eth0"],
		"subscription":{"receiver_id":null,"active":true},
		"privacy":true,
		"caps":{"constraint_sets":[{"urn:x-nmos:cap:transport:privacy":{"enum":[true]}}]}
	}`)
	s, err := is04.DecodeSender(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if s.Privacy == nil || !*s.Privacy {
		t.Fatalf("privacy attribute lost: %v", s.Privacy)
	}
	out, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !contains(out, `"privacy":true`) {
		t.Errorf("re-encoded sender dropped privacy: %s", out)
	}

	// A sender without privacy keeps the attribute ABSENT (not false).
	noPriv := []byte(`{"id":"cccccccc-3333-4333-8333-333333333333","version":"1:0","label":"s","description":"d","tags":{},"flow_id":null,"transport":"urn:x-nmos:transport:rtp","device_id":"eeeeeeee-5555-4555-8555-555555555555","manifest_href":null,"interface_bindings":[],"subscription":{"receiver_id":null,"active":false}}`)
	s2, err := is04.DecodeSender(noPriv)
	if err != nil {
		t.Fatalf("decode no-privacy: %v", err)
	}
	if s2.Privacy != nil {
		t.Errorf("absent privacy must stay nil, got %v", *s2.Privacy)
	}
	out2, _ := json.Marshal(s2)
	if contains(out2, `"privacy"`) {
		t.Errorf("absent privacy must not serialise: %s", out2)
	}
}

func TestPrivacyCapValues(t *testing.T) {
	caps := map[string]any{
		"urn:x-nmos:cap:transport:privacy": map[string]any{"enum": []any{true}},
	}
	if v := is04.PrivacyCapValues(caps); len(v) != 1 || !v[0] {
		t.Errorf("cap values = %v, want [true]", v)
	}
	if v := is04.PrivacyCapValues(map[string]any{}); v != nil {
		t.Errorf("absent cap = %v, want nil", v)
	}
}

func TestPrivacyConsistency(t *testing.T) {
	cases := []struct {
		name    string
		privacy *bool
		cap     []bool
		wantErr bool
	}{
		{"no cap, no attr", nil, nil, false},
		{"no cap, privacy true", boolp(true), nil, false},
		{"cap true-only + privacy true", boolp(true), []bool{true}, false},
		{"cap true-only + privacy false", boolp(false), []bool{true}, true},
		{"cap true-only + privacy absent", nil, []bool{true}, true},
		{"cap false-only + privacy false", boolp(false), []bool{false}, false},
		{"cap false-only + privacy true", boolp(true), []bool{false}, true},
		{"cap false-only + privacy absent", nil, []bool{false}, false},
		// The capability is SINGULAR: a two-valued enum is itself invalid.
		{"cap two-valued (illegal) + privacy true", boolp(true), []bool{true, false}, true},
		{"cap two-valued (illegal) + privacy absent", nil, []bool{true, false}, true},
	}
	for _, tc := range cases {
		err := is04.ValidatePrivacyConsistency(tc.privacy, tc.cap)
		if (err != nil) != tc.wantErr {
			t.Errorf("%s: err=%v, wantErr=%v", tc.name, err, tc.wantErr)
		}
	}
}
