package is14_test

// The decoder arms the round-trip tests do not reach: malformed JSON
// on every request body, the strict holder decoder's unknown-member
// and trailing-JSON refusals, and the registry helpers.

import (
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is14"
)

func TestDecodersRejectMalformedJSON(t *testing.T) {
	cases := []struct {
		name    string
		decode  func([]byte) error
		raw     string
		wantErr string
	}{
		{"holder not JSON", func(b []byte) error { _, err := is14.DecodeBulkPropertiesHolder(b); return err }, `{`, "decode bulk properties holder"},
		{"holder unknown member", func(b []byte) error { _, err := is14.DecodeBulkPropertiesHolder(b); return err }, `{"validationFingerprint":null,"values":[],"bogus":1}`, "decode bulk properties holder"},
		{"holder trailing JSON", func(b []byte) error { _, err := is14.DecodeBulkPropertiesHolder(b); return err }, `{"validationFingerprint":null,"values":[]} {}`, "trailing JSON"},
		{"set request not JSON", func(b []byte) error { _, err := is14.DecodeBulkPropertiesSetRequest(b); return err }, `{"arguments":`, "decode bulkProperties set request"},
		{"put request not JSON", func(b []byte) error { _, err := is14.DecodePropertyValuePutRequest(b); return err }, `[`, "decode property value put request"},
		{"patch request not JSON", func(b []byte) error { _, err := is14.DecodeMethodPatchRequest(b); return err }, `{"arguments":{`, "decode method patch request"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.decode([]byte(tc.raw))
			if err == nil {
				t.Fatalf("decoder accepted %s", tc.raw)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %q, want it to mention %q", err, tc.wantErr)
			}
		})
	}
}

func TestVersionSelection(t *testing.T) {
	if got := is14.AllCodecs(); len(got) != 1 || got[0].APIVer() != "v1.0" {
		t.Fatalf("AllCodecs = %v, want the single v1.0 codec", got)
	}
	c, err := is14.SelectHighest([]string{"v0.9", "v1.0"})
	if err != nil || c.APIVer() != "v1.0" {
		t.Fatalf("SelectHighest = %v, %v", c, err)
	}
	if _, err := is14.SelectHighest([]string{"v2.0"}); err == nil {
		t.Error("a peer with no common version must be an error, never a downgrade")
	}
}

// Registering a codec that claims another spec is an init-time bug;
// the panic is the only place to catch it before the wrong codec
// starts answering IS-14 traffic.
func TestRegisterRejectsForeignSpecID(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("Register must panic on a foreign SpecID")
		}
		if msg, _ := r.(string); !strings.Contains(msg, "is-12") {
			t.Fatalf("panic = %v, want it to name the foreign SpecID", r)
		}
	}()
	is14.Register(foreignCodec{})
}

type foreignCodec struct{ is14.Codec }

func (foreignCodec) SpecID() string    { return "is-12" }
func (foreignCodec) APIVer() string    { return "v1.0" }
func (foreignCodec) SpecPatch() string { return "v1.0.0" }
